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
)

// Precompile address
const AddressHex = "0x0000000000000000000000000000000000000900"

// Gas costs
const (
	GasVerifyAttestation uint64 = 500000
	GasVerifyRSA         uint64 = 20000
	GasParseUserData     uint64 = 1000
)

const ABI = `[
	{
		"name": "verifyAttestation",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "attestationDocument", "type": "bytes"},
			{"name": "rootCertificate", "type": "bytes"}
		],
		"outputs": [{
			"name": "result",
			"type": "tuple",
			"components": [
				{"name": "valid", "type": "bool"},
				{"name": "pcr0", "type": "bytes"},
				{"name": "pcr1", "type": "bytes"},
				{"name": "pcr2", "type": "bytes"},
				{"name": "userData", "type": "bytes"},
				{"name": "errorMsg", "type": "string"}
			]
		}]
	},
	{
		"name": "verifyRSASignature",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "publicKeyDER", "type": "bytes"},
			{"name": "messageHash", "type": "bytes32"},
			{"name": "signature", "type": "bytes"}
		],
		"outputs": [{"name": "valid", "type": "bool"}]
	},
	{
		"name": "parseNitridingUserData",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [{"name": "userData", "type": "bytes"}],
		"outputs": [
			{"name": "tlsCertHash", "type": "bytes32"},
			{"name": "signingKeyHash", "type": "bytes32"}
		]
	}
]`

// Precompile implements the minimal TEE Verifier
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// NewPrecompile creates a new instance
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

// RequiredGas returns gas cost
func (p *Precompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}
	method, err := p.abi.MethodById(input[:4])
	if err != nil {
		return 0
	}
	switch method.Name {
	case "verifyAttestation":
		return GasVerifyAttestation
	case "verifyRSASignature":
		return GasVerifyRSA
	case "parseNitridingUserData":
		return GasParseUserData
	default:
		return GasParseUserData
	}
}

// Run executes the precompile
func (p *Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) ([]byte, error) {
	if len(contract.Input) < 4 {
		return nil, fmt.Errorf("invalid input")
	}

	method, err := p.abi.MethodById(contract.Input[:4])
	if err != nil {
		return nil, fmt.Errorf("method not found")
	}

	args, err := method.Inputs.Unpack(contract.Input[4:])
	if err != nil {
		return nil, fmt.Errorf("unpack inputs: %w", err)
	}

	switch method.Name {
	case "verifyAttestation":
		return p.verifyAttestation(method, args)
	case "verifyRSASignature":
		return p.verifyRSASignature(method, args)
	case "parseNitridingUserData":
		return p.parseNitridingUserData(method, args)
	default:
		return nil, fmt.Errorf("unknown method: %s", method.Name)
	}
}

// ============================================================================
// METHOD: verifyAttestation
// ============================================================================

type AttestationResultABI struct {
	Valid    bool   `abi:"valid"`
	Pcr0     []byte `abi:"pcr0"`
	Pcr1     []byte `abi:"pcr1"`
	Pcr2     []byte `abi:"pcr2"`
	UserData []byte `abi:"userData"`
	ErrorMsg string `abi:"errorMsg"`
}

func (p *Precompile) verifyAttestation(method *abi.Method, args []interface{}) ([]byte, error) {
	attestationDoc := args[0].([]byte)
	rootCert := args[1].([]byte)

	// Use default AWS root cert if none provided
	if len(rootCert) == 0 {
		rootCert = DefaultAWSNitroRootCertPEM
	}

	// Convert to base64 for verification function
	attestationBase64 := base64.StdEncoding.EncodeToString(attestationDoc)

	// Verify attestation
	result, err := VerifyAttestationDocument(attestationBase64, rootCert, nil)

	abiResult := AttestationResultABI{}

	if err != nil {
		abiResult.Valid = false
		abiResult.ErrorMsg = err.Error()
	} else if !result.Valid {
		abiResult.Valid = false
		abiResult.ErrorMsg = result.ErrorMessage
	} else {
		abiResult.Valid = true
		abiResult.Pcr0 = result.PCRs.PCR0
		abiResult.Pcr1 = result.PCRs.PCR1
		abiResult.Pcr2 = result.PCRs.PCR2
		abiResult.UserData = result.UserData
		abiResult.ErrorMsg = ""
	}

	return method.Outputs.Pack(abiResult)
}

// ============================================================================
// METHOD: verifyRSASignature
// ============================================================================

func (p *Precompile) verifyRSASignature(method *abi.Method, args []interface{}) ([]byte, error) {
	publicKeyDER := args[0].([]byte)
	messageHash := args[1].([32]byte)
	signature := args[2].([]byte)

	valid := verifyRSAPSS(publicKeyDER, messageHash[:], signature)
	return method.Outputs.Pack(valid)
}

func verifyRSAPSS(publicKeyDER []byte, messageHash []byte, signature []byte) bool {
	// Parse public key
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return false
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return false
	}

	// RSA-PSS verification with SHA256
	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}

	// Hash the message hash with SHA256 for RSA-PSS
	h := sha256.Sum256(messageHash)

	err = rsa.VerifyPSS(rsaPub, gcrypto.SHA256, h[:], signature, opts)
	return err == nil
}

// ============================================================================
// METHOD: parseNitridingUserData
// ============================================================================

func (p *Precompile) parseNitridingUserData(method *abi.Method, args []interface{}) ([]byte, error) {
	userData := args[0].([]byte)

	// Nitriding format: 68 bytes
	// [0:2]   = 0x1220 prefix
	// [2:34]  = SHA256(TLS cert DER)
	// [34:36] = 0x1220 prefix
	// [36:68] = SHA256(signing key DER)

	var tlsCertHash [32]byte
	var signingKeyHash [32]byte

	if len(userData) == 68 {
		copy(tlsCertHash[:], userData[2:34])
		copy(signingKeyHash[:], userData[36:68])
	}
	// If wrong length, return zero hashes (Solidity will fail the comparison)

	return method.Outputs.Pack(tlsCertHash, signingKeyHash)
}
