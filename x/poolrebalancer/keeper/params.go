// Package keeper implements the poolrebalancer module keeper.
//
// params.go contains params get/set helpers and typed accessors.
package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams returns the current module params.
func (k Keeper) GetParams(ctx context.Context) (params types.Params, err error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ParamsKey)
	if err != nil {
		return params, err
	}
	if bz == nil || len(bz) == 0 {
		return types.DefaultParams(), nil
	}
	if err := k.cdc.Unmarshal(bz, &params); err != nil {
		return params, err
	}
	return params, nil
}

// SetParams stores module params after validating pool delegator safety.
// In particular, it rejects pool_delegator_address changes that would orphan
// tracked pending undelegations/redelegations.
func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	currentParams, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	if err := k.validatePoolDelegatorAddressChange(ctx, currentParams.PoolDelegatorAddress, params.PoolDelegatorAddress); err != nil {
		return err
	}
	if err := k.validatePoolDelegatorAddress(ctx, params.PoolDelegatorAddress); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	bz := k.cdc.MustMarshal(&params)
	return store.Set(types.ParamsKey, bz)
}

func (k Keeper) hasPendingUndelegations(ctx context.Context) (bool, error) {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	iter := storetypes.KVStorePrefixIterator(store, types.PendingUndelegationQueueKey)
	defer iter.Close() //nolint:errcheck
	return iter.Valid(), nil
}

func (k Keeper) hasPendingRedelegationsForDelegator(ctx context.Context, del sdk.AccAddress) (bool, error) {
	if del.Empty() {
		return false, nil
	}
	entries, err := k.GetAllPendingRedelegations(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.DelegatorAddress == del.String() {
			return true, nil
		}
	}
	return false, nil
}

// validatePoolDelegatorAddressChange prevents changing/clearing
// pool_delegator_address while pool-tracked pending state exists.
func (k Keeper) validatePoolDelegatorAddressChange(ctx context.Context, current, next string) error {
	if current == next || current == "" {
		return nil
	}

	hasUndelegations, err := k.hasPendingUndelegations(ctx)
	if err != nil {
		return err
	}
	if hasUndelegations {
		return fmt.Errorf("cannot change pool_delegator_address while pending undelegations exist")
	}

	currentDel, err := sdk.AccAddressFromBech32(current)
	if err != nil {
		return fmt.Errorf("invalid current pool_delegator_address: %w", err)
	}
	hasRedelegations, err := k.hasPendingRedelegationsForDelegator(ctx, currentDel)
	if err != nil {
		return err
	}
	if hasRedelegations {
		return fmt.Errorf("cannot change pool_delegator_address while pending redelegations exist for current pool delegator")
	}

	if next == "" {
		return nil
	}
	nextDel, err := sdk.AccAddressFromBech32(next)
	if err != nil {
		return fmt.Errorf("invalid next pool_delegator_address: %w", err)
	}
	hasRedelegations, err = k.hasPendingRedelegationsForDelegator(ctx, nextDel)
	if err != nil {
		return err
	}
	if hasRedelegations {
		return fmt.Errorf("cannot change pool_delegator_address while pending redelegations exist for next pool delegator")
	}
	return nil
}

// GetPoolDelegatorAddress returns the configured pool delegator address (empty if not set).
func (k Keeper) GetPoolDelegatorAddress(ctx context.Context) (sdk.AccAddress, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if params.PoolDelegatorAddress == "" {
		return sdk.AccAddress{}, nil
	}
	return sdk.AccAddressFromBech32(params.PoolDelegatorAddress)
}

// GetMaxTargetValidators returns MaxTargetValidators from params.
func (k Keeper) GetMaxTargetValidators(ctx context.Context) (uint32, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	return params.MaxTargetValidators, nil
}

// GetRebalanceThresholdBP returns RebalanceThresholdBP from params.
func (k Keeper) GetRebalanceThresholdBP(ctx context.Context) (uint32, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	return params.RebalanceThresholdBp, nil
}

// GetMaxOpsPerBlock returns MaxOpsPerBlock from params.
func (k Keeper) GetMaxOpsPerBlock(ctx context.Context) (uint32, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return 0, err
	}
	return params.MaxOpsPerBlock, nil
}

// GetMaxMovePerOp returns MaxMovePerOp from params (as math.Int; zero means no cap).
func (k Keeper) GetMaxMovePerOp(ctx context.Context) (math.Int, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	if params.MaxMovePerOp.IsNil() {
		return math.ZeroInt(), nil
	}
	return params.MaxMovePerOp, nil
}

// GetUseUndelegateFallback returns UseUndelegateFallback from params.
func (k Keeper) GetUseUndelegateFallback(ctx context.Context) (bool, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	return params.UseUndelegateFallback, nil
}
