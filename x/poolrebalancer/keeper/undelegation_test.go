package keeper

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

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

func TestCompletePendingUndelegations_CreditsPoolBeforeDelete(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin("stake", math.NewInt(123))
	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

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

	err := k.CompletePendingUndelegations(ctx)
	require.Error(t, err)
}
