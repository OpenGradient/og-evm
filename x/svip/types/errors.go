package types

import errorsmod "cosmossdk.io/errors"

var (
	ErrNotActivated     = errorsmod.Register(ModuleName, 2, "svip is not activated")
	ErrPaused           = errorsmod.Register(ModuleName, 3, "svip is paused")
	ErrPoolExhausted    = errorsmod.Register(ModuleName, 4, "svip pool is exhausted")
	ErrInvalidAuthority = errorsmod.Register(ModuleName, 5, "invalid authority")
	ErrAlreadyActivated = errorsmod.Register(ModuleName, 6, "svip is already activated")
	ErrPoolNotFunded    = errorsmod.Register(ModuleName, 7, "svip pool has no funds")
	ErrNotYetActivated  = errorsmod.Register(ModuleName, 8, "svip has not been activated yet")
	ErrHalfLifeChange   = errorsmod.Register(ModuleName, 9, "half_life_seconds change exceeds 50% limit")
)
