package tee

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// Precompile constants
const (
	AddressHex = "0x0000000000000000000000000000000000000900"

	// TODO Gas costs: To be validated and benchmarked !!!
	GasRegisterTEE             uint64 = 100000
	GasRegisterWithAttestation uint64 = 600000
	GasVerifySignature         uint64 = 20000
	GasVerifySettlement        uint64 = 25000
	GasSetActive               uint64 = 10000
	GasQuery                   uint64 = 1000
)

// Method names
const (
	MethodRegisterTEE             = "registerTEE"
	MethodRegisterWithAttestation = "registerTEEWithAttestation"
	MethodVerifyAttestation       = "verifyAttestation"
	MethodDeactivateTEE           = "deactivateTEE"
	MethodActivateTEE             = "activateTEE"
	MethodVerifySignature         = "verifySignature"
	MethodVerifySettlement        = "verifySettlement"
	MethodGetTEE                  = "getTEE"
	MethodIsActive                = "isActive"
	MethodGetPublicKey            = "getPublicKey"
	MethodComputeTEEId            = "computeTEEId"
	MethodComputeMessageHash      = "computeMessageHash"
)

// Precompile implements the TEE Registry
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// NewPrecompile creates a new TEE Registry precompile instance
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
	case MethodRegisterTEE:
		return GasRegisterTEE
	case MethodRegisterWithAttestation:
		return GasRegisterWithAttestation
	case MethodVerifySignature:
		return GasVerifySignature
	case MethodVerifySettlement:
		return GasVerifySettlement
	case MethodDeactivateTEE, MethodActivateTEE:
		return GasSetActive
	default:
		return GasQuery
	}
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

	// Check write protection
	if readOnly && p.isWriteMethod(method.Name) {
		return nil, ErrWriteProtection
	}

	storage := NewStorage(evm.StateDB, p.address)
	ctx := &callContext{
		evm:      evm,
		contract: contract,
		storage:  storage,
		method:   method,
	}

	switch method.Name {
	// Registration
	case MethodRegisterTEE:
		return p.registerTEE(ctx, args)
	case MethodRegisterWithAttestation:
		return p.registerTEEWithAttestation(ctx, args)
	case MethodVerifyAttestation:
		return p.verifyAttestation(ctx, args)

	// Management
	case MethodDeactivateTEE:
		return p.deactivateTEE(ctx, args)
	case MethodActivateTEE:
		return p.activateTEE(ctx, args)

	// Verification
	case MethodVerifySignature:
		return p.verifySignature(ctx, args)
	case MethodVerifySettlement:
		return p.verifySettlement(ctx, args)

	// Queries
	case MethodGetTEE:
		return p.getTEE(ctx, args)
	case MethodIsActive:
		return p.isActive(ctx, args)
	case MethodGetPublicKey:
		return p.getPublicKey(ctx, args)

	// Utilities
	case MethodComputeTEEId:
		return p.computeTEEId(ctx, args)
	case MethodComputeMessageHash:
		return p.computeMessageHash(ctx, args)

	default:
		return nil, ErrMethodNotFound
	}
}

func (p *Precompile) isWriteMethod(name string) bool {
	switch name {
	case MethodRegisterTEE, MethodRegisterWithAttestation,
		MethodDeactivateTEE, MethodActivateTEE, MethodVerifySettlement:
		return true
	}
	return false
}

// callContext holds execution context
type callContext struct {
	evm      *vm.EVM
	contract *vm.Contract
	storage  *Storage
	method   *abi.Method
}

func (c *callContext) caller() common.Address {
	return c.contract.Caller()
}

func (c *callContext) timestamp() *big.Int {
	return new(big.Int).SetUint64(c.evm.Context.Time)
}

// ============================================================================
// REGISTRATION METHODS
// ============================================================================

// registerTEE handles trusted registration (without attestation verification)
func (p *Precompile) registerTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	// Parse arguments: registerTEE(bytes publicKey, PCRMeasurements pcrs)
	publicKey := args[0].([]byte)
	pcrsABI := args[1].(struct {
		Pcr0 [32]byte `json:"pcr0"`
		Pcr1 [32]byte `json:"pcr1"`
		Pcr2 [32]byte `json:"pcr2"`
	})

	// Validate public key
	if err := validatePublicKey(publicKey); err != nil {
		return nil, err
	}

	// Compute TEE ID
	teeId := crypto.Keccak256Hash(publicKey)

	// Check if already exists
	if ctx.storage.Exists(teeId) {
		return nil, ErrTEEAlreadyExists
	}

	// Store TEE
	now := ctx.timestamp()
	ctx.storage.StoreTEE(TEEInfo{
		TEEId:         teeId,
		Owner:         ctx.caller(),
		PublicKey:     publicKey,
		PCRs:          FromABIPCRs(ABIPCRMeasurements(pcrsABI)),
		Active:        true,
		RegisteredAt:  now,
		LastUpdatedAt: now,
	})

	return ctx.method.Outputs.Pack(teeId)
}

