package keeper

import (
	"bytes"
	"testing"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func newTestKeeper(t *testing.T) (sdk.Context, Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingKeeper := &stakingkeeper.Keeper{} // zero value; do not call staking methods in unit tests

	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := NewKeeper(cdc, storeService, stakingKeeper, authority, nil)
	return ctx, k
}
