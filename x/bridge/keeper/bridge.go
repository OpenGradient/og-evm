package keeper

import (
	"github.com/cosmos/evm/x/bridge/types"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MintForBridge mints native tokens to the recipient after an inbound bridge transfer.
func (k Keeper) MintForBridge(ctx sdk.Context, recipient sdk.AccAddress, amount sdkmath.Int) error {
	if err := sdk.VerifyAddressFormat(recipient); err != nil {
		return errorsmod.Wrap(types.ErrInvalidAddress, err.Error())
	}
	if err := k.validateBridgeTransfer(ctx, amount); err != nil {
		return err
	}

	denom := k.getDenom()
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))

	if err := k.bk.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return errorsmod.Wrap(err, "failed to mint bridge coins")
	}
	if err := k.bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, coins); err != nil {
		return errorsmod.Wrap(err, "failed to send bridge coins to recipient")
	}

	k.AddTotalMinted(ctx, amount)
	types.EmitBridgeMintEvent(ctx, recipient.String(), amount.String(), denom)

	k.Logger(ctx).Info(
		"minted bridge tokens",
		"recipient", recipient.String(),
		"amount", amount.String(),
		"denom", denom,
	)

	return nil
}

// BurnForBridge burns native tokens from the sender before an outbound bridge transfer.
func (k Keeper) BurnForBridge(ctx sdk.Context, sender sdk.AccAddress, amount sdkmath.Int) error {
	if err := sdk.VerifyAddressFormat(sender); err != nil {
		return errorsmod.Wrap(types.ErrInvalidAddress, err.Error())
	}
	if err := k.validateBridgeTransfer(ctx, amount); err != nil {
		return err
	}

	denom := k.getDenom()
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))

	if err := k.bk.SendCoinsFromAccountToModule(ctx, sender, types.ModuleName, coins); err != nil {
		return errorsmod.Wrap(err, "failed to move bridge coins into module account")
	}
	if err := k.bk.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return errorsmod.Wrap(err, "failed to burn bridge coins")
	}

	k.AddTotalBurned(ctx, amount)
	types.EmitBridgeBurnEvent(ctx, sender.String(), amount.String(), denom)

	k.Logger(ctx).Info(
		"burned bridge tokens",
		"sender", sender.String(),
		"amount", amount.String(),
		"denom", denom,
	)

	return nil
}

func (k Keeper) validateBridgeTransfer(ctx sdk.Context, amount sdkmath.Int) error {
	params := k.GetParams(ctx)
	if !params.Enabled {
		return types.ErrBridgeDisabled
	}
	if !amount.IsPositive() {
		return errorsmod.Wrapf(types.ErrInvalidAmount, "amount must be positive: %s", amount.String())
	}
	if params.MaxTransferAmount.IsPositive() && amount.GT(params.MaxTransferAmount) {
		return errorsmod.Wrapf(
			types.ErrExceedsMaxTransfer,
			"amount %s exceeds max transfer amount %s",
			amount.String(),
			params.MaxTransferAmount.String(),
		)
	}

	return nil
}
