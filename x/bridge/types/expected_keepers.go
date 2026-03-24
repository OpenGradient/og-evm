package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// AccountKeeper defines the expected account keeper interface.
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) authtypes.ModuleAccountI
}

// BankKeeper defines the expected bank keeper interface.
// In app.go, pass PreciseBankKeeper which implements this for 18-decimal support.
type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amounts sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amounts sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}