// registerTEEWithAttestation handles trustless registration with attestation verification
func (p *Precompile) registerTEEWithAttestation(ctx *callContext, args []interface{}) ([]byte, error) {
	// Parse arguments: registerTEEWithAttestation(bytes attestation, PCRMeasurements expectedPcrs)
	attestationBytes := args[0].([]byte)
	expectedPcrsABI := args[1].(struct {
		Pcr0 [32]byte `json:"pcr0"`
		Pcr1 [32]byte `json:"pcr1"`
		Pcr2 [32]byte `json:"pcr2"`
	})

	// Convert attestation to base64 for verification function
	attestationBase64 := base64.StdEncoding.EncodeToString(attestationBytes)

	// Build expected PCRs for verification (32 bytes - truncated)
	expectedPCRs := PCRValues384{
		PCR0: expectedPcrsABI.Pcr0[:],
		PCR1: expectedPcrsABI.Pcr1[:],
		PCR2: expectedPcrsABI.Pcr2[:],
	}

	// Verify attestation document
	result, err := VerifyAttestationDocumentTruncated(attestationBase64, expectedPCRs, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAttestationInvalid, err)
	}
	if !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrAttestationInvalid, result.ErrorMessage)
	}

	// Validate extracted public key
	if err := validatePublicKey(result.PublicKey); err != nil {
		return nil, err
	}

	// Compute TEE ID
	teeId := crypto.Keccak256Hash(result.PublicKey)

	// Check if already exists
	if ctx.storage.Exists(teeId) {
		return nil, ErrTEEAlreadyExists
	}

	// Store TEE
	now := ctx.timestamp()
	ctx.storage.StoreTEE(TEEInfo{
		TEEId:         teeId,
		Owner:         ctx.caller(),
		PublicKey:     result.PublicKey,
		PCRs:          FromABIPCRs(ABIPCRMeasurements(expectedPcrsABI)),
		Active:        true,
		RegisteredAt:  now,
		LastUpdatedAt: now,
	})

	return ctx.method.Outputs.Pack(teeId)
}

// verifyAttestation verifies attestation without registering (view function)
func (p *Precompile) verifyAttestation(ctx *callContext, args []interface{}) ([]byte, error) {
	attestationBytes := args[0].([]byte)
	expectedPcrsABI := args[1].(struct {
		Pcr0 [32]byte `json:"pcr0"`
		Pcr1 [32]byte `json:"pcr1"`
		Pcr2 [32]byte `json:"pcr2"`
	})

	attestationBase64 := base64.StdEncoding.EncodeToString(attestationBytes)
	expectedPCRs := PCRValues384{
		PCR0: expectedPcrsABI.Pcr0[:],
		PCR1: expectedPcrsABI.Pcr1[:],
		PCR2: expectedPcrsABI.Pcr2[:],
	}

	result, _ := VerifyAttestationDocumentTruncated(attestationBase64, expectedPCRs, nil)

	var publicKey []byte
	if result != nil && result.Valid {
		publicKey = result.PublicKey
	}

	return ctx.method.Outputs.Pack(result != nil && result.Valid, publicKey)
}

// ============================================================================
// MANAGEMENT METHODS
// ============================================================================

func (p *Precompile) deactivateTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	// Check ownership
	owner, exists := ctx.storage.GetOwner(teeId)
	if !exists {
		return nil, ErrTEENotFound
	}
	if owner != ctx.caller() {
		return nil, ErrNotTEEOwner
	}

	// Deactivate
	if err := ctx.storage.SetActive(teeId, false, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) activateTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	// Check ownership
	owner, exists := ctx.storage.GetOwner(teeId)
	if !exists {
		return nil, ErrTEENotFound
	}
	if owner != ctx.caller() {
		return nil, ErrNotTEEOwner
	}

	// Activate
	if err := ctx.storage.SetActive(teeId, true, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

// ============================================================================
// VERIFICATION METHODS
// ============================================================================

// verifySignature verifies a TEE signature (view function)
func (p *Precompile) verifySignature(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])
	inputHash := args[1].([32]byte)
	outputHash := args[2].([32]byte)
	timestamp := args[3].(*big.Int)
	signature := args[4].([]byte)

	valid := p.doVerifySignature(ctx.storage, teeId, inputHash, outputHash, timestamp, signature)

	return ctx.method.Outputs.Pack(valid)
}

