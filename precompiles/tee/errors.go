package tee

import "errors"

var (
	// Registration errors
	ErrTEENotFound      = errors.New("tee: not found")
	ErrTEEAlreadyExists = errors.New("tee: already exists")
	ErrTEENotActive     = errors.New("tee: not active")
	ErrNotTEEOwner      = errors.New("tee: caller is not owner")

	// Attestation errors
	ErrAttestationInvalid = errors.New("tee: invalid attestation")
	ErrPCRMismatch        = errors.New("tee: PCR mismatch")

	// Signature errors
	ErrInvalidSignature = errors.New("tee: invalid signature")
	ErrInvalidPublicKey = errors.New("tee: invalid public key")

	// Input errors
	ErrInvalidInput    = errors.New("tee: invalid input")
	ErrMethodNotFound  = errors.New("tee: method not found")
	ErrWriteProtection = errors.New("tee: write protection")
)
