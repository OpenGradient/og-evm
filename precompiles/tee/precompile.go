package tee

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"time"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// Precompile constants
const (
	AddressHex = "0x0000000000000000000000000000000000000900"

	// Gas costs
	GasVerifyAttestation uint64 = 500000 // Expensive due to crypto operations
	GasVerifyRSAPSS      uint64 = 20000  // RSA signature verification

	// Input size limits (DoS prevention)
	MaxAttestationSize uint64 = 16 * 1024 // 16KB max attestation document
	MaxCertificateSize uint64 = 8 * 1024  // 8KB max certificate
	MaxPublicKeySize   uint64 = 8 * 1024  // 8KB max public key
	MaxSignatureSize   uint64 = 1024      // 1KB max signature
	MaxRSAKeySize      uint64 = 1024      // 8192 bits max RSA key
	MinRSAKeySize      uint64 = 256       // 2048 bits min RSA key
	MaxPCRSize         uint64 = 64        // 64 bytes max per PCR value
)

// Method names
const (
	MethodVerifyAttestation = "verifyAttestation"
	MethodVerifyRSAPSS      = "verifyRSAPSS"
)

// Precompile implements TEE verification (AWS Nitro attestation + RSA-PSS signatures)
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// NewPrecompile creates a new TEE verifier precompile
func NewPrecompile() (*Precompile, error) {
	return &Precompile{
		abi:     ABI,
		address: common.HexToAddress(AddressHex),
	}, nil
}

// Address returns the precompile address
func (p *Precompile) Address() common.Address {
	return p.address
}

// RequiredGas calculates gas cost for the call
func (p *Precompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}

	method, err := p.abi.MethodById(input[:4])
	if err != nil {
		return 0
	}

	switch method.Name {
	case MethodVerifyAttestation:
		return GasVerifyAttestation
	case MethodVerifyRSAPSS:
		return GasVerifyRSAPSS
	}

	return 0
}

// Run executes the precompile
func (p *Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) ([]byte, error) {
	if len(contract.Input) < 4 {
		return nil, ErrInvalidInput
	}

	method, err := p.abi.MethodById(contract.Input[:4])
	if err != nil {
		return nil, ErrMethodNotFound
	}

	args, err := method.Inputs.Unpack(contract.Input[4:])
	if err != nil {
		return nil, fmt.Errorf("unpack inputs: %w", err)
	}

	switch method.Name {
	case MethodVerifyAttestation:
		return p.verifyAttestation(evm, method, args)
	case MethodVerifyRSAPSS:
		return p.verifyRSAPSS(method, args)
	}

	return nil, ErrMethodNotFound
}

// verifyAttestation verifies AWS Nitro attestation and extracts validated data
func (p *Precompile) verifyAttestation(evm *vm.EVM, method *abi.Method, args []interface{}) ([]byte, error) {
	// Safe type assertions
	attestationDoc, ok := args[0].([]byte)
	if !ok {
		return method.Outputs.Pack(false, common.Hash{})
	}

	signingPublicKey, ok := args[1].([]byte)
	if !ok {
		return method.Outputs.Pack(false, common.Hash{})
	}

	tlsCertificate, ok := args[2].([]byte)
	if !ok {
		return method.Outputs.Pack(false, common.Hash{})
	}

	rootCertificate, ok := args[3].([]byte)
	if !ok {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Validate input sizes (DoS prevention)
	if uint64(len(attestationDoc)) > MaxAttestationSize {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if uint64(len(signingPublicKey)) > MaxPublicKeySize {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if uint64(len(tlsCertificate)) > MaxCertificateSize {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if uint64(len(rootCertificate)) > MaxCertificateSize {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Validate non-empty inputs
	if len(attestationDoc) == 0 {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if len(signingPublicKey) == 0 {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if len(tlsCertificate) == 0 {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Use default AWS root cert if none provided
	if len(rootCertificate) == 0 {
		rootCertificate = DefaultAWSNitroRootCertPEM
	}

	// Convert attestation to base64 (expected format)
	attestationBase64 := base64.StdEncoding.EncodeToString(attestationDoc)

	// Use block timestamp for deterministic certificate validation across all nodes
	blockTime := time.Unix(int64(evm.Context.Time), 0)

	// Verify attestation document using imported verification logic
	result, err := VerifyAttestationDocument(attestationBase64, rootCertificate, nil, &blockTime)
	if err != nil {
		return method.Outputs.Pack(false, common.Hash{})
	}

	if !result.Valid {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Verify Nitriding dual-key binding (TLS certificate)
	if err := VerifyTLSCertificateBinding(tlsCertificate, result.UserData); err != nil {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Verify Nitriding dual-key binding (signing key)
	if err := VerifySigningKeyBinding(signingPublicKey, result.UserData); err != nil {
		return method.Outputs.Pack(false, common.Hash{})
	}

	// Compute PCR hash from extracted PCRs
	pcrHash := computePCRHash(result.PCRs.PCR0, result.PCRs.PCR1, result.PCRs.PCR2)

	// Return success and PCR hash
	return method.Outputs.Pack(true, pcrHash)
}

// verifyRSAPSS verifies RSA-PSS signature with SHA-256
func (p *Precompile) verifyRSAPSS(method *abi.Method, args []interface{}) ([]byte, error) {
	// Safe type assertions
	publicKeyDER, ok := args[0].([]byte)
	if !ok {
		return method.Outputs.Pack(false)
	}

	messageHash, ok := args[1].([32]byte)
	if !ok {
		return method.Outputs.Pack(false)
	}

	signature, ok := args[2].([]byte)
	if !ok {
		return method.Outputs.Pack(false)
	}

	// Validate input sizes (DoS prevention)
	if uint64(len(publicKeyDER)) > MaxPublicKeySize {
		return method.Outputs.Pack(false)
	}

	if uint64(len(signature)) > MaxSignatureSize {
		return method.Outputs.Pack(false)
	}

	// Parse public key
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return method.Outputs.Pack(false)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return method.Outputs.Pack(false)
	}

	// Verify key size (2048-8192 bits)
	keySize := uint64(rsaPub.Size())
	if keySize < MinRSAKeySize || keySize > MaxRSAKeySize {
		return method.Outputs.Pack(false)
	}

	// Configure RSA-PSS options
	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}

	// Hash the message hash with SHA256 for RSA-PSS
	// (The message hash is already keccak256, we apply SHA256 for RSA)
	h := sha256.Sum256(messageHash[:])

	// Verify signature
	err = rsa.VerifyPSS(rsaPub, gcrypto.SHA256, h[:], signature, opts)
	if err != nil {
		return method.Outputs.Pack(false)
	}

	return method.Outputs.Pack(true)
}

// computePCRHash computes keccak256 hash of concatenated PCR values
func computePCRHash(pcr0, pcr1, pcr2 []byte) common.Hash {
	// Validate PCR sizes to prevent excessive memory allocation
	if uint64(len(pcr0)) > MaxPCRSize || uint64(len(pcr1)) > MaxPCRSize || uint64(len(pcr2)) > MaxPCRSize {
		// Return empty hash for invalid PCR sizes
		return common.Hash{}
	}

	data := make([]byte, 0, len(pcr0)+len(pcr1)+len(pcr2))
	data = append(data, pcr0...)
	data = append(data, pcr1...)
	data = append(data, pcr2...)
	return crypto.Keccak256Hash(data)
}