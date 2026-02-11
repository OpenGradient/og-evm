package tee

import "errors"

var (
	ErrInvalidInput    = errors.New("tee: invalid input")
	ErrMethodNotFound  = errors.New("tee: method not found")
	ErrInvalidKey      = errors.New("tee: invalid public key")
	ErrInvalidDocument = errors.New("tee: invalid attestation document")
)

// Additional errors for verification
var (
	ErrInvalidCertificate       = errors.New("tee: invalid certificate")
	ErrPublicKeyBindingFailed   = errors.New("tee: public key binding verification failed")
)
