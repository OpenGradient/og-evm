package keeper

import (
	"github.com/cosmos/evm/x/svip/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// BeginBlock distributes decayed rewards to FeeCollector.
func (k Keeper) BeginBlock(ctx sdk.Context) error {
	params := k.GetParams(ctx)

	// 1. Guard: skip if not active
	if !params.Activated || params.Paused {
		return nil
	}

	// 2. Time context
	now := ctx.BlockTime()
	activationTime := k.GetActivationTime(ctx)
	lastBlockTime := k.GetLastBlockTime(ctx)

	totalElapsed := now.Sub(activationTime).Seconds()
	blockDelta := now.Sub(lastBlockTime).Seconds()

	if totalElapsed <= 0 || blockDelta <= 0 {
		return nil
	}

	// 3. Calculate reward (exponential decay, hardcoded)
	poolAtActivation := k.GetPoolBalanceAtActivation(ctx)
	reward := CalculateBlockReward(params.HalfLifeSeconds, poolAtActivation, totalElapsed, blockDelta)

	if reward.IsZero() || reward.IsNegative() {
		k.SetLastBlockTime(ctx, now)
		return nil
	}

	// 4. Cap at actual pool balance
	poolBalance := k.getPoolBalance(ctx)
	if poolBalance.LT(reward) {
		reward = poolBalance
	}
	if reward.IsZero() {
		k.SetLastBlockTime(ctx, now)
		return nil
	}

	// 5. Transfer: svip → fee_collector
	denom := k.getDenom(ctx)
	coins := sdk.NewCoins(sdk.NewCoin(denom, reward))
	err := k.bk.SendCoinsFromModuleToModule(
		ctx,
		types.ModuleName,
		authtypes.FeeCollectorName,
		coins,
	)
	if err != nil {
		return err
	}

	// 6. Bookkeeping + events
	k.AddTotalDistributed(ctx, reward)
	k.SetLastBlockTime(ctx, now)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"svip_reward",
		sdk.NewAttribute("amount", coins.String()),
		sdk.NewAttribute("pool_remaining", k.getPoolBalance(ctx).String()),
	))

	return nil
}