// verifySettlement verifies signature and emits event (state-changing)
func (p *Precompile) verifySettlement(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])
	inputHash := args[1].([32]byte)
	outputHash := args[2].([32]byte)
	timestamp := args[3].(*big.Int)
	signature := args[4].([]byte)

	valid := p.doVerifySignature(ctx.storage, teeId, inputHash, outputHash, timestamp, signature)

	// TODO: Emit SettlementVerified event if valid

	return ctx.method.Outputs.Pack(valid)
}

// doVerifySignature performs the actual signature verification
func (p *Precompile) doVerifySignature(
	storage *Storage,
	teeId common.Hash,
	inputHash, outputHash [32]byte,
	timestamp *big.Int,
	signature []byte,
) bool {
	// Check TEE is active
	if !storage.IsActive(teeId) {
		return false
	}

	// Get public key
	publicKeyDER, err := storage.GetPublicKey(teeId)
	if err != nil || len(publicKeyDER) == 0 {
		return false
	}

	// Compute message hash: keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
	messageHash := computeMessageHashInternal(inputHash, outputHash, timestamp)

	// Verify RSA-PSS signature
	return verifyRSASignature(publicKeyDER, messageHash[:], signature) == nil
}

// ============================================================================
// QUERY METHODS
// ============================================================================

func (p *Precompile) getTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	info, exists := ctx.storage.LoadTEE(teeId)
	if !exists {
		return nil, ErrTEENotFound
	}

	return ctx.method.Outputs.Pack(info.ToABI())
}

func (p *Precompile) isActive(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])
	return ctx.method.Outputs.Pack(ctx.storage.IsActive(teeId))
}

func (p *Precompile) getPublicKey(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	publicKey, err := ctx.storage.GetPublicKey(teeId)
	if err != nil {
		return nil, err
	}

	return ctx.method.Outputs.Pack(publicKey)
}

// ============================================================================
// UTILITY METHODS
// ============================================================================

func (p *Precompile) computeTEEId(ctx *callContext, args []interface{}) ([]byte, error) {
	publicKey := args[0].([]byte)
	teeId := crypto.Keccak256Hash(publicKey)
	return ctx.method.Outputs.Pack(teeId)
}

func (p *Precompile) computeMessageHash(ctx *callContext, args []interface{}) ([]byte, error) {
	inputHash := args[0].([32]byte)
	outputHash := args[1].([32]byte)
	timestamp := args[2].(*big.Int)

	hash := computeMessageHashInternal(inputHash, outputHash, timestamp)
	return ctx.method.Outputs.Pack(hash)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// computeMessageHashInternal computes keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
func computeMessageHashInternal(inputHash, outputHash [32]byte, timestamp *big.Int) common.Hash {
	// abi.encodePacked: inputHash (32) + outputHash (32) + timestamp (32, left-padded)
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])

	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)

	return crypto.Keccak256Hash(data)
}

// validatePublicKey checks if the public key is valid RSA
func validatePublicKey(publicKeyDER []byte) error {
	if len(publicKeyDER) == 0 {
		return ErrInvalidPublicKey
	}

	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return fmt.Errorf("%w: parse failed", ErrInvalidPublicKey)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: not RSA key", ErrInvalidPublicKey)
	}

	// Require at least 2048-bit key
	if rsaPub.Size() < 256 {
		return fmt.Errorf("%w: key too small", ErrInvalidPublicKey)
	}

	return nil
}

// verifyRSASignature verifies RSA-PSS signature with SHA-256
func verifyRSASignature(publicKeyDER []byte, messageHash []byte, signature []byte) error {
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return ErrInvalidPublicKey
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return ErrInvalidPublicKey
	}

	// Verify RSA-PSS signature
	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}

	// Hash the message if not already 32 bytes
	var hash []byte
	if len(messageHash) == 32 {
		hash = messageHash
	} else {
		h := sha256.Sum256(messageHash)
		hash = h[:]
	}

	return rsa.VerifyPSS(rsaPub, gcrypto.SHA256, hash, signature, opts)
}
