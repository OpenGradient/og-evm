package rsa

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"strings"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// Precompile constants
const (
	AddressHex = "0x0000000000000000000000000000000000000902"

	// Gas costs
	GasVerifyRSAPSS uint64 = 20000 // RSA signature verification
)

// Method names
const (
	MethodVerifyRSAPSS = "verifyRSAPSS"
)

// Precompile implements RSA-PSS signature verification
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// NewPrecompile creates a new RSA verifier precompile
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

	if method.Name == MethodVerifyRSAPSS {
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

	if method.Name == MethodVerifyRSAPSS {
		return p.verifyRSAPSS(method, args)
	}

	return nil, ErrMethodNotFound
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
