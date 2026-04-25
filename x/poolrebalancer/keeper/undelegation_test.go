package keeper

import (
	"bytes"
	"errors"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func readPreparedMaturedUndelegationCreditSum(t *testing.T, ctx sdk.Context, k Keeper) math.Int {
	t.Helper()
	require.NotNil(t, k.transientKey)
	store := ctx.TransientStore(k.transientKey)
	bz := store.Get(maturedPoolUndelegationCreditTransientKey)
	require.NotNil(t, bz)

	var sum math.Int
	require.NoError(t, sum.Unmarshal(bz))
	return sum
}

func TestCompletePendingUndelegations_RemovesQueueAndIndex(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	denom := "stake"
	setPoolDelegatorForTest(t, ctx, &k, del)

	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin(denom, math.NewInt(123))
	entry := types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	queueKey := types.GetPendingUndelegationQueueKey(completion, del)
	indexKey := types.GetPendingUndelegationByValIndexKey(val, completion, denom, del)

	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.NotNil(t, bz)

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		del.String() + "|" + val.String(): {
			DelegatorAddress: del.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: coin.Amount},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	require.NoError(t, k.CompletePendingUndelegations(ctx))

	bz, err = store.Get(queueKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = store.Get(indexKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	// Idempotency.
	require.NoError(t, k.CompletePendingUndelegations(ctx))
}

func TestSetPendingUndelegation_RejectsWithoutPoolDelegator(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))

	err := k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   sdk.UnwrapSDKContext(ctx).BlockTime().Add(time.Hour),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "pending undelegations require PoolDelegatorAddress")
}

func TestSetPendingUndelegation_RejectsDifferentDelegator(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	err := k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: otherDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   sdk.UnwrapSDKContext(ctx).BlockTime().Add(time.Hour),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "must match PoolDelegatorAddress")
}

func TestBeginTrackedUndelegation_RejectsDifferentDelegatorBeforeStakingMutation(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	_, _, err := k.BeginTrackedUndelegation(ctx, otherDel, val, sdk.NewCoin("stake", math.NewInt(1)))

	require.Error(t, err)
	require.Contains(t, err.Error(), "must match PoolDelegatorAddress")
	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	require.Empty(t, mockSK.undelegateRecords)
}

func TestPrepareMaturedPoolUndelegationCredits_ErrWhenMaturedRowsContainDifferentDelegator(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	completion := ctx.BlockTime().Add(-time.Second)
	seedPendingUndelegationUnchecked(t, ctx, k, types.PendingUndelegation{
		DelegatorAddress: otherDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	})

	err := k.PrepareMaturedPoolUndelegationCredits(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must match PoolDelegatorAddress")
}

func TestCompletePendingUndelegations_ErrAndRetainsQueueWhenMaturedRowsContainDifferentDelegator(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	completion := ctx.BlockTime().Add(-time.Second)
	entry := types.PendingUndelegation{
		DelegatorAddress: otherDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	}
	seedPendingUndelegationUnchecked(t, ctx, k, entry)
	require.NoError(t, k.setMaturedPoolUndelegationCreditSum(ctx, math.ZeroInt()))

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must match PoolDelegatorAddress")

	store := k.storeService.OpenKVStore(ctx)
	queueBz, err := store.Get(types.GetPendingUndelegationQueueKey(completion, otherDel))
	require.NoError(t, err)
	require.NotNil(t, queueBz)
	indexBz, err := store.Get(types.GetPendingUndelegationByValIndexKey(val, completion, entry.Balance.Denom, otherDel))
	require.NoError(t, err)
	require.NotNil(t, indexBz)
}

func TestCompletionTimeMatches_NormalizesLocationAndMonotonic(t *testing.T) {
	base := time.Unix(1_700_000_000, 123_456_789).UTC()
	parsed, err := time.Parse(time.RFC3339Nano, base.Format(time.RFC3339Nano))
	require.NoError(t, err)

	// Equivalent instant but with monotonic component on one side.
	withMonotonic := time.Now().UTC()
	withMonotonic = withMonotonic.Add(base.Sub(withMonotonic))

	require.True(t, completionTimeMatches(base, parsed))
	require.True(t, completionTimeMatches(base, withMonotonic))
}

func TestCompletionTimeMatches_DetectsDifferentInstants(t *testing.T) {
	a := time.Unix(1_700_000_000, 0).UTC()
	b := a.Add(time.Nanosecond)

	require.False(t, completionTimeMatches(a, b))
}

func TestLoadMaturedUndelegationBatches_EmptyStore(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))

	batches, err := k.loadMaturedUndelegationBatches(ctx, ctx.BlockTime())
	require.NoError(t, err)
	require.Empty(t, batches)
}

