package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	evmencoding "github.com/cosmos/evm/encoding"
	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/cosmos/evm/x/svip/types"
	"github.com/cosmos/evm/x/svip/types/mocks"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type testData struct {
	ctx      sdk.Context
	keeper   keeper.Keeper
	storeKey *storetypes.KVStoreKey
	bk       *mocks.BankKeeper
	ak       *mocks.AccountKeeper
}

func newMockedTestData(t *testing.T) testData {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey) //nolint: staticcheck

	bk := mocks.NewBankKeeper(t)
	ak := mocks.NewAccountKeeper(t)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)

	chainID := testconstants.SixDecimalsChainID.EVMChainID
	cfg := evmencoding.MakeConfig(chainID)
	cdc := cfg.Codec
	k := keeper.NewKeeper(cdc, storeKey, authority, bk, ak) //nolint: staticcheck
	evmConfigurator := vmtypes.NewEVMConfigurator().
		WithEVMCoinInfo(testconstants.ExampleChainCoinInfo[testconstants.SixDecimalsChainID])
	evmConfigurator.ResetTestConfig()
	err := evmConfigurator.Configure()
	require.NoError(t, err)

	return testData{
		ctx:      ctx,
		keeper:   k,
		storeKey: storeKey,
		bk:       bk,
		ak:       ak,
	}
}

func TestGetSetParams(t *testing.T) {
	td := newMockedTestData(t)

	// Default params when nothing set
	params := td.keeper.GetParams(td.ctx)
	require.Equal(t, types.DefaultParams(), params)

	// Set custom params and read back
	custom := types.Params{
		Activated:       true,
		Paused:          false,
		HalfLifeSeconds: 31536000,
	}
	require.NoError(t, td.keeper.SetParams(td.ctx, custom))
	got := td.keeper.GetParams(td.ctx)
	require.Equal(t, custom, got)
}

func TestGetSetTotalDistributed(t *testing.T) {
	td := newMockedTestData(t)

	// Default is zero
	require.True(t, td.keeper.GetTotalDistributed(td.ctx).IsZero())

	// Set and read back
	td.keeper.SetTotalDistributed(td.ctx, sdkmath.NewInt(500))
	require.Equal(t, sdkmath.NewInt(500), td.keeper.GetTotalDistributed(td.ctx))

	// AddTotalDistributed accumulates
	td.keeper.AddTotalDistributed(td.ctx, sdkmath.NewInt(300))
	require.Equal(t, sdkmath.NewInt(800), td.keeper.GetTotalDistributed(td.ctx))
}

func TestGetSetActivationTime(t *testing.T) {
	td := newMockedTestData(t)

	// Default is zero time
	require.True(t, td.keeper.GetActivationTime(td.ctx).IsZero())

	// Set and read back
	now := time.Now().UTC().Truncate(time.Second)
	td.keeper.SetActivationTime(td.ctx, now)
	require.Equal(t, now, td.keeper.GetActivationTime(td.ctx).UTC().Truncate(time.Second))
}

func TestGetSetLastBlockTime(t *testing.T) {
	td := newMockedTestData(t)

	// Default is zero time
	require.True(t, td.keeper.GetLastBlockTime(td.ctx).IsZero())

	// Set and read back
	now := time.Now().UTC().Truncate(time.Second)
	td.keeper.SetLastBlockTime(td.ctx, now)
	require.Equal(t, now, td.keeper.GetLastBlockTime(td.ctx).UTC().Truncate(time.Second))
}

func TestGetSetPoolBalanceAtActivation(t *testing.T) {
	td := newMockedTestData(t)

	// Default is zero
	require.True(t, td.keeper.GetPoolBalanceAtActivation(td.ctx).IsZero())

	// Set and read back
	val := sdkmath.NewInt(1_000_000_000)
	td.keeper.SetPoolBalanceAtActivation(td.ctx, val)
	require.Equal(t, val, td.keeper.GetPoolBalanceAtActivation(td.ctx))
}

