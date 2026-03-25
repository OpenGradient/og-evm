package keeper

import (
	"github.com/cosmos/evm/x/bridge/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams returns the current bridge module parameters.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ParamsStoreKey)
	if bz == nil {
		return types.DefaultParams()
	}

	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params
}

// SetParams validates and stores the current bridge module parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}

	store := ctx.KVStore(k.storeKey)
	store.Set(types.ParamsStoreKey, k.cdc.MustMarshal(&params))
	return nil
}

// GetAuthorizedContract returns the currently authorized bridge contract address.
func (k Keeper) GetAuthorizedContract(ctx sdk.Context) string {
	return k.GetParams(ctx).AuthorizedContract
}
