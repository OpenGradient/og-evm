package tee

import "errors"

var (
	// Precompile-level errors
	ErrInvalidInput   = errors.New("tee: invalid input")
	ErrMethodNotFound = errors.New("tee: method not found")

	// Attestation errors (used by attestation.go)
	ErrAttestationInvalid     = errors.New("tee: invalid attestation")
	ErrInvalidCertificate     = errors.New("tee: invalid certificate format")
	ErrPublicKeyBindingFailed = errors.New("tee: public key does not match attestation binding")

	// Signature errors
	ErrInvalidPublicKey = errors.New("tee: invalid public key")
)
