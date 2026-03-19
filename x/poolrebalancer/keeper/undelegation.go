package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"github.com/cosmos/cosmos-sdk/runtime"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// addPendingUndelegation records an undelegation until its completion time.
// It appends to the (completionTime, delegator) queue and writes a by-validator index entry.
func (k Keeper) addPendingUndelegation(ctx context.Context, del sdk.AccAddress, val sdk.ValAddress, coin sdk.Coin, completionTime time.Time) error {
	store := k.storeService.OpenKVStore(ctx)
	denom := coin.Denom

	// Queue entry at (completionTime, delegator).
	queueKey := types.GetPendingUndelegationQueueKey(completionTime, del)
	var queued types.QueuedUndelegation
	if bz, err := store.Get(queueKey); err == nil && bz != nil && len(bz) > 0 {
		if err := k.cdc.Unmarshal(bz, &queued); err != nil {
			return err
		}
	}
	queued.Entries = append(queued.Entries, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completionTime,
	})
	queueBz := k.cdc.MustMarshal(&queued)
	if err := store.Set(queueKey, queueBz); err != nil {
		return err
	}

	// By-validator index; value is unused.
	indexKey := types.GetPendingUndelegationByValIndexKey(val, completionTime, denom, del)
	return store.Set(indexKey, []byte{})
}

// BeginTrackedUndelegation calls the staking keeper and records the undelegation for later cleanup.
func (k Keeper) BeginTrackedUndelegation(ctx context.Context, del sdk.AccAddress, valAddr sdk.ValAddress, coin sdk.Coin) (completionTime time.Time, amountUnbonded math.Int, err error) {
	if !coin.Amount.IsPositive() {
		return time.Time{}, math.ZeroInt(), errors.New("undelegate amount must be positive")
	}

	val, err := k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("get validator: %w", err)
	}
	shares, err := val.SharesFromTokens(coin.Amount)
	if err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("shares from tokens: %w", err)
	}
	if !shares.IsPositive() {
		return time.Time{}, math.ZeroInt(), errors.New("shares amount is not positive")
	}

	// Ensure the delegation has at least the requested shares.
	delegation, err := k.stakingKeeper.GetDelegation(ctx, del, valAddr)
	if err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("get delegation: %w", err)
	}
	if delegation.Shares.LT(shares) {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("insufficient delegation: have %s shares, need %s", delegation.Shares, shares)
	}

	completionTime, amountUnbonded, err = k.stakingKeeper.Undelegate(ctx, del, valAddr, shares)
	if err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("undelegate: %w", err)
	}

	if amountUnbonded.IsZero() {
		return completionTime, amountUnbonded, nil
	}

	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("bond denom: %w", err)
	}
	if err := k.addPendingUndelegation(ctx, del, valAddr, sdk.NewCoin(bondDenom, amountUnbonded), completionTime); err != nil {
		return time.Time{}, math.ZeroInt(), fmt.Errorf("add pending undelegation: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUndelegationStarted,
			sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
			sdk.NewAttribute(types.AttributeKeyValidator, valAddr.String()),
			sdk.NewAttribute(types.AttributeKeyAmount, amountUnbonded.String()),
			sdk.NewAttribute(types.AttributeKeyDenom, bondDenom),
			sdk.NewAttribute(types.AttributeKeyCompletionTime, completionTime.UTC().Format(time.RFC3339Nano)),
		),
	)

	return completionTime, amountUnbonded, nil
}

// CompletePendingUndelegations deletes matured pending undelegation queue and index entries.
// The staking module handles actual token payout to the delegator; we only clean up our tracking state.
func (k Keeper) CompletePendingUndelegations(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()
	completed := 0

	coreStore := k.storeService.OpenKVStore(ctx)
	iterStore := runtime.KVStoreAdapter(coreStore)

	// Iterate all queue entries with completionTime <= blockTime.
	start := types.PendingUndelegationQueueKey
	end := types.GetPendingUndelegationQueueKeyByTime(blockTime)
	endExclusive := append(append([]byte{}, end...), 0xFF)

	iter := iterStore.Iterator(start, endExclusive)
	defer iter.Close() //nolint:errcheck

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		completionTime, err := types.ParsePendingUndelegationQueueKeyForCompletionTime(key)
		if err != nil {
			return err
		}

		var queued types.QueuedUndelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			return err
		}

		// Delete by-validator index entries for each queued undelegation entry.
		for _, entry := range queued.Entries {
			delAddr, err := sdk.AccAddressFromBech32(entry.DelegatorAddress)
			if err != nil {
				return err
			}
			valAddr, err := sdk.ValAddressFromBech32(entry.ValidatorAddress)
			if err != nil {
				return err
			}
			indexKey := types.GetPendingUndelegationByValIndexKey(valAddr, completionTime, entry.Balance.Denom, delAddr)
			if err := coreStore.Delete(indexKey); err != nil {
				return err
			}
			completed++
		}

		// Delete the queue key itself.
		iterStore.Delete(key)
	}

	if completed > 0 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeUndelegationsCompleted,
				sdk.NewAttribute(types.AttributeKeyCount, strconv.Itoa(completed)),
				sdk.NewAttribute(types.AttributeKeyCompletionTime, blockTime.UTC().Format(time.RFC3339Nano)),
			),
		)
	}

	return nil
}
