package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// addPendingRedelegation records a redelegation until its completion time.
// It writes the primary record, a by-source index record, and appends to the completion-time queue.
func (k Keeper) addPendingRedelegation(ctx context.Context, del sdk.AccAddress, srcVal, dstVal sdk.ValAddress, coin sdk.Coin, completionTime time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
	denom := coin.Denom

	// Primary key: merge if an entry already exists for the same (del, denom, src, dst, completion).
	primaryKey := types.GetPendingRedelegationKey(del, denom, srcVal, dstVal, completionTime)
	var entry types.PendingRedelegation
	if bz, err := store.Get(primaryKey); err == nil && bz != nil && len(bz) > 0 {
		if err := k.cdc.Unmarshal(bz, &entry); err != nil {
			return err
		}
		entry.Amount = entry.Amount.Add(coin)
	} else {
		entry = types.PendingRedelegation{
			DelegatorAddress:    del.String(),
			SrcValidatorAddress: srcVal.String(),
			DstValidatorAddress: dstVal.String(),
			Amount:              coin,
			CompletionTime:      completionTime,
		}
	}
	primaryBz := k.cdc.MustMarshal(&entry)
	if err := store.Set(primaryKey, primaryBz); err != nil {
		return err
	}

	// Index by source validator; value is unused.
	indexKey := types.GetPendingRedelegationBySrcIndexKey(srcVal, completionTime, denom, dstVal, del)
	if err := store.Set(indexKey, []byte{}); err != nil {
		return err
	}

	// Append to the completion-time queue.
	queueKey := types.GetPendingRedelegationQueueKey(completionTime)
	var queued types.QueuedRedelegation
	if bz, err := store.Get(queueKey); err == nil && bz != nil && len(bz) > 0 {
		if err := k.cdc.Unmarshal(bz, &queued); err != nil {
			return err
		}
	}
	queued.Entries = append(queued.Entries, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              coin,
		CompletionTime:      completionTime,
	})
	queueBz := k.cdc.MustMarshal(&queued)
	return store.Set(queueKey, queueBz)
}

// HasImmatureRedelegationTo reports whether there's any in-flight redelegation to dstVal
// for the given (delegator, denom). This is used to prevent transitive redelegations.
func (k Keeper) HasImmatureRedelegationTo(ctx context.Context, del sdk.AccAddress, dstVal sdk.ValAddress, denom string) bool {
	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	prefix := types.GetPendingRedelegationPrefix(del, denom, dstVal)
	iter := storetypes.KVStorePrefixIterator(store, prefix)
	defer iter.Close() //nolint:errcheck
	return iter.Valid()
}

// CanBeginRedelegation performs local checks before calling the staking keeper.
// It rejects self-redelegations, non-positive amounts, and transitive redelegations.
func (k Keeper) CanBeginRedelegation(ctx context.Context, del sdk.AccAddress, srcVal, dstVal sdk.ValAddress, coin sdk.Coin) bool {
	if srcVal.Equals(dstVal) {
		return false
	}
	if !coin.Amount.IsPositive() {
		return false
	}
	if k.HasImmatureRedelegationTo(ctx, del, srcVal, coin.Denom) {
		return false
	}
	return true
}

// BeginTrackedRedelegation calls the staking keeper and records the redelegation for later cleanup.
func (k Keeper) BeginTrackedRedelegation(ctx context.Context, del sdk.AccAddress, srcVal, dstVal sdk.ValAddress, coin sdk.Coin) (completionTime time.Time, err error) {
	unbondingTime, err := k.stakingKeeper.UnbondingTime(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("unbonding time: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	completionTime = sdkCtx.BlockTime().Add(unbondingTime)

	srcValidator, err := k.stakingKeeper.GetValidator(ctx, srcVal)
	if err != nil {
		return time.Time{}, fmt.Errorf("get source validator: %w", err)
	}
	shares, err := srcValidator.SharesFromTokens(coin.Amount)
	if err != nil {
		return time.Time{}, fmt.Errorf("shares from tokens: %w", err)
	}
	if !shares.IsPositive() {
		return time.Time{}, errors.New("shares amount is not positive")
	}

	completionTime, err = k.stakingKeeper.BeginRedelegation(ctx, del, srcVal, dstVal, shares)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin redelegation: %w", err)
	}

	if err := k.addPendingRedelegation(ctx, del, srcVal, dstVal, coin, completionTime); err != nil {
		return time.Time{}, fmt.Errorf("add pending redelegation: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRedelegationStarted,
			sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
			sdk.NewAttribute(types.AttributeKeySrcValidator, srcVal.String()),
			sdk.NewAttribute(types.AttributeKeyDstValidator, dstVal.String()),
			sdk.NewAttribute(types.AttributeKeyAmount, coin.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyDenom, coin.Denom),
			sdk.NewAttribute(types.AttributeKeyCompletionTime, completionTime.UTC().Format(time.RFC3339Nano)),
		),
	)

	return completionTime, nil
}

// deletePendingRedelegation removes the primary and index records for a pending redelegation.
// Deletes are idempotent: removing a missing key is a no-op.
func (k Keeper) deletePendingRedelegation(ctx context.Context, entry types.PendingRedelegation, completion time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
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
	denom := entry.Amount.Denom

	primaryKey := types.GetPendingRedelegationKey(del, denom, srcVal, dstVal, completion)
	if err := store.Delete(primaryKey); err != nil {
		return err
	}
	indexKey := types.GetPendingRedelegationBySrcIndexKey(srcVal, completion, denom, dstVal, del)
	return store.Delete(indexKey)
}

// CompletePendingRedelegations removes matured redelegation records and their indexes.
func (k Keeper) CompletePendingRedelegations(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()
	completed := 0

	store := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	start := types.PendingRedelegationQueueKey
	end := types.GetPendingRedelegationQueueKey(blockTime)
	// Include keys with completionTime <= blockTime by using an exclusive end key immediately after end.
	endExclusive := append(append([]byte{}, end...), 0xFF)

	iter := store.Iterator(start, endExclusive)
	defer iter.Close() //nolint:errcheck

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		completion, err := types.ParsePendingRedelegationQueueKey(key)
		if err != nil {
			return err
		}
		var queued types.QueuedRedelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			return err
		}
		for _, entry := range queued.Entries {
			if err := k.deletePendingRedelegation(ctx, entry, completion); err != nil {
				return err
			}
			completed++
		}
		store.Delete(key)
	}

	if completed > 0 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeRedelegationsCompleted,
				sdk.NewAttribute(types.AttributeKeyCount, strconv.Itoa(completed)),
				sdk.NewAttribute(types.AttributeKeyCompletionTime, blockTime.UTC().Format(time.RFC3339Nano)),
			),
		)
	}

	return nil
}
