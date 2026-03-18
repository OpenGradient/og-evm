package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/cosmos/evm/x/svip/types"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func govAuthority() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

func TestUpdateParams_InvalidAuthority(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: "invalid",
		Params:    types.DefaultParams(),
	})
	require.ErrorContains(t, err, "invalid authority")
}

func TestUpdateParams_CannotDeactivate(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Set as activated
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 31536000,
	}))

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: false, HalfLifeSeconds: 31536000},
	})
	require.ErrorContains(t, err, "cannot deactivate")
}

func TestUpdateParams_HalfLifeChangeCap(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 100_000_000,
	}))

	// >50% increase (1.6x) should fail
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: true, HalfLifeSeconds: 160_000_000},
	})
	require.ErrorIs(t, err, types.ErrHalfLifeChange)

	// >50% decrease (0.4x) should fail
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: true, HalfLifeSeconds: 40_000_000},
	})
	require.ErrorIs(t, err, types.ErrHalfLifeChange)

	// Exact 0.5x boundary (ratio == 0.5) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: true, HalfLifeSeconds: 50_000_000},
	})
	require.NoError(t, err)

	// Reset to 100M for next boundary test
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 100_000_000,
	}))

	// Exact 1.5x boundary (ratio == 1.5) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: true, HalfLifeSeconds: 150_000_000},
	})
	require.NoError(t, err)

	// Reset to 100M for next test
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 100_000_000,
	}))

	// Within 50% (1.3x) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: true, HalfLifeSeconds: 130_000_000},
	})
	require.NoError(t, err)
}

func TestUpdateParams_HalfLifeMinimum(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{Activated: false, HalfLifeSeconds: 1000}, // < 1 year
	})
	require.ErrorContains(t, err, "half_life_seconds must be >= 1 year")
}

func TestUpdateParams_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	newParams := types.Params{Activated: false, HalfLifeSeconds: 63072000} // 2 years
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    newParams,
	})
	require.NoError(t, err)
	require.Equal(t, newParams, td.keeper.GetParams(td.ctx))
}

func TestActivate_InvalidAuthority(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.Activate(td.ctx, &types.MsgActivate{Authority: "bad"})
	require.ErrorContains(t, err, "invalid authority")
}

func TestActivate_AlreadyActivated(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 31536000,
	}))

	_, err := srv.Activate(td.ctx, &types.MsgActivate{Authority: govAuthority()})
	require.ErrorIs(t, err, types.ErrAlreadyActivated)
}

func TestActivate_MissingHalfLife(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Default params: half_life = 0
	_, err := srv.Activate(td.ctx, &types.MsgActivate{Authority: govAuthority()})
	require.ErrorContains(t, err, "half_life_seconds must be set")
}

func TestActivate_PoolNotFunded(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: false, HalfLifeSeconds: 31536000,
	}))

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, sdkmath.ZeroInt()))

	_, err := srv.Activate(td.ctx, &types.MsgActivate{Authority: govAuthority()})
	require.ErrorIs(t, err, types.ErrPoolNotFunded)
}

func TestActivate_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	halfLife := int64(31536000)
	poolBalance := sdkmath.NewInt(1_000_000_000_000)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: false, HalfLifeSeconds: halfLife,
	}))

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))

	blockTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(blockTime)

	_, err := srv.Activate(ctx, &types.MsgActivate{Authority: govAuthority()})
	require.NoError(t, err)

	params := td.keeper.GetParams(ctx)
	require.True(t, params.Activated)
	require.Equal(t, poolBalance, td.keeper.GetPoolBalanceAtActivation(ctx))
	require.Equal(t, blockTime, td.keeper.GetActivationTime(ctx).UTC())
	require.Equal(t, blockTime, td.keeper.GetLastBlockTime(ctx).UTC())
}

func TestReactivate_NotActivated(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.Reactivate(td.ctx, &types.MsgReactivate{Authority: govAuthority()})
	require.ErrorIs(t, err, types.ErrNotYetActivated)
}

func TestReactivate_PoolNotFunded(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 31536000,
	}))

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, sdkmath.ZeroInt()))

	_, err := srv.Reactivate(td.ctx, &types.MsgReactivate{Authority: govAuthority()})
	require.ErrorIs(t, err, types.ErrPoolNotFunded)
}

func TestReactivate_ClearsPaused(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, Paused: true, HalfLifeSeconds: 31536000,
	}))
	td.keeper.SetTotalPausedSeconds(td.ctx, 500)

	poolBalance := sdkmath.NewInt(1_000_000_000_000)
	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))

	blockTime := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(blockTime)

	_, err := srv.Reactivate(ctx, &types.MsgReactivate{Authority: govAuthority()})
	require.NoError(t, err)

	params := td.keeper.GetParams(ctx)
	require.False(t, params.Paused, "reactivate should clear paused flag")
	require.Equal(t, int64(0), td.keeper.GetTotalPausedSeconds(ctx))
	require.True(t, td.keeper.GetTotalDistributed(ctx).IsZero())
}

func TestReactivate_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, HalfLifeSeconds: 31536000,
	}))
	td.keeper.SetTotalDistributed(td.ctx, sdkmath.NewInt(5000))

	newPoolBalance := sdkmath.NewInt(500_000_000_000)
	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, newPoolBalance))

	blockTime := time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(blockTime)

	_, err := srv.Reactivate(ctx, &types.MsgReactivate{Authority: govAuthority()})
	require.NoError(t, err)

	require.Equal(t, newPoolBalance, td.keeper.GetPoolBalanceAtActivation(ctx))
	require.Equal(t, blockTime, td.keeper.GetActivationTime(ctx).UTC())
	require.True(t, td.keeper.GetTotalDistributed(ctx).IsZero())
	require.Equal(t, int64(0), td.keeper.GetTotalPausedSeconds(ctx))
}

