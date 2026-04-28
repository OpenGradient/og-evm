package keeper

import (
	"context"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetPendingRedelegation writes a pending redelegation entry to the store, including its queue and index keys.
// This is intended for genesis import/export.
func (k Keeper) SetPendingRedelegation(ctx context.Context, entry types.PendingRedelegation) error {
	del, err := sdk.AccAddressFromBech32(entry.DelegatorAddress)
	if err != nil {
		return err
	}
	srcVal, err := sdk.ValAddressFromBech32(entry.SrcValidatorAddress)
	if err != nil {
		return err
	}
	dstVal, err := sdk.ValAddressFromBech32(entry.DstValidatorAddress)
	if err != nil {
		return err
	}
	return k.addPendingRedelegation(ctx, del, srcVal, dstVal, entry.Amount, entry.CompletionTime)
}

// GetAllPendingRedelegations returns all pending redelegation entries stored under the primary key prefix.
func (k Keeper) GetAllPendingRedelegations(ctx context.Context) ([]types.PendingRedelegation, error) {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	iter := storetypes.KVStorePrefixIterator(store, types.PendingRedelegationKey)
	defer iter.Close() //nolint:errcheck

	out := make([]types.PendingRedelegation, 0)
	for ; iter.Valid(); iter.Next() {
		var entry types.PendingRedelegation
		if err := k.cdc.Unmarshal(iter.Value(), &entry); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}
