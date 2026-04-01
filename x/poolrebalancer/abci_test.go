package poolrebalancer

import (
	"bytes"
	"testing"
	"time"

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

func newEndBlockerTestKeeper(t *testing.T) (sdk.Context, keeper.Keeper, *storetypes.KVStoreKey) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingKeeper := &stakingkeeper.Keeper{} // zero value; tests avoid staking calls
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))

	k := keeper.NewKeeper(cdc, storeService, stakingKeeper, authority, nil)
	return ctx, k, storeKey
}

func TestEndBlocker_OperationalErrorIsNonHalting(t *testing.T) {
	ctx, k, storeKey := newEndBlockerTestKeeper(t)

	// Inject malformed params directly to force an operational module error
	// (GetParams unmarshal failure) while keeping cleanup paths healthy.
	// This now first impacts community pool automation lookup in EndBlock.
	ctx.KVStore(storeKey).Set(types.ParamsKey, []byte("not-a-valid-proto"))

	err := EndBlocker(ctx, k)
	require.NoError(t, err, "operational errors should not halt EndBlocker")
}

func TestEndBlocker_CleanupErrorRemainsHalting(t *testing.T) {
	ctx, k, storeKey := newEndBlockerTestKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	// Seed an invalid queued redelegation value so cleanup fails on unmarshal.
	maturedKey := types.GetPendingRedelegationQueueKey(now.Add(-time.Second))
	ctx.KVStore(storeKey).Set(maturedKey, []byte("not-a-valid-proto"))

	err := EndBlocker(ctx, k)
	require.Error(t, err, "cleanup failures should remain halting")
}

