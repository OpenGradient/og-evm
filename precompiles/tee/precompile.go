package tee

import (
	"bytes"
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

	// Gas costs
	GasAdmin              uint64 = 50000
	GasRegisterWithAttest uint64 = 600000
	GasVerifySignature    uint64 = 20000
	GasVerifySettlement   uint64 = 25000
	GasSetActive          uint64 = 10000
	GasPCRManagement      uint64 = 50000
	GasTEETypeManagement  uint64 = 30000
	GasCertManagement     uint64 = 100000
	GasQuery              uint64 = 1000
	GasQueryList          uint64 = 5000
)

// Method names
const (
	// Admin
	MethodAddAdmin    = "addAdmin"
	MethodRemoveAdmin = "removeAdmin"
	MethodIsAdmin     = "isAdmin"
	MethodGetAdmins   = "getAdmins"

	// TEE Type
	MethodAddTEEType        = "addTEEType"
	MethodDeactivateTEEType = "deactivateTEEType"
	MethodIsValidTEEType    = "isValidTEEType"
	MethodGetTEETypes       = "getTEETypes"

	// PCR
	MethodApprovePCR     = "approvePCR"
	MethodRevokePCR      = "revokePCR"
	MethodIsPCRApproved  = "isPCRApproved"
	MethodGetActivePCRs  = "getActivePCRs"
	MethodGetPCRDetails  = "getPCRDetails"
	MethodComputePCRHash = "computePCRHash"

	// Certificate
	MethodSetAWSRootCertificate     = "setAWSRootCertificate"
	MethodGetAWSRootCertificateHash = "getAWSRootCertificateHash"

	// Registration
	MethodRegisterWithAttestation = "registerTEEWithAttestation"

	// Management
	MethodDeactivateTEE = "deactivateTEE"
	MethodActivateTEE   = "activateTEE"

	// Verification
	MethodVerifySignature  = "verifySignature"
	MethodVerifySettlement = "verifySettlement"

	// Queries
	MethodGetTEE         = "getTEE"
	MethodGetActiveTEEs  = "getActiveTEEs"
	MethodGetTEEsByType  = "getTEEsByType"
	MethodGetTEEsByOwner = "getTEEsByOwner"
	MethodGetPublicKey   = "getPublicKey"
	MethodIsActive       = "isActive"

	// Utilities
	MethodComputeTEEId       = "computeTEEId"
	MethodComputeMessageHash = "computeMessageHash"
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
	case MethodAddAdmin, MethodRemoveAdmin:
		return GasAdmin
	case MethodRegisterWithAttestation:
		return GasRegisterWithAttest
	case MethodVerifySignature:
		return GasVerifySignature
	case MethodVerifySettlement:
		return GasVerifySettlement
	case MethodDeactivateTEE, MethodActivateTEE:
		return GasSetActive
	case MethodApprovePCR, MethodRevokePCR:
		return GasPCRManagement
	case MethodSetAWSRootCertificate:
		return GasCertManagement
	case MethodAddTEEType, MethodDeactivateTEEType:
		return GasTEETypeManagement
	case MethodGetActiveTEEs, MethodGetTEEsByType, MethodGetTEEsByOwner, MethodGetAdmins, MethodGetTEETypes, MethodGetActivePCRs:
		return GasQueryList
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
	// Admin
	case MethodAddAdmin:
		return p.addAdmin(ctx, args)
	case MethodRemoveAdmin:
		return p.removeAdmin(ctx, args)
	case MethodIsAdmin:
		return p.isAdminQuery(ctx, args)
	case MethodGetAdmins:
		return p.getAdmins(ctx, args)

	// TEE Type
	case MethodAddTEEType:
		return p.addTEEType(ctx, args)
	case MethodDeactivateTEEType:
		return p.deactivateTEEType(ctx, args)
	case MethodIsValidTEEType:
		return p.isValidTEEType(ctx, args)
	case MethodGetTEETypes:
		return p.getTEETypes(ctx, args)

	// PCR
	case MethodApprovePCR:
		return p.approvePCR(ctx, args)
	case MethodRevokePCR:
		return p.revokePCR(ctx, args)
	case MethodIsPCRApproved:
		return p.isPCRApproved(ctx, args)
	case MethodGetActivePCRs:
		return p.getActivePCRs(ctx, args)
	case MethodGetPCRDetails:
		return p.getPCRDetails(ctx, args)
	case MethodComputePCRHash:
		return p.computePCRHash(ctx, args)

	// Certificate
	case MethodSetAWSRootCertificate:
		return p.setAWSRootCertificate(ctx, args)
	case MethodGetAWSRootCertificateHash:
		return p.getAWSRootCertificateHash(ctx, args)

	// Registration
	case MethodRegisterWithAttestation:
		return p.registerTEEWithAttestation(ctx, args)

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
	case MethodGetActiveTEEs:
		return p.getActiveTEEs(ctx, args)
	case MethodGetTEEsByType:
		return p.getTEEsByType(ctx, args)
	case MethodGetTEEsByOwner:
		return p.getTEEsByOwner(ctx, args)
	case MethodGetPublicKey:
		return p.getPublicKey(ctx, args)
	case MethodIsActive:
		return p.isActive(ctx, args)

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
	case MethodAddAdmin, MethodRemoveAdmin,
		MethodAddTEEType, MethodDeactivateTEEType,
		MethodApprovePCR, MethodRevokePCR, MethodSetAWSRootCertificate,
		MethodRegisterWithAttestation,
		MethodDeactivateTEE, MethodActivateTEE,
		MethodVerifySettlement:
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
// ADMIN METHODS
// ============================================================================

func (p *Precompile) addAdmin(ctx *callContext, args []interface{}) ([]byte, error) {
	newAdmin := args[0].(common.Address)

	// Allow first admin to bootstrap if no admins exist
	admins := ctx.storage.GetAdmins()
	if len(admins) > 0 && !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	if err := ctx.storage.AddAdmin(newAdmin); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) removeAdmin(ctx *callContext, args []interface{}) ([]byte, error) {
	admin := args[0].(common.Address)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	if err := ctx.storage.RemoveAdmin(admin); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) isAdminQuery(ctx *callContext, args []interface{}) ([]byte, error) {
	account := args[0].(common.Address)
	return ctx.method.Outputs.Pack(ctx.storage.IsAdmin(account))
}

func (p *Precompile) getAdmins(ctx *callContext, args []interface{}) ([]byte, error) {
	admins := ctx.storage.GetAdmins()
	return ctx.method.Outputs.Pack(admins)
}

// ============================================================================
// TEE TYPE METHODS
// ============================================================================

func (p *Precompile) addTEEType(ctx *callContext, args []interface{}) ([]byte, error) {
	typeId := args[0].(uint8)
	name := args[1].(string)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	if err := ctx.storage.AddTEEType(typeId, name, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) deactivateTEEType(ctx *callContext, args []interface{}) ([]byte, error) {
	typeId := args[0].(uint8)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	if err := ctx.storage.DeactivateTEEType(typeId); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) isValidTEEType(ctx *callContext, args []interface{}) ([]byte, error) {
	typeId := args[0].(uint8)
	return ctx.method.Outputs.Pack(ctx.storage.IsValidTEEType(typeId))
}

func (p *Precompile) getTEETypes(ctx *callContext, args []interface{}) ([]byte, error) {
	types := ctx.storage.GetTEETypes()

	abiTypes := make([]ABITEETypeInfo, len(types))
	for i, t := range types {
		abiTypes[i] = t.ToABI()
	}

	return ctx.method.Outputs.Pack(abiTypes)
}

// ============================================================================
// PCR METHODS
// ============================================================================

func (p *Precompile) approvePCR(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrsABI := args[0].(struct {
		Pcr0 []byte `json:"pcr0"`
		Pcr1 []byte `json:"pcr1"`
		Pcr2 []byte `json:"pcr2"`
	})
	version := args[1].(string)
	previousPcrHash := args[2].([32]byte)
	gracePeriod := args[3].(*big.Int)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	// Compute PCR hash
	pcrHash := computePCRHashInternal(pcrsABI.Pcr0, pcrsABI.Pcr1, pcrsABI.Pcr2)

	// Set expiry on previous PCR if provided
	if previousPcrHash != [32]byte{} {
		expiresAt := new(big.Int).Add(ctx.timestamp(), gracePeriod)
		if err := ctx.storage.SetPCRExpiry(common.BytesToHash(previousPcrHash[:]), expiresAt); err != nil {
			return nil, err
		}
	}

	// Approve new PCR (no expiry)
	if err := ctx.storage.ApprovePCR(pcrHash, version, nil, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) revokePCR(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrHashArg := args[0].([32]byte)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	if err := ctx.storage.RevokePCR(common.BytesToHash(pcrHashArg[:])); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) isPCRApproved(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrsABI := args[0].(struct {
		Pcr0 []byte `json:"pcr0"`
		Pcr1 []byte `json:"pcr1"`
		Pcr2 []byte `json:"pcr2"`
	})

	pcrHash := computePCRHashInternal(pcrsABI.Pcr0, pcrsABI.Pcr1, pcrsABI.Pcr2)
	approved := ctx.storage.IsPCRApproved(pcrHash, ctx.timestamp())

	return ctx.method.Outputs.Pack(approved)
}

func (p *Precompile) getActivePCRs(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrs := ctx.storage.GetActivePCRs(ctx.timestamp())

	// Convert to [32]byte array
	result := make([][32]byte, len(pcrs))
	for i, h := range pcrs {
		result[i] = h
	}

	return ctx.method.Outputs.Pack(result)
}

func (p *Precompile) getPCRDetails(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrHashArg := args[0].([32]byte)

	details, exists := ctx.storage.GetPCRDetails(common.BytesToHash(pcrHashArg[:]))
	if !exists {
		return nil, ErrPCRNotFound
	}

	return ctx.method.Outputs.Pack(details.ToABI())
}

func (p *Precompile) computePCRHash(ctx *callContext, args []interface{}) ([]byte, error) {
	pcrsABI := args[0].(struct {
		Pcr0 []byte `json:"pcr0"`
		Pcr1 []byte `json:"pcr1"`
		Pcr2 []byte `json:"pcr2"`
	})

	pcrHash := computePCRHashInternal(pcrsABI.Pcr0, pcrsABI.Pcr1, pcrsABI.Pcr2)
	return ctx.method.Outputs.Pack(pcrHash)
}

// ============================================================================
// CERTIFICATE METHODS
// ============================================================================

func (p *Precompile) setAWSRootCertificate(ctx *callContext, args []interface{}) ([]byte, error) {
	certificate := args[0].([]byte)

	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	// Validate certificate format
	if err := ValidateRootCertificate(certificate); err != nil {
		return nil, err
	}

	// Store full certificate bytes
	ctx.storage.SetAWSRootCertificate(certificate)

	return nil, nil
}

func (p *Precompile) getAWSRootCertificateHash(ctx *callContext, args []interface{}) ([]byte, error) {
	hash := ctx.storage.GetAWSRootCertificateHash()
	return ctx.method.Outputs.Pack(hash)
}

// ============================================================================
// REGISTRATION METHODS
// ============================================================================

func (p *Precompile) registerTEEWithAttestation(ctx *callContext, args []interface{}) ([]byte, error) {
	attestationBytes := args[0].([]byte)
	publicKeyDER := args[1].([]byte) // Public key should be provided by operator
	paymentAddress := args[2].(common.Address)
	endpoint := args[3].(string)
	teeType := args[4].(uint8)

	// Check caller is admin
	if !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotAdmin
	}

	// Check TEE type is valid
	if !ctx.storage.IsValidTEEType(teeType) {
		return nil, ErrInvalidTEEType
	}

	// Validate the provided public key format FIRST
	if err := validatePublicKey(publicKeyDER); err != nil {
		return nil, err
	}

	// Get root certificate from storage (or use default)
	rootCert := ctx.storage.GetAWSRootCertificate()
	if len(rootCert) == 0 {
		rootCert = DefaultAWSNitroRootCertPEM
	}

	// Convert attestation to base64
	attestationBase64 := base64.StdEncoding.EncodeToString(attestationBytes)

	// Verify attestation and extract PCRs using stored/default cert
	result, err := VerifyAttestationDocument(attestationBase64, rootCert, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAttestationInvalid, err)
	}
	if !result.Valid {
		return nil, fmt.Errorf("%w: %s", ErrAttestationInvalid, result.ErrorMessage)
	}

	// =========================================================================
	// Verify public key binding (Nitriding framework support)
	// =========================================================================
	// Nitriding puts SHA256(publicKey) in the attestation's public_key field
	// We verify the provided public key matches this binding
	if err := verifyPublicKeyBinding(result.PublicKey, publicKeyDER); err != nil {
		return nil, err
	}

	// Compute PCR hash from extracted PCRs
	pcrHash := computePCRHashInternal(result.PCRs.PCR0, result.PCRs.PCR1, result.PCRs.PCR2)

	// Check PCR is in approved list
	if !ctx.storage.IsPCRApproved(pcrHash, ctx.timestamp()) {
		return nil, ErrPCRNotApproved
	}

	// Compute TEE ID from the actual public key
	teeId := crypto.Keccak256Hash(publicKeyDER)

	// Check if already exists
	if ctx.storage.Exists(teeId) {
		return nil, ErrTEEAlreadyExists
	}

	// Store TEE with the actual public key
	now := ctx.timestamp()
	ctx.storage.StoreTEE(TEEInfo{
		TEEId:          teeId,
		Owner:          ctx.caller(),
		PaymentAddress: paymentAddress,
		Endpoint:       endpoint,
		PublicKey:      publicKeyDER,
		PCRHash:        pcrHash,
		TEEType:        teeType,
		Active:         true,
		RegisteredAt:   now,
		LastUpdatedAt:  now,
	})

	return ctx.method.Outputs.Pack(teeId)
}

// verifyPublicKeyBinding verifies that the provided public key matches the attestation binding
// Supports both Nitriding mode (hash binding) and standard mode (full key)
func verifyPublicKeyBinding(attestationPubKey, providedPubKey []byte) error {
	if len(attestationPubKey) == 0 {
		// No public key in attestation - accept provided key
		// This allows for configurations that don't include public key in attestation
		return nil
	}

	if len(attestationPubKey) == sha256.Size {
		// Nitriding mode: attestation contains SHA256 hash of public key
		expectedHash := sha256.Sum256(providedPubKey)
		if !bytes.Equal(attestationPubKey, expectedHash[:]) {
			return ErrPublicKeyBindingFailed
		}
		return nil
	}

	// Standard mode: attestation contains full public key
	if !bytes.Equal(attestationPubKey, providedPubKey) {
		return ErrPublicKeyBindingFailed
	}

	return nil
}

// ============================================================================
// MANAGEMENT METHODS
// ============================================================================

func (p *Precompile) deactivateTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	owner, exists := ctx.storage.GetOwner(teeId)
	if !exists {
		return nil, ErrTEENotFound
	}

	// Allow owner or admin
	if owner != ctx.caller() && !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotTEEOwner
	}

	if err := ctx.storage.SetActive(teeId, false, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Precompile) activateTEE(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])

	owner, exists := ctx.storage.GetOwner(teeId)
	if !exists {
		return nil, ErrTEENotFound
	}

	// Allow owner or admin
	if owner != ctx.caller() && !ctx.storage.IsAdmin(ctx.caller()) {
		return nil, ErrNotTEEOwner
	}

	if err := ctx.storage.SetActive(teeId, true, ctx.timestamp()); err != nil {
		return nil, err
	}

	return nil, nil
}

