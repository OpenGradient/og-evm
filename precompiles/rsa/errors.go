package rsa

import "errors"

var (
	ErrInvalidInput   = errors.New("rsa: invalid input")
	ErrMethodNotFound = errors.New("rsa: method not found")
	ErrInvalidKey     = errors.New("rsa: invalid public key")
)