func TestLoadMaturedUndelegationBatches_IncludesMatureExcludesImmature(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, del)

	matureCompletion := ctx.BlockTime().Add(-time.Second)
	immatureCompletion := ctx.BlockTime().Add(time.Hour)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:   matureCompletion,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(99)),
		CompletionTime:   immatureCompletion,
	}))

	batches, err := k.loadMaturedUndelegationBatches(ctx, ctx.BlockTime())
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.True(t, completionTimeMatches(batches[0].completionTime, matureCompletion))
	require.Len(t, batches[0].queued.Entries, 1)
	require.Equal(t, "10", batches[0].queued.Entries[0].Balance.Amount.String())
}

func TestLoadMaturedUndelegationBatches_MultipleDelegatorsSameBlockTime(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	delA := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	delB := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)

	seedPendingUndelegationUnchecked(t, ctx, k, types.PendingUndelegation{
		DelegatorAddress: delA.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	})
	seedPendingUndelegationUnchecked(t, ctx, k, types.PendingUndelegation{
		DelegatorAddress: delB.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(2)),
		CompletionTime:   completion,
	})

	batches, err := k.loadMaturedUndelegationBatches(ctx, ctx.BlockTime())
	require.NoError(t, err)
	require.Len(t, batches, 2)

	sum := math.ZeroInt()
	for _, b := range batches {
		require.True(t, completionTimeMatches(b.completionTime, completion))
		require.Len(t, b.queued.Entries, 1)
		sum = sum.Add(b.queued.Entries[0].Balance.Amount)
	}
	require.True(t, sum.Equal(math.NewInt(3)))
}

func TestPrepareMaturedPoolUndelegationCredits_WritesZeroWhenPoolDelegatorEmpty(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sum := readPreparedMaturedUndelegationCreditSum(t, sdk.UnwrapSDKContext(ctx), k)
	require.True(t, sum.IsZero())
}

func TestPrepareMaturedPoolUndelegationCredits_ErrWhenPoolDelegatorEmptyWithMaturedRows(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{0xAB}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{0xCD}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	denom := "stake"
	coin := sdk.NewCoin(denom, math.NewInt(42))
	seedPendingUndelegationUnchecked(t, ctx, k, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	})

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Empty(t, params.PoolDelegatorAddress)
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))

	err = k.PrepareMaturedPoolUndelegationCredits(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "matured undelegations exist but PoolDelegatorAddress is empty")
	require.Empty(t, mockEVM.methods)
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))
}

func TestCompletePendingUndelegations_ErrWhenPoolDelegatorClearedAfterPrepareRetainsQueue(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(42))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: coin.Amount},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))

	// Simulate a corrupted import/store mutation that bypassed SetParams' pending-state guard.
	cleared := types.DefaultParams()
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(types.ParamsKey, k.cdc.MustMarshal(&cleared)))

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "matured undelegations exist but PoolDelegatorAddress is empty")
	require.Empty(t, mockEVM.methods)

	queueBz, err := store.Get(types.GetPendingUndelegationQueueKey(completion, poolDel))
	require.NoError(t, err)
	require.NotNil(t, queueBz, "queue must remain when config corruption halts cleanup")
	indexBz, err := store.Get(types.GetPendingUndelegationByValIndexKey(val, completion, coin.Denom, poolDel))
	require.NoError(t, err)
	require.NotNil(t, indexBz, "index must remain when config corruption halts cleanup")
}

