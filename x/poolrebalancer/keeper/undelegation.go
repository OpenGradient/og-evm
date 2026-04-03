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

// maturedUndelegationBatch is a snapshot of one queue key and its unmarshaled entries.
type maturedUndelegationBatch struct {
	queueKey       []byte
	completionTime time.Time
	queued         types.QueuedUndelegation
}

// CompletePendingUndelegations credits CommunityPool stakeable principal for matured module-tracked
// undelegations, then deletes queue and index entries. The EVM call also reduces CommunityPool.totalStaked
// by the credited amount so principal NAV matches module undelegations that bypass withdraw().
// Credit runs before deletes so a failed EVM call retains queue state for retry. The staking module pays
// out liquid tokens before this runs (staking EndBlock).
func (k Keeper) CompletePendingUndelegations(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	coreStore := k.storeService.OpenKVStore(ctx)
	iterStore := runtime.KVStoreAdapter(coreStore)

	start := types.PendingUndelegationQueueKey
	end := types.GetPendingUndelegationQueueKeyByTime(blockTime)
	endExclusive := append(append([]byte{}, end...), 0xFF)

	iter := iterStore.Iterator(start, endExclusive)
	var batches []maturedUndelegationBatch
	for ; iter.Valid(); iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		completionTime, err := types.ParsePendingUndelegationQueueKeyForCompletionTime(key)
		if err != nil {
			iter.Close() //nolint:errcheck
			return err
		}

		var queued types.QueuedUndelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			iter.Close() //nolint:errcheck
			return err
		}
		batches = append(batches, maturedUndelegationBatch{
			queueKey:       key,
			completionTime: completionTime,
			queued:         queued,
		})
	}
	iter.Close() //nolint:errcheck

	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}

	var bondDenom string
	if !poolDel.Empty() {
		bondDenom, err = k.stakingKeeper.BondDenom(ctx)
		if err != nil {
			return fmt.Errorf("bond denom: %w", err)
		}
	}

	creditSum := math.ZeroInt()
	if !poolDel.Empty() {
		poolBech := poolDel.String()
		for _, b := range batches {
			for _, e := range b.queued.Entries {
				if e.DelegatorAddress == poolBech && e.Balance.Denom == bondDenom {
					creditSum = creditSum.Add(e.Balance.Amount)
				}
			}
		}
	}

	if creditSum.IsPositive() {
		if k.evmKeeper == nil {
			return fmt.Errorf("poolrebalancer: matured pool undelegations %s require evm keeper", creditSum)
		}
		if poolDel.Empty() {
			return fmt.Errorf("poolrebalancer: matured pool undelegations %s require PoolDelegatorAddress", creditSum)
		}
		if err := k.creditCommunityPoolStakeableFromRebalance(sdkCtx, poolDel, creditSum); err != nil {
			return err
		}
	}

	completed := 0
	for _, b := range batches {
		for _, entry := range b.queued.Entries {
			delAddr, err := sdk.AccAddressFromBech32(entry.DelegatorAddress)
			if err != nil {
				return err
			}
			valAddr, err := sdk.ValAddressFromBech32(entry.ValidatorAddress)
			if err != nil {
				return err
			}
			indexKey := types.GetPendingUndelegationByValIndexKey(valAddr, b.completionTime, entry.Balance.Denom, delAddr)
			if err := coreStore.Delete(indexKey); err != nil {
				return err
			}
			completed++
		}
		iterStore.Delete(b.queueKey)
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