// ============================================================================
// VERIFICATION METHODS
// ============================================================================

func (p *Precompile) verifySignature(ctx *callContext, args []interface{}) ([]byte, error) {
	request := args[0].(struct {
		TeeId        [32]byte `json:"teeId"`
		RequestHash  [32]byte `json:"requestHash"`
		ResponseHash [32]byte `json:"responseHash"`
		Timestamp    *big.Int `json:"timestamp"`
		Signature    []byte   `json:"signature"`
	})

	teeId := common.BytesToHash(request.TeeId[:])

	// Validate timestamp bounds
	if err := p.validateTimestamp(ctx, request.Timestamp); err != nil {
		return ctx.method.Outputs.Pack(false)
	}

	valid := p.doVerifySignature(ctx.storage, teeId, request.RequestHash, request.ResponseHash, request.Timestamp, request.Signature)

	return ctx.method.Outputs.Pack(valid)
}

func (p *Precompile) verifySettlement(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])
	inputHash := args[1].([32]byte)
	outputHash := args[2].([32]byte)
	timestamp := args[3].(*big.Int)
	signature := args[4].([]byte)

	// Validate timestamp bounds
	if err := p.validateTimestamp(ctx, timestamp); err != nil {
		return nil, err
	}

	// Compute settlement hash for replay protection
	settlementHash := ComputeSettlementHash(teeId, inputHash, outputHash, timestamp)

	// Check replay protection
	if ctx.storage.IsSettlementUsed(settlementHash) {
		return nil, ErrSettlementAlreadyUsed
	}

	// Verify signature
	valid := p.doVerifySignature(ctx.storage, teeId, inputHash, outputHash, timestamp, signature)

	if valid {
		// Mark settlement as used (replay protection)
		ctx.storage.MarkSettlementUsed(settlementHash)

		// TODO: Emit SettlementVerified event
		// Events require EVM log support which depends on your chain implementation
	}

	return ctx.method.Outputs.Pack(valid)
}

