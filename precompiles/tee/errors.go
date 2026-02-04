package tee

import "errors"

var (
	// ============ Registration Errors ============
	ErrTEENotFound      = errors.New("tee: not found")
	ErrTEEAlreadyExists = errors.New("tee: already exists")
	ErrTEENotActive     = errors.New("tee: not active")
	ErrNotTEEOwner      = errors.New("tee: caller is not owner")

	// ============ Admin Errors ============
	ErrNotAdmin              = errors.New("tee: caller is not admin")
	ErrAdminAlreadyExists    = errors.New("tee: admin already exists")
	ErrAdminNotFound         = errors.New("tee: admin not found")
	ErrCannotRemoveLastAdmin = errors.New("tee: cannot remove last admin")

	// ============ PCR Registry Errors ============
	ErrPCRNotApproved = errors.New("tee: PCR not in approved list")
	ErrPCRExpired     = errors.New("tee: PCR has expired")
	ErrPCRNotFound    = errors.New("tee: PCR not found")

	// ============ TEE Type Errors ============
	ErrInvalidTEEType  = errors.New("tee: invalid or inactive TEE type")
	ErrTEETypeExists   = errors.New("tee: TEE type already exists")
	ErrTEETypeNotFound = errors.New("tee: TEE type not found")

	// ============ Attestation Errors ============
	ErrAttestationInvalid    = errors.New("tee: invalid attestation")
	ErrPCRMismatch           = errors.New("tee: PCR mismatch")
	ErrRootCertificateNotSet = errors.New("tee: AWS root certificate not set")
	ErrInvalidCertificate    = errors.New("tee: invalid certificate format")

	// ============ Public Key Binding Errors ============
	ErrPublicKeyBindingFailed = errors.New("tee: public key does not match attestation binding")

	// ============ Signature Errors ============
	ErrInvalidSignature = errors.New("tee: invalid signature")
	ErrInvalidPublicKey = errors.New("tee: invalid public key")

	// ============ Settlement Errors ============
	ErrSettlementAlreadyUsed = errors.New("tee: settlement already verified")
	ErrTimestampTooOld       = errors.New("tee: timestamp too old")
	ErrTimestampInFuture     = errors.New("tee: timestamp in future")

	// ============ Input Errors ============
	ErrInvalidInput    = errors.New("tee: invalid input")
	ErrMethodNotFound  = errors.New("tee: method not found")
	ErrWriteProtection = errors.New("tee: write protection")
)
