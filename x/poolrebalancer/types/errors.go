package types

import (
	"cosmossdk.io/errors"
)

// Sentinel errors for the poolrebalancer module.
var (
	ErrInvalidPoolDelegator   = errors.Register(ModuleName, 1, "pool delegator address not set or invalid")
	ErrTransitiveRedelegation = errors.Register(ModuleName, 2, "redelegation blocked: immature redelegation to source validator")
	ErrSameValidator          = errors.Register(ModuleName, 3, "source and destination validator cannot be the same")
	ErrInvalidAmount          = errors.Register(ModuleName, 4, "amount must be positive")
	ErrNoDelegation           = errors.Register(ModuleName, 5, "no delegation found for delegator and validator")
)