func TestBeginBlock_NotActivated(t *testing.T) {
	td := newMockedTestData(t)

	// Default params: not activated — should skip without any bank calls
	err := td.keeper.BeginBlock(td.ctx)
	require.NoError(t, err)

	// Bank keeper should not have been called
	td.bk.AssertNotCalled(t, "SendCoinsFromModuleToModule")
}

func TestBeginBlock_Paused(t *testing.T) {
	td := newMockedTestData(t)

	params := types.Params{Activated: true, Paused: true, HalfLifeSeconds: 31536000}
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	err := td.keeper.BeginBlock(td.ctx)
	require.NoError(t, err)

	td.bk.AssertNotCalled(t, "SendCoinsFromModuleToModule")
}

func TestBeginBlock_Distributes(t *testing.T) {
	td := newMockedTestData(t)

	denom := vmtypes.GetEVMCoinDenom()
	halfLife := int64(31536000)
	poolBalance := sdkmath.NewInt(1_000_000_000_000)

	// Set params as activated
	params := types.Params{Activated: true, Paused: false, HalfLifeSeconds: halfLife}
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	// Set activation state
	activationTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	td.keeper.SetActivationTime(td.ctx, activationTime)
	td.keeper.SetLastBlockTime(td.ctx, activationTime.Add(95*time.Second))
	td.keeper.SetPoolBalanceAtActivation(td.ctx, poolBalance)

	// Set block time to 100s after activation
	blockTime := activationTime.Add(100 * time.Second)
	ctx := td.ctx.WithBlockTime(blockTime)

	// Calculate expected reward
	reward := keeper.CalculateBlockReward(halfLife, poolBalance, 100, 5)
	require.True(t, reward.IsPositive())

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	// Mock: GetModuleAddress returns the svip module addr
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	// Mock: GetBalance returns the pool balance
	td.bk.On("GetBalance", ctx, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))
	// Mock: SendCoinsFromModuleToModule succeeds
	td.bk.On("SendCoinsFromModuleToModule",
		ctx,
		types.ModuleName,
		authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewCoin(denom, reward)),
	).Return(nil)
	// Mock: GetBalance for post-distribution event (pool_remaining)
	td.bk.On("GetBalance", ctx, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance.Sub(reward)))

	err := td.keeper.BeginBlock(ctx)
	require.NoError(t, err)

	// Verify bookkeeping
	require.Equal(t, reward, td.keeper.GetTotalDistributed(ctx))
	require.Equal(t, blockTime, td.keeper.GetLastBlockTime(ctx).UTC())

	td.bk.AssertCalled(t, "SendCoinsFromModuleToModule",
		ctx, types.ModuleName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewCoin(denom, reward)),
	)
}

func TestBeginBlock_CapsAtPoolBalance(t *testing.T) {
	td := newMockedTestData(t)

	denom := vmtypes.GetEVMCoinDenom()
	halfLife := int64(31536000)
	poolAtActivation := sdkmath.NewInt(1_000_000_000_000)
	// Remaining pool balance is tiny — smaller than calculated reward
	tinyBalance := sdkmath.NewInt(1)

	params := types.Params{Activated: true, Paused: false, HalfLifeSeconds: halfLife}
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	activationTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	td.keeper.SetActivationTime(td.ctx, activationTime)
	td.keeper.SetLastBlockTime(td.ctx, activationTime.Add(95*time.Second))
	td.keeper.SetPoolBalanceAtActivation(td.ctx, poolAtActivation)

	blockTime := activationTime.Add(100 * time.Second)
	ctx := td.ctx.WithBlockTime(blockTime)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", ctx, moduleAddr, denom).Return(sdk.NewCoin(denom, tinyBalance))
	// The send should use tinyBalance (the cap), not the calculated reward
	td.bk.On("SendCoinsFromModuleToModule",
		ctx,
		types.ModuleName,
		authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewCoin(denom, tinyBalance)),
	).Return(nil)

	err := td.keeper.BeginBlock(ctx)
	require.NoError(t, err)

	require.Equal(t, tinyBalance, td.keeper.GetTotalDistributed(ctx))
}