// Transient sum is credited via creditStakeableFromRebalance (pending reserve), not by lowering totalStaked.
func TestPrepareMaturedPoolUndelegationCredits_UsesStakingBalanceForSlashAlignment(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	k.evmKeeper = &mockEVMKeeper{}

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(100)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{
					CompletionTime: completion,
					Balance:        math.NewInt(90),
				},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sum := readPreparedMaturedUndelegationCreditSum(t, sdk.UnwrapSDKContext(ctx), k)
	require.Equal(t, "90", sum.String())
}

func TestPrepareMaturedPoolUndelegationCredits_DedupesByTriple(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	k.evmKeeper = &mockEVMKeeper{}

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(30)),
		CompletionTime:   completion,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(70)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{
					CompletionTime: completion,
					Balance:        math.NewInt(50),
				},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sum := readPreparedMaturedUndelegationCreditSum(t, sdk.UnwrapSDKContext(ctx), k)
	require.Equal(t, "50", sum.String())
	require.Equal(t, 1, mockSK.getUBDCalls)
}

// TestPrepareAndComplete_TwoUBDEntriesSameValidator_DifferentCompletionsBothMature verifies that two
// pending queue rows at different completion times for the same (pool delegator, validator) each
// pick up the matching staking UnbondingDelegation entry balance and the EndBlock credit is the sum.
func TestPrepareAndComplete_TwoUBDEntriesSameValidator_DifferentCompletionsBothMature(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completionEarly := ctx.BlockTime().Add(-2 * time.Second)
	completionLate := ctx.BlockTime().Add(-time.Second)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(999)),
		CompletionTime:   completionEarly,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(888)),
		CompletionTime:   completionLate,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completionEarly, Balance: math.NewInt(40)},
				{CompletionTime: completionLate, Balance: math.NewInt(55)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sum := readPreparedMaturedUndelegationCreditSum(t, sdk.UnwrapSDKContext(ctx), k)
	require.Equal(t, "95", sum.String(), "credit sum must be both UBD entry balances")
	require.Equal(t, 2, mockSK.getUBDCalls, "one GetUnbondingDelegation per distinct completion triple")

	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{"creditStakeableFromRebalance"}, mockEVM.methods)
	require.Len(t, mockEVM.args, 1)
	amount, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "95", amount.String())

	store := k.storeService.OpenKVStore(ctx)
	for _, completion := range []time.Time{completionEarly, completionLate} {
		qk := types.GetPendingUndelegationQueueKey(completion, poolDel)
		bz, err := store.Get(qk)
		require.NoError(t, err)
		require.Nil(t, bz, "queue key at completion %v should be removed", completion)
		ik := types.GetPendingUndelegationByValIndexKey(val, completion, "stake", poolDel)
		bz, err = store.Get(ik)
		require.NoError(t, err)
		require.Nil(t, bz, "index at completion %v should be removed", completion)
	}
}

// TestPrepareAndComplete_MultipleUBDEntriesSameCompletionTime verifies that when staking keeps more than
// one UnbondingDelegationEntry with the same CompletionTime (different CreationHeight — possible when
// block header times repeat), Prepare sums all their balances for one queue triple and Complete credits once.
func TestPrepareAndComplete_MultipleUBDEntriesSameCompletionTime(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(500)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CreationHeight: 10, CompletionTime: completion, Balance: math.NewInt(40)},
				{CreationHeight: 11, CompletionTime: completion, Balance: math.NewInt(55)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sum := readPreparedMaturedUndelegationCreditSum(t, sdk.UnwrapSDKContext(ctx), k)
	require.Equal(t, "95", sum.String(), "credit must sum both UBD entries maturing at this completion")
	require.Equal(t, 1, mockSK.getUBDCalls)

	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{"creditStakeableFromRebalance"}, mockEVM.methods)
	require.Len(t, mockEVM.args, 1)
	amount, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "95", amount.String())
}

