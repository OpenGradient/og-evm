package keeper

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestHasImmatureRedelegationTo_BlocksSrcWhenDstHasIncoming(t *testing.T) {
	ctx, k := newTestKeeper(t)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	denom := "stake"

	entry := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin(denom, math.NewInt(100)),
		CompletionTime:      ctx.BlockTime().Add(time.Hour),
	}
	require.NoError(t, k.SetPendingRedelegation(ctx, entry))

	require.True(t, k.HasImmatureRedelegationTo(ctx, del, dstVal, denom))

	otherVal := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))
	require.False(t, k.HasImmatureRedelegationTo(ctx, del, otherVal, denom))
}

func TestCompletePendingRedelegations_RemovesPrimaryIndexAndQueue(t *testing.T) {
	ctx, k := newTestKeeper(t)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	denom := "stake"

	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin(denom, math.NewInt(10))
	entry := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              coin,
		CompletionTime:      completion,
	}
	require.NoError(t, k.SetPendingRedelegation(ctx, entry))

	primaryKey := types.GetPendingRedelegationKey(del, denom, srcVal, dstVal, completion)
	indexKey := types.GetPendingRedelegationBySrcIndexKey(srcVal, completion, denom, dstVal, del)
	queueKey := types.GetPendingRedelegationQueueKey(completion)

	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(primaryKey)
	require.NoError(t, err)
	require.NotNil(t, bz)

	require.NoError(t, k.CompletePendingRedelegations(ctx))

	bz, err = store.Get(primaryKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = store.Get(indexKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = store.Get(queueKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	// Idempotency: running again should not error.
	require.NoError(t, k.CompletePendingRedelegations(ctx))
}

func TestSetPendingRedelegation_DistinctSourcesDoNotMerge(t *testing.T) {
	ctx, k := newTestKeeper(t)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	srcB := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	denom := "stake"
	completion := ctx.BlockTime().Add(time.Hour)

	entryA := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcA.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin(denom, math.NewInt(10)),
		CompletionTime:      completion,
	}
	entryB := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcB.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin(denom, math.NewInt(15)),
		CompletionTime:      completion,
	}
	require.NoError(t, k.SetPendingRedelegation(ctx, entryA))
	require.NoError(t, k.SetPendingRedelegation(ctx, entryB))

	store := k.storeService.OpenKVStore(ctx)
	keyA := types.GetPendingRedelegationKey(del, denom, srcA, dstVal, completion)
	keyB := types.GetPendingRedelegationKey(del, denom, srcB, dstVal, completion)

	bzA, err := store.Get(keyA)
	require.NoError(t, err)
	require.NotNil(t, bzA)

	bzB, err := store.Get(keyB)
	require.NoError(t, err)
	require.NotNil(t, bzB)
}
