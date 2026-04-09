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

var maturedPoolUndelegationCreditTransientKey = []byte{0x01}

func normalizeCompletionTime(t time.Time) time.Time {
	// Strip monotonic component and force UTC for deterministic equality checks.
	return t.Round(0).UTC()
}

func completionTimeMatches(a, b time.Time) bool {
	return normalizeCompletionTime(a).Equal(normalizeCompletionTime(b))
}

// sumStakingUnbondingEntriesMatchingCompletion returns the sum of balances on staking unbonding
// delegation entries for (del, val) whose completion matches keyCompletion (normalized). Used for
// module-queue credit and expected pending-rebalance accounting; matches merged UBD rows.
func (k Keeper) sumStakingUnbondingEntriesMatchingCompletion(
	ctx context.Context,
	del sdk.AccAddress,
	valAddr sdk.ValAddress,
	keyCompletion time.Time,
) (math.Int, error) {
	ubd, err := k.stakingKeeper.GetUnbondingDelegation(ctx, del, valAddr)
	if err != nil {
		return math.Int{}, fmt.Errorf("get unbonding delegation: %w", err)
	}
	tripleBalance := math.ZeroInt()
	found := false
	for _, entry := range ubd.Entries {
		if completionTimeMatches(entry.CompletionTime, keyCompletion) {
			tripleBalance = tripleBalance.Add(entry.Balance)
			found = true
		}
	}
	if !found {
		return math.Int{}, fmt.Errorf(
			"missing unbonding entry for completion %s",
			normalizeCompletionTime(keyCompletion).Format(time.RFC3339Nano),
		)
	}
	return tripleBalance, nil
}