func TestPrepareMaturedPoolUndelegationCredits_ErrOnMissingUBD(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	k.evmKeeper = &mockEVMKeeper{}

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:   completion,
	}))

	err := k.PrepareMaturedPoolUndelegationCredits(ctx)
	require.Error(t, err)
}

func TestPrepareMaturedPoolUndelegationCredits_ErrWhenStakingUBDCompletionDesynced(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	k.evmKeeper = &mockEVMKeeper{}

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion.Add(time.Hour), Balance: math.NewInt(10)},
			},
		},
	}

	err := k.PrepareMaturedPoolUndelegationCredits(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing unbonding entry for completion")

	store := k.storeService.OpenKVStore(ctx)
	queueBz, err := store.Get(types.GetPendingUndelegationQueueKey(completion, poolDel))
	require.NoError(t, err)
	require.NotNil(t, queueBz, "desynced queue must remain for operator repair")
}

func TestCompletePendingUndelegations_CreditsPoolBeforeDelete(t *testing.T) {
	// Mock EVM has no storage; on-chain, pendingRebalanceUnbondReserve must cover the credit (prior reconciles).
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(123))
	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(123)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{"creditStakeableFromRebalance"}, mockEVM.methods)
	require.Len(t, mockEVM.args, 1)
	amount, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "123", amount.String())

	store := k.storeService.OpenKVStore(ctx)
	queueKey := types.GetPendingUndelegationQueueKey(completion, poolDel)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.Nil(t, bz)
}

func TestCompletePendingUndelegations_RetainsQueueOnCreditVMFailure(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		failedVM: map[string]string{
			"creditStakeableFromRebalance": "execution reverted",
		},
	}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx), "dirty only after successful credit")

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(50))
	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(50)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	preparedSum := readPreparedMaturedUndelegationCreditSum(t, sdkCtx, k)
	require.Equal(t, "50", preparedSum.String())

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
	require.False(t, k.getCommunityPoolReconcileDirty(ctx), "failed credit must not set reconcile dirty")

	store := k.storeService.OpenKVStore(ctx)
	queueKey := types.GetPendingUndelegationQueueKey(completion, poolDel)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.NotNil(t, bz)

	indexKey := types.GetPendingUndelegationByValIndexKey(val, completion, coin.Denom, poolDel)
	idxBz, err := store.Get(indexKey)
	require.NoError(t, err)
	require.NotNil(t, idxBz, "validator index entry must remain until credit+delete succeed")

	afterFailSum := readPreparedMaturedUndelegationCreditSum(t, sdkCtx, k)
	require.True(t, afterFailSum.Equal(preparedSum), "transient snapshot must not be cleared on Complete error")
}

// TestCompletePendingUndelegations_RetainsQueueOnCreditCallEVMError covers CallEVM returning (nil, err)
// before MsgEthereumTxResponse is inspected (transport / keeper error path). Transient credit sums are
// cosmossdk.io/math.Int (256-bit bounded); values that do not fit a Solidity uint256 cannot be stored and
// are covered at the coercion helper level in TestCommunityPoolStakeBucketBigInt.
func TestCompletePendingUndelegations_RetainsQueueOnCreditCallEVMError(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		errByMethod: map[string]error{
			"creditStakeableFromRebalance": errors.New("mock call evm transport failure"),
		},
	}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(77))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(77)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	preparedSum := readPreparedMaturedUndelegationCreditSum(t, sdkCtx, k)
	require.Equal(t, "77", preparedSum.String())

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
	require.False(t, k.getCommunityPoolReconcileDirty(ctx), "transport error before credit must not set dirty")

	store := k.storeService.OpenKVStore(ctx)
	queueKey := types.GetPendingUndelegationQueueKey(completion, poolDel)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.NotNil(t, bz)

	indexKey := types.GetPendingUndelegationByValIndexKey(val, completion, coin.Denom, poolDel)
	idxBz, err := store.Get(indexKey)
	require.NoError(t, err)
	require.NotNil(t, idxBz)

	afterFailSum := readPreparedMaturedUndelegationCreditSum(t, sdkCtx, k)
	require.True(t, afterFailSum.Equal(preparedSum))
}

