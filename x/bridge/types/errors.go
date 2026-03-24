package types

import errorsmod "cosmossdk.io/errors"

var (
	ErrBridgeDisabled     = errorsmod.Register(ModuleName, 2, "bridge is disabled")
	ErrUnauthorizedCaller = errorsmod.Register(ModuleName, 3, "unauthorized caller")
	ErrInvalidAmount      = errorsmod.Register(ModuleName, 4, "invalid amount")
	ErrExceedsMaxTransfer = errorsmod.Register(ModuleName, 5, "exceeds max transfer amount")
	ErrInvalidAddress     = errorsmod.Register(ModuleName, 6, "invalid address")
	ErrInvalidParams      = errorsmod.Register(ModuleName, 7, "invalid params")
	ErrInvalidAuthority   = errorsmod.Register(ModuleName, 8, "invalid authority")
)
