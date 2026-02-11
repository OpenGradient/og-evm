package tee

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// Precompile constants
const (
	AddressHex = "0x0000000000000000000000000000000000000901"

	// Gas costs
	GasVerifyAttestation uint64 = 500000 // Expensive due to crypto operations
	GasVerifyRSAPSS      uint64 = 20000  // RSA signature verification
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
	parsed, err := abi.JSON(strings.NewReader(ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	return &Precompile{
		abi:     parsed,
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
		return p.verifyAttestation(method, args)
	case MethodVerifyRSAPSS:
		return p.verifyRSAPSS(method, args)
	}

	return nil, ErrMethodNotFound
}

// verifyAttestation verifies AWS Nitro attestation and extracts validated data
func (p *Precompile) verifyAttestation(method *abi.Method, args []interface{}) ([]byte, error) {
	attestationDoc := args[0].([]byte)
	signingPublicKey := args[1].([]byte)
	tlsCertificate := args[2].([]byte)
	rootCertificate := args[3].([]byte)

	// Validate inputs
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

	// Verify attestation document using imported verification logic
	result, err := VerifyAttestationDocument(attestationBase64, rootCertificate, nil)
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
	publicKeyDER := args[0].([]byte)
	messageHash := args[1].([32]byte)
	signature := args[2].([]byte)

	// Parse public key
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return method.Outputs.Pack(false)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return method.Outputs.Pack(false)
	}

	// Verify minimum key size (2048 bits)
	if rsaPub.Size() < 256 {
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
	data := make([]byte, 0, len(pcr0)+len(pcr1)+len(pcr2))
	data = append(data, pcr0...)
	data = append(data, pcr1...)
	data = append(data, pcr2...)
	return crypto.Keccak256Hash(data)
}
