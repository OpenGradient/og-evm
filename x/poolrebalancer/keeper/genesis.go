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

// SetPendingUndelegation writes a pending undelegation entry to the store, including its queue and index keys.
// This is intended for genesis import/export.
func (k Keeper) SetPendingUndelegation(ctx context.Context, entry types.PendingUndelegation) error {
	del, err := sdk.AccAddressFromBech32(entry.DelegatorAddress)
	if err != nil {
		return err
	}
	val, err := sdk.ValAddressFromBech32(entry.ValidatorAddress)
	if err != nil {
		return err
	}
	return k.addPendingUndelegation(ctx, del, val, entry.Balance, entry.CompletionTime)
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

// GetAllPendingUndelegations returns all pending undelegation entries by iterating queue keys and flattening entries.
func (k Keeper) GetAllPendingUndelegations(ctx context.Context) ([]types.PendingUndelegation, error) {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	iter := storetypes.KVStorePrefixIterator(store, types.PendingUndelegationQueueKey)
	defer iter.Close() //nolint:errcheck

	out := make([]types.PendingUndelegation, 0)
	for ; iter.Valid(); iter.Next() {
		var queued types.QueuedUndelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			return nil, err
		}
		out = append(out, queued.Entries...)
	}
	return out, nil
}