// TestCompletePendingUndelegations_RetryAfterCreditVMFailureSucceeds proves the same BeginBlock transient
// snapshot can complete after the EVM credit path starts succeeding (e.g. same EndBlock retry is not required
// to re-run Prepare if the context still holds the prepared sum).
func TestCompletePendingUndelegations_RetryAfterCreditVMFailureSucceeds(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		failedVM: map[string]string{
			"creditStakeableFromRebalance": "execution reverted",
		},
	}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(33))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(33)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	require.Error(t, k.CompletePendingUndelegations(ctx))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx), "first failed credit must not set dirty")

	mockEVM.failedVM = nil
	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{
		"creditStakeableFromRebalance",
		"creditStakeableFromRebalance",
	}, mockEVM.methods)
	amt0, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "33", amt0.String())
	amt1, ok := mockEVM.args[1][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "33", amt1.String())

	store := k.storeService.OpenKVStore(ctx)
	queueKey := types.GetPendingUndelegationQueueKey(completion, poolDel)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	finalSum := readPreparedMaturedUndelegationCreditSum(t, sdkCtx, k)
	require.True(t, finalSum.IsZero(), "transient cleared after successful Complete")
	require.True(t, k.getCommunityPoolReconcileDirty(ctx), "successful credit sets reconcile dirty")
}

func TestSetPendingUndelegation_RejectsNonBondDenom(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	err := k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("otherdenom", math.NewInt(777)),
		CompletionTime:   ctx.BlockTime().Add(time.Hour),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), `pending undelegation denom "otherdenom" must match bond denom "stake"`)
}

func TestPrepareMaturedPoolUndelegationCredits_ErrWhenMaturedRowsContainNonBondDenom(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	completion := ctx.BlockTime().Add(-time.Second)
	seedPendingUndelegationUnchecked(t, ctx, k, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("otherdenom", math.NewInt(1)),
		CompletionTime:   completion,
	})

	err := k.PrepareMaturedPoolUndelegationCredits(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), `pending undelegation denom "otherdenom" must match bond denom "stake"`)
}

func TestCompletePendingUndelegations_ErrAndRetainsQueueWhenMaturedRowsContainNonBondDenom(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	completion := ctx.BlockTime().Add(-time.Second)
	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("otherdenom", math.NewInt(1)),
		CompletionTime:   completion,
	}
	seedPendingUndelegationUnchecked(t, ctx, k, entry)
	require.NoError(t, k.setMaturedPoolUndelegationCreditSum(ctx, math.ZeroInt()))

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), `pending undelegation denom "otherdenom" must match bond denom "stake"`)

	store := k.storeService.OpenKVStore(ctx)
	queueBz, err := store.Get(types.GetPendingUndelegationQueueKey(completion, poolDel))
	require.NoError(t, err)
	require.NotNil(t, queueBz)
	indexBz, err := store.Get(types.GetPendingUndelegationByValIndexKey(val, completion, entry.Balance.Denom, poolDel))
	require.NoError(t, err)
	require.NotNil(t, indexBz)
}

func TestCompletePendingUndelegations_ErrWhenPoolCreditRequiresEVMButNil(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))
	k.evmKeeper = nil

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(1)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	k.evmKeeper = nil
	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
}

func TestCompletePendingUndelegations_ErrWhenSnapshotMissing(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, del)
	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	}))

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
}

func TestCompletePendingUndelegations_CreditsSlashAlignedNotQueueBalance(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	completion := ctx.BlockTime().Add(-time.Second)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(100)),
		CompletionTime:   completion,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completion, Balance: math.NewInt(63)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{"creditStakeableFromRebalance"}, mockEVM.methods)
	amount, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "63", amount.String())
}
