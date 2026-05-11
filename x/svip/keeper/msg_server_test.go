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

func TestUpdateParams_HalfLifeChangeCap(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 100_000_000}))
	td.keeper.SetActivated(td.ctx, true)

	// >50% increase (1.6x) should fail
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 160_000_000},
	})
	require.ErrorIs(t, err, types.ErrHalfLifeChange)

	// >50% decrease (0.4x) should fail
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 40_000_000},
	})
	require.ErrorIs(t, err, types.ErrHalfLifeChange)

	// Exact 0.5x boundary (ratio == 0.5) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 50_000_000},
	})
	require.NoError(t, err)

	// Reset to 100M for next boundary test
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 100_000_000}))

	// Exact 1.5x boundary (ratio == 1.5) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 150_000_000},
	})
	require.NoError(t, err)

	// Reset to 100M for next test
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 100_000_000}))

	// Within 50% (1.3x) should pass
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 130_000_000},
	})
	require.NoError(t, err)
}

func TestUpdateParams_HalfLifeMinimum(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 1000}, // < 1 year
	})
	require.ErrorContains(t, err, "half_life_seconds must be >= 1 year")
}

func TestUpdateParams_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	newParams := types.Params{HalfLifeSeconds: 63072000} // 2 years
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

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)

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

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))

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

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: halfLife}))

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))

	blockTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(blockTime)

	_, err := srv.Activate(ctx, &types.MsgActivate{Authority: govAuthority()})
	require.NoError(t, err)

	require.True(t, td.keeper.GetActivated(ctx))
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

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)

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

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)
	td.keeper.SetPaused(td.ctx, true)
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

	require.False(t, td.keeper.GetPaused(ctx), "reactivate should clear paused flag")
	require.Equal(t, int64(0), td.keeper.GetTotalPausedSeconds(ctx))
	require.True(t, td.keeper.GetTotalDistributed(ctx).IsZero())
}

func TestReactivate_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)
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
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)
	td.keeper.SetPaused(td.ctx, true)

	pauseStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	td.keeper.SetLastBlockTime(td.ctx, pauseStart)

	// Unpause 100 seconds later
	unpauseTime := pauseStart.Add(100 * time.Second)
	ctx := td.ctx.WithBlockTime(unpauseTime)

	_, err := srv.Pause(ctx, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.NoError(t, err)

	require.Equal(t, int64(100), td.keeper.GetTotalPausedSeconds(ctx))
	require.Equal(t, unpauseTime, td.keeper.GetLastBlockTime(ctx).UTC())

	// Verify paused flag cleared
	require.False(t, td.keeper.GetPaused(ctx))
}

func TestPause_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Must be activated for pause to work
	td.keeper.SetActivated(td.ctx, true)

	// Set LastBlockTime so unpause has a valid reference point
	td.keeper.SetLastBlockTime(td.ctx, td.ctx.BlockTime())

	// Pause
	_, err := srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: true})
	require.NoError(t, err)
	require.True(t, td.keeper.GetPaused(td.ctx))

	// Unpause
	_, err = srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.NoError(t, err)
	require.False(t, td.keeper.GetPaused(td.ctx))
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
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: halfLife}))
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

func TestUpdateParams_RejectZeroHalfLifeWhenActivated(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Set a valid half-life and activate
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 31536000}))
	td.keeper.SetActivated(td.ctx, true)

	// Attempt to set half_life to 0 while activated — must fail
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 0},
	})
	require.ErrorContains(t, err, "half_life_seconds must be > 0 while module is activated")

	// Params must remain unchanged
	require.Equal(t, int64(31536000), td.keeper.GetParams(td.ctx).HalfLifeSeconds)
}

func TestUpdateParams_AllowZeroHalfLifePreActivation(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Module is not activated — setting to 0 should be allowed (default state)
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 0},
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), td.keeper.GetParams(td.ctx).HalfLifeSeconds)
}

