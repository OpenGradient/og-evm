package keeper

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// validatePoolDelegatorAddress enforces pool_delegator_address safety for governance and genesis.
//
// Contract-only policy: a non-empty address must be validated with IsContract on the EVM keeper,
// except bootstrap when auth has no account yet. User accounts with signing keys are rejected.
// There is no module-account shortcut. Non-empty pool address requires a non-nil evm keeper.
func (k Keeper) validatePoolDelegatorAddress(ctx context.Context, poolDelStr string) error {
	if poolDelStr == "" {
		return nil
	}
	poolDel, err := sdk.AccAddressFromBech32(poolDelStr)
	if err != nil {
		return fmt.Errorf("invalid pool_delegator_address: %w", err)
	}

	if k.accountKeeper == nil && k.evmKeeper == nil {
		return fmt.Errorf("pool_delegator_address requires account keeper or evm keeper for validation")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var acc sdk.AccountI
	if k.accountKeeper != nil {
		acc = k.accountKeeper.GetAccount(ctx, poolDel)
		if acc != nil {
			if pk := acc.GetPubKey(); pk != nil && len(pk.Bytes()) > 0 {
				return fmt.Errorf("pool_delegator_address cannot be a user account with signing keys")
			}
		}
	}

	if k.evmKeeper == nil {
		return fmt.Errorf("pool_delegator_address requires evm keeper when set")
	}

	if k.evmKeeper.IsContract(sdkCtx, common.BytesToAddress(poolDel.Bytes())) {
		return nil
	}
	if k.accountKeeper != nil && acc == nil {
		// Bootstrap: params may be set before the contract account exists in auth.
		return nil
	}
	return fmt.Errorf("pool_delegator_address must be an EVM contract when evm keeper is configured")
}