func (k Keeper) setMaturedPoolUndelegationCreditSum(ctx context.Context, sum math.Int) error {
	if k.transientKey == nil {
		return errors.New("poolrebalancer: transient key is nil")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := sdkCtx.TransientStore(k.transientKey)
	bz, err := sum.Marshal()
	if err != nil {
		return fmt.Errorf("marshal matured undelegation credit sum: %w", err)
	}
	store.Set(maturedPoolUndelegationCreditTransientKey, bz)
	return nil
}

func (k Keeper) getMaturedPoolUndelegationCreditSum(ctx context.Context) (math.Int, error) {
	if k.transientKey == nil {
		return math.Int{}, errors.New("poolrebalancer: transient key is nil")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bz := sdkCtx.TransientStore(k.transientKey).Get(maturedPoolUndelegationCreditTransientKey)
	if len(bz) == 0 {
		return math.Int{}, errors.New("poolrebalancer: missing matured pool undelegation credit snapshot (PrepareMaturedPoolUndelegationCredits not run?)")
	}
	var sum math.Int
	if err := sum.Unmarshal(bz); err != nil {
		return math.Int{}, fmt.Errorf("unmarshal matured undelegation credit sum: %w", err)
	}
	return sum, nil
}

// PrepareMaturedPoolUndelegationCredits snapshots slash-adjusted staking unbonding balances for
// matured pool-tracked undelegations and writes the sum into transient store for EndBlock use.
func (k Keeper) PrepareMaturedPoolUndelegationCredits(ctx context.Context) error {
	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if poolDel.Empty() {
		return k.setMaturedPoolUndelegationCreditSum(ctx, math.ZeroInt())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	batches, err := k.loadMaturedUndelegationBatches(ctx, sdkCtx.BlockTime())
	if err != nil {
		return err
	}

	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return fmt.Errorf("bond denom: %w", err)
	}

	type tripleKey struct {
		delegator      string
		validator      string
		completionTime time.Time
	}

	poolBech := poolDel.String()
	seen := make(map[tripleKey]struct{})
	creditSum := math.ZeroInt()

	for _, b := range batches {
		for _, e := range b.queued.Entries {
			if e.DelegatorAddress != poolBech || e.Balance.Denom != bondDenom {
				continue
			}
			key := tripleKey{
				delegator:      poolBech,
				validator:      e.ValidatorAddress,
				completionTime: normalizeCompletionTime(b.completionTime),
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			valAddr, err := sdk.ValAddressFromBech32(e.ValidatorAddress)
			if err != nil {
				return err
			}
			tripleBalance, err := k.sumStakingUnbondingEntriesMatchingCompletion(ctx, poolDel, valAddr, key.completionTime)
			if err != nil {
				return fmt.Errorf("get unbonding delegation for (%s,%s,%s): %w", key.delegator, key.validator, key.completionTime.Format(time.RFC3339Nano), err)
			}
			creditSum = creditSum.Add(tripleBalance)
		}
	}

	return k.setMaturedPoolUndelegationCreditSum(ctx, creditSum)
}

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
	if err := k.markCommunityPoolReconcileDirtyIfPoolDelegator(ctx, del); err != nil {
		return time.Time{}, math.ZeroInt(), err
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

// loadMaturedUndelegationBatches returns queued undelegation batches whose completion time is at or before blockTime.
// Iterator bounds match CompletePendingUndelegations: [PendingUndelegationQueueKey, GetPendingUndelegationQueueKeyByTime(blockTime)+0xFF).
func (k Keeper) loadMaturedUndelegationBatches(ctx context.Context, blockTime time.Time) ([]maturedUndelegationBatch, error) {
	coreStore := k.storeService.OpenKVStore(ctx)
	iterStore := runtime.KVStoreAdapter(coreStore)

	start := types.PendingUndelegationQueueKey
	end := types.GetPendingUndelegationQueueKeyByTime(blockTime)
	endExclusive := append(append([]byte{}, end...), 0xFF)

	iter := iterStore.Iterator(start, endExclusive)
	defer iter.Close() //nolint:errcheck

	var batches []maturedUndelegationBatch
	for ; iter.Valid(); iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		completionTime, err := types.ParsePendingUndelegationQueueKeyForCompletionTime(key)
		if err != nil {
			return nil, err
		}

		var queued types.QueuedUndelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			return nil, err
		}
		batches = append(batches, maturedUndelegationBatch{
			queueKey:       key,
			completionTime: completionTime,
			queued:         queued,
		})
	}
	return batches, nil
}

// loadImmatureUndelegationBatches returns queued undelegation batches whose completion time is strictly
// after blockTime. Iterator range is (matured upper bound, PendingUndelegationByValIndexKey) so only
// 0x21 queue keys are included (not 0x22 validator index keys).
func (k Keeper) loadImmatureUndelegationBatches(ctx context.Context, blockTime time.Time) ([]maturedUndelegationBatch, error) {
	coreStore := k.storeService.OpenKVStore(ctx)
	iterStore := runtime.KVStoreAdapter(coreStore)

	blockTime = normalizeCompletionTime(blockTime)
	maturedEnd := types.GetPendingUndelegationQueueKeyByTime(blockTime)
	start := append(append([]byte(nil), maturedEnd...), 0xFF)
	endExclusive := append([]byte(nil), types.PendingUndelegationByValIndexKey...)

	iter := iterStore.Iterator(start, endExclusive)
	defer iter.Close() //nolint:errcheck

	var batches []maturedUndelegationBatch
	for ; iter.Valid(); iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		completionTime, err := types.ParsePendingUndelegationQueueKeyForCompletionTime(key)
		if err != nil {
			return nil, err
		}
		if !normalizeCompletionTime(completionTime).After(blockTime) {
			continue
		}

		var queued types.QueuedUndelegation
		if err := k.cdc.Unmarshal(iter.Value(), &queued); err != nil {
			return nil, err
		}
		batches = append(batches, maturedUndelegationBatch{
			queueKey:       key,
			completionTime: completionTime,
			queued:         queued,
		})
	}
	return batches, nil
}

// CompletePendingUndelegations credits stakeable principal via creditStakeableFromRebalance using the
// slash-adjusted sum from PrepareMaturedPoolUndelegationCredits, then deletes queue and index entries.
// creditStakeableFromRebalance reduces pendingRebalanceUnbondReserve only (not totalStaked). Reverts if
// contract pending < creditSum. Credit runs before deletes. A positive credit sets the reconcile-dirty flag.
// Staking EndBlock runs before this module so unbonded tokens are liquid.
func (k Keeper) CompletePendingUndelegations(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	coreStore := k.storeService.OpenKVStore(ctx)
	iterStore := runtime.KVStoreAdapter(coreStore)

	batches, err := k.loadMaturedUndelegationBatches(ctx, blockTime)
	if err != nil {
		return err
	}

	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}

	if len(batches) == 0 {
		return nil
	}

	creditSum, err := k.getMaturedPoolUndelegationCreditSum(ctx)
	if err != nil {
		return err
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
		if err := k.setCommunityPoolReconcileDirty(ctx, true); err != nil {
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

	return k.setMaturedPoolUndelegationCreditSum(ctx, math.ZeroInt())
}