func TestUpdateParams_TwoStepExploitBlocked(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Set a valid half-life and activate
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 100_000_000}))
	td.keeper.SetActivated(td.ctx, true)

	// Step 1 of the exploit: try to set half_life to 0 — must be blocked
	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 0},
	})
	require.Error(t, err, "step 1 of two-step exploit must be blocked")

	// Verify the half-life is still the original value
	require.Equal(t, int64(100_000_000), td.keeper.GetParams(td.ctx).HalfLifeSeconds)

	// Step 2 would have been: set to an arbitrary value bypassing the 50% cap.
	// Since step 1 is blocked, the exploit is impossible. Verify the 50% cap
	// still applies: jumping from 100M to 999B must fail.
	_, err = srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    types.Params{HalfLifeSeconds: 999_999_999_999},
	})
	require.ErrorIs(t, err, types.ErrHalfLifeChange, "50%% cap must still block large jumps")
}

func TestReactivate_RejectZeroHalfLife(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Force an inconsistent state: activated=true but half_life=0
	// (shouldn't happen with the UpdateParams fix, but tests defense-in-depth)
	td.keeper.SetActivated(td.ctx, true)
	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: 0}))

	_, err := srv.Reactivate(td.ctx, &types.MsgReactivate{Authority: govAuthority()})
	require.ErrorContains(t, err, "half_life_seconds must be set before reactivation")
}

func TestPause_RejectedBeforeActivation(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	// Module is not activated — pause must be rejected
	_, err := srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: true})
	require.ErrorIs(t, err, types.ErrNotYetActivated)

	// Unpause also rejected before activation
	_, err = srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: false})
	require.ErrorIs(t, err, types.ErrNotYetActivated)
}

func TestActivate_ClearsStalePauseState(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	halfLife := int64(31536000)
	poolBalance := sdkmath.NewInt(1_000_000_000_000)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: halfLife}))

	// Simulate poisoned pre-activation state (e.g. from genesis import)
	td.keeper.SetPaused(td.ctx, true)
	td.keeper.SetTotalPausedSeconds(td.ctx, 99999999999)

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))

	blockTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(blockTime)

	_, err := srv.Activate(ctx, &types.MsgActivate{Authority: govAuthority()})
	require.NoError(t, err)

	// Verify activation cleaned the poisoned state
	require.False(t, td.keeper.GetPaused(ctx), "activate must clear paused flag")
	require.Equal(t, int64(0), td.keeper.GetTotalPausedSeconds(ctx), "activate must reset total_paused_seconds")
	require.True(t, td.keeper.GetActivated(ctx))
}

func TestPause_PreActivationPoisonFixed(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	halfLife := int64(31536000)
	poolBalance := sdkmath.NewInt(1_000_000_000_000_000)

	require.NoError(t, td.keeper.SetParams(td.ctx, types.Params{HalfLifeSeconds: halfLife}))

	denom := vmtypes.GetEVMCoinDenom()
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	td.ak.On("GetModuleAddress", types.ModuleName).Return(moduleAddr)
	td.bk.On("GetBalance", mock.Anything, moduleAddr, denom).Return(sdk.NewCoin(denom, poolBalance))
	td.bk.On("SendCoinsFromModuleToModule",
		mock.Anything, types.ModuleName, authtypes.FeeCollectorName, mock.Anything,
	).Return(nil)

	// 1. Attempt pause before activation — must fail
	_, err := srv.Pause(td.ctx, &types.MsgPause{Authority: govAuthority(), Paused: true})
	require.ErrorIs(t, err, types.ErrNotYetActivated)

	// 2. Activate
	activationTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := td.ctx.WithBlockTime(activationTime)
	_, err = srv.Activate(ctx, &types.MsgActivate{Authority: govAuthority()})
	require.NoError(t, err)

	// 3. Verify TotalPausedSeconds is 0 after activation
	require.Equal(t, int64(0), td.keeper.GetTotalPausedSeconds(ctx))

	// 4. Run BeginBlock 10 seconds later — should produce rewards
	t10 := activationTime.Add(10 * time.Second)
	ctx10 := td.ctx.WithBlockTime(t10)
	err = td.keeper.BeginBlock(ctx10)
	require.NoError(t, err)

	// 5. Verify rewards were actually distributed (not zero)
	distributed := td.keeper.GetTotalDistributed(ctx10)
	require.True(t, distributed.IsPositive(),
		"rewards must be distributed after clean activation, got %s", distributed)
}