func (p *Precompile) validateTimestamp(ctx *callContext, timestamp *big.Int) error {
	now := ctx.timestamp().Uint64()
	ts := timestamp.Uint64()

	// Check not too far in future
	if ts > now+FutureTimeTolerance {
		return ErrTimestampInFuture
	}

	// Check not too old
	if ts < now-MaxSettlementAge {
		return ErrTimestampTooOld
	}

	return nil
}

func (p *Precompile) doVerifySignature(
	storage *Storage,
	teeId common.Hash,
	inputHash, outputHash [32]byte,
	timestamp *big.Int,
	signature []byte,
) bool {
	if !storage.IsActive(teeId) {
		return false
	}

	publicKeyDER, err := storage.GetPublicKey(teeId)
	if err != nil || len(publicKeyDER) == 0 {
		return false
	}

	messageHash := computeMessageHashInternal(inputHash, outputHash, timestamp)
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

func (p *Precompile) getActiveTEEs(ctx *callContext, args []interface{}) ([]byte, error) {
	tees := ctx.storage.GetActiveTEEs()

	result := make([][32]byte, len(tees))
	for i, h := range tees {
		result[i] = h
	}

	return ctx.method.Outputs.Pack(result)
}

func (p *Precompile) getTEEsByType(ctx *callContext, args []interface{}) ([]byte, error) {
	teeType := args[0].(uint8)
	tees := ctx.storage.GetTEEsByType(teeType)

	result := make([][32]byte, len(tees))
	for i, h := range tees {
		result[i] = h
	}

	return ctx.method.Outputs.Pack(result)
}

func (p *Precompile) getTEEsByOwner(ctx *callContext, args []interface{}) ([]byte, error) {
	owner := args[0].(common.Address)
	tees := ctx.storage.GetTEEsByOwner(owner)

	result := make([][32]byte, len(tees))
	for i, h := range tees {
		result[i] = h
	}

	return ctx.method.Outputs.Pack(result)
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

func (p *Precompile) isActive(ctx *callContext, args []interface{}) ([]byte, error) {
	teeIdArg := args[0].([32]byte)
	teeId := common.BytesToHash(teeIdArg[:])
	return ctx.method.Outputs.Pack(ctx.storage.IsActive(teeId))
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

func computeMessageHashInternal(inputHash, outputHash [32]byte, timestamp *big.Int) common.Hash {
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])

	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)

	return crypto.Keccak256Hash(data)
}

func computePCRHashInternal(pcr0, pcr1, pcr2 []byte) common.Hash {
	data := make([]byte, 0, len(pcr0)+len(pcr1)+len(pcr2))
	data = append(data, pcr0...)
	data = append(data, pcr1...)
	data = append(data, pcr2...)
	return crypto.Keccak256Hash(data)
}

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

	if rsaPub.Size() < 256 {
		return fmt.Errorf("%w: key too small", ErrInvalidPublicKey)
	}

	return nil
}

func verifyRSASignature(publicKeyDER []byte, messageHash []byte, signature []byte) error {
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return ErrInvalidPublicKey
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return ErrInvalidPublicKey
	}

	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}

	// Hash the message hash with SHA256 for RSA-PSS
	h := sha256.Sum256(messageHash)

	return rsa.VerifyPSS(rsaPub, gcrypto.SHA256, h[:], signature, opts)
}
