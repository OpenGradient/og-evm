package keeper

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func newKeeperWithStaking(t *testing.T, sk types.StakingKeeper) (sdk.Context, Keeper) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0).UTC())
	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := NewKeeper(cdc, storeService, tKey, sk, nil, authority, nil, newMockAccountKeeper())
	return ctx, k
}

func TestLoadImmatureUndelegationBatches_ExcludesMatureIncludesFuture(t *testing.T) {
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

	batches, err := k.loadImmatureUndelegationBatches(ctx, ctx.BlockTime())
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.True(t, normalizeCompletionTime(batches[0].completionTime).After(normalizeCompletionTime(ctx.BlockTime())))
	require.Len(t, batches[0].queued.Entries, 1)
	require.Equal(t, "99", batches[0].queued.Entries[0].Balance.Amount.String())
}

func TestComputeExpectedBondedPrincipal_SkipsNonBondedValidators(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	bondedVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	unbondingVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	bondedV := stakingtypes.Validator{
		OperatorAddress: bondedVal.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	unbondingV := stakingtypes.Validator{
		OperatorAddress: unbondingVal.String(),
		Tokens:          math.NewInt(500),
		DelegatorShares: math.LegacyNewDec(500),
		Status:          stakingtypes.Unbonding,
	}
	sk := &mockStakingKeeper{
		validatorByAddr: map[string]stakingtypes.Validator{
			bondedVal.String():    bondedV,
			unbondingVal.String(): unbondingV,
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(100)},
			{DelegatorAddress: del.String(), ValidatorAddress: unbondingVal.String(), Shares: math.LegacyNewDec(50)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			bondedVal.String():    {DelegatorAddress: del.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(100)},
			unbondingVal.String(): {DelegatorAddress: del.String(), ValidatorAddress: unbondingVal.String(), Shares: math.LegacyNewDec(50)},
		},
	}
	ctx, k := newKeeperWithStaking(t, sk)
	sum, err := k.ComputeExpectedBondedPrincipal(ctx, del)
	require.NoError(t, err)
	require.Equal(t, "100", sum.String())
}

func TestGetDelegatorDelegationsWithLimit_ErrorsAtScanLimit(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	sk := &mockStakingKeeper{
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(1)},
			{DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(1)},
		},
	}
	ctx, k := newKeeperWithStaking(t, sk)

	_, err := k.getDelegatorDelegationsWithLimit(ctx, del, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "possibly truncated staking view")
}

func TestGetDelegatorDelegationsWithLimit_AllowsBelowScanLimit(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	sk := &mockStakingKeeper{
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: val.String(), Shares: math.LegacyNewDec(1)},
		},
	}
	ctx, k := newKeeperWithStaking(t, sk)

	delegations, err := k.getDelegatorDelegationsWithLimit(ctx, del, 2)
	require.NoError(t, err)
	require.Len(t, delegations, 1)
}

func TestComputeExpectedPendingRebalancePrincipal_UsesStakingUBDAndDedupes(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	setPoolDelegatorForTest(t, ctx, &k, del)

	immatureCompletion := ctx.BlockTime().Add(time.Hour)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   immatureCompletion,
	}))

	ubd := stakingtypes.UnbondingDelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{Balance: math.NewInt(77), CompletionTime: immatureCompletion},
		},
	}
	sk, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	sk.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		del.String() + "|" + val.String(): ubd,
	}

	pending, err := k.ComputeExpectedPendingRebalancePrincipal(ctx, del, ctx.BlockTime())
	require.NoError(t, err)
	require.Equal(t, "77", pending.String())
}

func TestComputeExpectedBondedPrincipal_SkipsUnbondedValidator(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	bondedVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	unbondedVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	bondedV := stakingtypes.Validator{
		OperatorAddress: bondedVal.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	unbondedV := stakingtypes.Validator{
		OperatorAddress: unbondedVal.String(),
		Tokens:          math.NewInt(200),
		DelegatorShares: math.LegacyNewDec(200),
		Status:          stakingtypes.Unbonded,
	}
	sk := &mockStakingKeeper{
		validatorByAddr: map[string]stakingtypes.Validator{
			bondedVal.String():   bondedV,
			unbondedVal.String(): unbondedV,
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(30)},
			{DelegatorAddress: del.String(), ValidatorAddress: unbondedVal.String(), Shares: math.LegacyNewDec(200)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			bondedVal.String():   {DelegatorAddress: del.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(30)},
			unbondedVal.String(): {DelegatorAddress: del.String(), ValidatorAddress: unbondedVal.String(), Shares: math.LegacyNewDec(200)},
		},
	}
	ctx, k := newKeeperWithStaking(t, sk)
	sum, err := k.ComputeExpectedBondedPrincipal(ctx, del)
	require.NoError(t, err)
	require.Equal(t, "30", sum.String())
}

func TestComputeExpectedPendingRebalancePrincipal_SumsMergedUBDEntriesPerTriple(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(3_000, 0).UTC())
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	immatureCT := ctx.BlockTime().Add(2 * time.Hour)
	setPoolDelegatorForTest(t, ctx, &k, del)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   immatureCT,
	}))

	ubd := stakingtypes.UnbondingDelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{Balance: math.NewInt(40), CompletionTime: immatureCT},
			{Balance: math.NewInt(7), CompletionTime: immatureCT},
		},
	}
	sk, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	sk.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		del.String() + "|" + val.String(): ubd,
	}

	pending, err := k.ComputeExpectedPendingRebalancePrincipal(ctx, del, ctx.BlockTime())
	require.NoError(t, err)
	require.Equal(t, "47", pending.String())
}

func TestComputeExpectedPendingRebalancePrincipal_DedupesDuplicateQueueRowsForSameTriple(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(4_000, 0).UTC())
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	immatureCT := ctx.BlockTime().Add(time.Minute)
	setPoolDelegatorForTest(t, ctx, &k, del)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(9)),
		CompletionTime:   immatureCT,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(11)),
		CompletionTime:   immatureCT,
	}))

	ubd := stakingtypes.UnbondingDelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Entries: []stakingtypes.UnbondingDelegationEntry{
			{Balance: math.NewInt(100), CompletionTime: immatureCT},
		},
	}
	sk, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	sk.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		del.String() + "|" + val.String(): ubd,
	}

	pending, err := k.ComputeExpectedPendingRebalancePrincipal(ctx, del, ctx.BlockTime())
	require.NoError(t, err)
	require.Equal(t, "100", pending.String())
}

func TestComputeExpectedPendingRebalancePrincipal_IgnoresNonQueuedUnbondingValidators(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(5_000, 0).UTC())
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	queuedVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	otherVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	immatureCT := ctx.BlockTime().Add(time.Hour)
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: queuedVal.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   immatureCT,
	}))

	sk, ok := k.stakingKeeper.(*mockStakingKeeper)
	require.True(t, ok)
	sk.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + queuedVal.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: queuedVal.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{Balance: math.NewInt(33), CompletionTime: immatureCT},
			},
		},
		poolDel.String() + "|" + otherVal.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: otherVal.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{Balance: math.NewInt(9_999), CompletionTime: immatureCT},
			},
		},
	}

	pending, err := k.ComputeExpectedPendingRebalancePrincipal(ctx, poolDel, ctx.BlockTime())
	require.NoError(t, err)
	require.Equal(t, "33", pending.String())
}