func TestPause_InvalidAuthority(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.Pause(td.ctx, &types.MsgPause{Authority: "bad", Paused: true})
	require.ErrorContains(t, err, "invalid authority")
}

func TestPause_AccumulatesPausedDuration(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Start with activated + paused
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: true, Paused: true, HalfLifeSeconds: 31536000,
	}))

	pauseStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	td.keeper.SetLastBlockTime(td.ctx, pauseStart)

	// Unpause 100 seconds later
	unpauseTime := pauseStart.Add(100 * time.Second)
	ctx := td.ctx.WithBlockTime(unpauseTime)

	_, err := srv.Pause(ctx, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.NoError(t, err)

	require.Equal(t, int64(100), td.keeper.GetTotalPausedSeconds(ctx))
	require.Equal(t, unpauseTime, td.keeper.GetLastBlockTime(ctx).UTC())

	// Verify params updated
	params := td.keeper.GetParams(ctx)
	require.False(t, params.Paused)
}

func TestPause_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Set LastBlockTime so unpause has a valid reference point
	td.keeper.SetLastBlockTime(td.ctx, td.ctx.BlockTime())

	// Pause
	_, err := srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: true})
	require.NoError(t, err)
	require.True(t, td.keeper.GetParams(td.ctx).Paused)

	// Unpause
	_, err = srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.NoError(t, err)
	require.False(t, td.keeper.GetParams(td.ctx).Paused)
}

func TestFundPool_InvalidDepositor(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.FundPool(td.ctx, &types.MsgFundPool{
		Depositor: "invalid",
		Amount:    sdk.NewCoins(sdk.NewInt64Coin("ogwei", 1000)),
	})
	require.Error(t, err)
}

func TestFundPool_WrongDenom(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	depositor := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	_, err := srv.FundPool(td.ctx, &types.MsgFundPool{
		Depositor: depositor,
		Amount:    sdk.NewCoins(sdk.NewInt64Coin("wrongdenom", 1000)),
	})
	require.ErrorContains(t, err, "invalid denom")
}

func TestFundPool_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	denom := vmtypes.GetEVMCoinDenom()
	depositor := authtypes.NewModuleAddress(govtypes.ModuleName)
	amount := sdk.NewCoins(sdk.NewInt64Coin(denom, 1000))

	td.bk.On("SendCoinsFromAccountToModule", td.ctx, depositor, types.ModuleName, amount).Return(nil)

	_, err := srv.FundPool(td.ctx, &types.MsgFundPool{
		Depositor: depositor.String(),
		Amount:    amount,
	})
	require.NoError(t, err)

	td.bk.AssertCalled(t, "SendCoinsFromAccountToModule", td.ctx, depositor, types.ModuleName, amount)
}

func TestBeginBlock_AfterPauseUnpause(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	denom := vmtypes.GetEVMCoinDenom()
	halfLife := int64(31536000) // 1 year
	poolBalance := sdkmath.NewInt(1_000_000_000_000_000)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))
	td.bk.On("SendCoinsFromModuleToModule",
		mock.Anything, types.ModuleName, authtypes.FeeCollectorName, mock.Anything,
	).Return(nil)

	// 1. Activate at T=0
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{
		Activated: false, HalfLifeSeconds: halfLife,
	}))
	activationTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(activationTime)
	_, err := srv.Activate(ctx, &types.MsgActivate{Authority: govAuthority()})
	require.NoError(t, err)

	// 2. Run BeginBlock at T=100s (normal distribution)
	t100 := activationTime.Add(100 * time.Second)
	ctx100 := td.ctx.WithBlockTime(t100)
	err = td.keeper.BeginBlock(ctx100)
	require.NoError(t, err)

	step2Reward := td.keeper.GetTotalDistributed(ctx100)

	// 3. Pause at T=100s
	_, err = srv.Pause(ctx100, &types.MsgPause{Authority: govAuthority(), Paused: true})
	require.NoError(t, err)

	// 4. Unpause after a full half-life of pause
	pauseDuration := time.Duration(halfLife) * time.Second
	tUnpause := activationTime.Add(100*time.Second + pauseDuration)
	ctxUnpause := td.ctx.WithBlockTime(tUnpause)
	_, err = srv.Pause(ctxUnpause, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.NoError(t, err)
	require.Equal(t, int64(halfLife), td.keeper.GetTotalPausedSeconds(ctxUnpause))

	// 5. Run BeginBlock 5s after unpause
	// Active time = (100 + halfLife + 5) - 0 - halfLife(paused) = 105s, blockDelta = 5s
	tPost := tUnpause.Add(5 * time.Second)
	ctxPost := td.ctx.WithBlockTime(tPost)
	err = td.keeper.BeginBlock(ctxPost)
	require.NoError(t, err)

	// 6. Verify the exact reward distributed matches elapsed=105s (not halfLife+105s)
	correctReward := keeper.CalculateBlockReward(halfLife, poolBalance, 105, 5)
	buggyReward := keeper.CalculateBlockReward(halfLife, poolBalance, float64(halfLife)+105, 5)

	expectedTotal := step2Reward.Add(correctReward)
	require.Equal(t, expectedTotal, td.keeper.GetTotalDistributed(ctxPost),
		"total distributed should match correct elapsed, not buggy elapsed")

	// Sanity: correct reward must be ~2x buggy reward after 1 half-life pause
	require.True(t, correctReward.GT(buggyReward),
		"correct reward (%s) should be ~2x buggy reward (%s)", correctReward, buggyReward)
}
