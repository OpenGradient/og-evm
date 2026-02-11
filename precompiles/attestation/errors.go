package attestation

import "errors"

var (
	ErrInvalidInput    = errors.New("attestation: invalid input")
	ErrMethodNotFound  = errors.New("attestation: method not found")
	ErrInvalidDocument = errors.New("attestation: invalid attestation document")
)
