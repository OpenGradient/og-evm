package attestation

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// Precompile constants
const (
	AddressHex = "0x0000000000000000000000000000000000000901"

	// Gas costs
	GasVerifyAttestation uint64 = 500000 // Expensive due to crypto operations
)

// Method names
const (
	MethodVerifyAttestation = "verifyAttestation"
)

// Precompile implements AWS Nitro attestation verification
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// NewPrecompile creates a new attestation verifier precompile
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

	if method.Name == MethodVerifyAttestation {
		return GasVerifyAttestation
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

	if method.Name == MethodVerifyAttestation {
		return p.verifyAttestation(method, args)
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

// computePCRHash computes keccak256 hash of concatenated PCR values
func computePCRHash(pcr0, pcr1, pcr2 []byte) common.Hash {
	data := make([]byte, 0, len(pcr0)+len(pcr1)+len(pcr2))
	data = append(data, pcr0...)
	data = append(data, pcr1...)
	data = append(data, pcr2...)
	return common.BytesToHash(data)
}
