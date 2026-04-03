package keeper

import (
	"bytes"
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

// mockAccountKeeper is an in-memory auth stub for unit tests (e.g. user pubkey rejection).
type mockAccountKeeper struct {
	accounts map[string]sdk.AccountI
}

func newMockAccountKeeper() *mockAccountKeeper {
	return &mockAccountKeeper{accounts: make(map[string]sdk.AccountI)}
}

func (m *mockAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	if m == nil {
		return nil
	}
	return m.accounts[addr.String()]
}

func (m *mockAccountKeeper) SetAccount(_ context.Context, acc sdk.AccountI) {
	m.accounts[acc.GetAddress().String()] = acc
}

func (m *mockAccountKeeper) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return authtypes.NewBaseAccountWithAddress(addr)
}

// newTestKeeper returns a keeper with in-memory auth (mockAccountKeeper) and nil EVM.
// Before SetParams with a non-empty PoolDelegatorAddress, assign k.evmKeeper (e.g. &mockEVMKeeper{})
// unless the test intentionally exercises validation failure or clears EVM after a successful SetParams.
func newTestKeeper(t *testing.T) (sdk.Context, Keeper, *mockAccountKeeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingKeeper := &mockStakingKeeper{}

	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	mockAcc := newMockAccountKeeper()
	k := NewKeeper(cdc, storeService, stakingKeeper, authority, nil, mockAcc)
	return ctx, k, mockAcc
}

// newTestKeeperNilAuthAndEVM matches genesis-style wiring (no auth, no EVM). Non-empty
// pool_delegator_address cannot be persisted on this keeper.
func newTestKeeperNilAuthAndEVM(t *testing.T) (sdk.Context, Keeper) {
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
