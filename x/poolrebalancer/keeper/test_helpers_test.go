package keeper

import (
	"bytes"
	"testing"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func newTestKeeper(t *testing.T) (sdk.Context, Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingKeeper := &mockStakingKeeper{}

	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := NewKeeper(cdc, storeService, stakingKeeper, authority, nil, nil)
	return ctx, k
}
