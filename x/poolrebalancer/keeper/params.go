// Package keeper implements the poolrebalancer module keeper.
//
// params.go contains params get/set helpers and typed accessors.
package keeper

import (
	"context"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"cosmossdk.io/math"
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

// SetParams stores the module params.
func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	if err := k.validatePoolDelegatorAddress(ctx, params.PoolDelegatorAddress); err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)
	bz := k.cdc.MustMarshal(&params)
	return store.Set(types.ParamsKey, bz)
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
