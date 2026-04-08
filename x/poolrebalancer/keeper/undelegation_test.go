package keeper

import (
	"bytes"
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

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: delA.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: delB.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(2)),
		CompletionTime:   completion,
	}))

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

func TestCompletePendingUndelegations_CreditsPoolBeforeDelete(t *testing.T) {
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
	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)

	store := k.storeService.OpenKVStore(ctx)
	queueKey := types.GetPendingUndelegationQueueKey(completion, poolDel)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.NotNil(t, bz)
}

func TestCompletePendingUndelegations_SumsOnlyPoolDelegatorBondDenom(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completionA := ctx.BlockTime().Add(-2 * time.Second)
	completionB := ctx.BlockTime().Add(-time.Second)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(40)),
		CompletionTime:   completionA,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: otherDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(999)),
		CompletionTime:   completionB,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("otherdenom", math.NewInt(777)),
		CompletionTime:   completionB,
	}))

	mockSK, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{CompletionTime: completionA, Balance: math.NewInt(40)},
			},
		},
	}

	require.NoError(t, k.PrepareMaturedPoolUndelegationCredits(ctx))
	require.NoError(t, k.CompletePendingUndelegations(ctx))

	require.Equal(t, []string{"creditStakeableFromRebalance"}, mockEVM.methods)
	amount, ok := mockEVM.args[0][0].(*big.Int)
	require.True(t, ok)
	require.Equal(t, "40", amount.String())
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
