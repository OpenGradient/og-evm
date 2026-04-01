package poolrebalancer

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestGenesis_ExportsAndRestoresPendingState(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(2_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := &stakingkeeper.Keeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, stakingK, authority, nil, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:      ctx.BlockTime().Add(time.Hour),
	}))

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: srcVal.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(5)),
		CompletionTime:   ctx.BlockTime().Add(2 * time.Hour),
	}))

	exported := ExportGenesis(ctx, k)
	require.NotNil(t, exported)
	require.Len(t, exported.PendingRedelegations, 1)
	require.Len(t, exported.PendingUndelegations, 1)

	// Restore into a fresh store/keeper.
	storeKey2 := storetypes.NewKVStoreKey(types.ModuleName)
	tKey2 := storetypes.NewTransientStoreKey("transient_test2")
	ctx2 := testutil.DefaultContext(storeKey2, tKey2).WithBlockTime(time.Unix(2_000, 0))

	storeService2 := runtime.NewKVStoreService(storeKey2)
	k2 := keeper.NewKeeper(cdc, storeService2, stakingK, authority, nil, nil)

	InitGenesis(ctx2, k2, exported)

	redels, err := k2.GetAllPendingRedelegations(ctx2)
	require.NoError(t, err)
	undels, err := k2.GetAllPendingUndelegations(ctx2)
	require.NoError(t, err)

	require.Len(t, redels, 1)
	require.Len(t, undels, 1)
	require.Equal(t, exported.PendingRedelegations[0].DelegatorAddress, redels[0].DelegatorAddress)
	require.Equal(t, exported.PendingUndelegations[0].DelegatorAddress, undels[0].DelegatorAddress)
}

func TestGenesis_RoundTripPreservesDistinctRedelegationSources(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(3_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := &stakingkeeper.Keeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, stakingK, authority, nil, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	srcB := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))
	dst := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	completion := ctx.BlockTime().Add(time.Hour)

	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcA.String(),
		DstValidatorAddress: dst.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:      completion,
	}))
	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcB.String(),
		DstValidatorAddress: dst.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(15)),
		CompletionTime:      completion,
	}))

	exported := ExportGenesis(ctx, k)
	require.Len(t, exported.PendingRedelegations, 2)

	// Restore into a fresh store/keeper and ensure both source-specific entries survive.
	storeKey2 := storetypes.NewKVStoreKey(types.ModuleName)
	tKey2 := storetypes.NewTransientStoreKey("transient_test2")
	ctx2 := testutil.DefaultContext(storeKey2, tKey2).WithBlockTime(time.Unix(3_000, 0))
	k2 := keeper.NewKeeper(cdc, runtime.NewKVStoreService(storeKey2), stakingK, authority, nil, nil)
	InitGenesis(ctx2, k2, exported)

	redels, err := k2.GetAllPendingRedelegations(ctx2)
	require.NoError(t, err)
	require.Len(t, redels, 2)

	srcSet := map[string]struct{}{}
	for _, e := range redels {
		srcSet[e.SrcValidatorAddress] = struct{}{}
		require.Equal(t, dst.String(), e.DstValidatorAddress)
		require.Equal(t, completion.UTC(), e.CompletionTime.UTC())
	}
	_, hasA := srcSet[srcA.String()]
	_, hasB := srcSet[srcB.String()]
	require.True(t, hasA)
	require.True(t, hasB)
}
