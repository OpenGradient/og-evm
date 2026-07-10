package keeper

import (
	"time"

	"github.com/cosmos/evm/x/svip/types"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// BeginBlock distributes decayed rewards to the fee collector.
//
// The math is all integer/fixed-point so every node computes the same result. Expected
// failures (corrupt decay state, a failed transfer) are logged and skipped for the block
// instead of returned, since a non-nil error from BeginBlock halts the chain.
func (k Keeper) BeginBlock(ctx sdk.Context) error {
	// Skip if not active.
	if !k.GetActivated(ctx) || k.GetPaused(ctx) {
		return nil
	}

	// Whole seconds elapsed since the last processed block.
	now := ctx.BlockTime()
	lastBlockTime := k.GetLastBlockTime(ctx)
	blockDelta := now.Unix() - lastBlockTime.Unix()
	if blockDelta <= 0 {
		// Sub-second or non-advancing block: keep the accrual, leave the anchor alone.
		return nil
	}

	// Load the decay state. A read error means corrupt state, so skip rather than halt.
	scheduledRemaining, err := k.GetScheduledRemaining(ctx)
	if err != nil {
		k.Logger(ctx).Error("svip: failed to read scheduled remaining; skipping emission", "error", err)
		return nil
	}
	decayFactor, err := k.GetDecayFactor(ctx)
	if err != nil {
		k.Logger(ctx).Error("svip: failed to read decay factor; skipping emission", "error", err)
		return nil
	}

	reward, newS := CalculateBlockReward(scheduledRemaining, decayFactor, blockDelta)

	// Never distribute more than the pool actually holds.
	poolBalance := k.getPoolBalance(ctx)
	if poolBalance.LT(reward) {
		reward = poolBalance
	}

	if !reward.IsPositive() {
		// Nothing to pay this block, but still advance the curve and the anchor.
		k.SetScheduledRemaining(ctx, k.clampToPool(ctx, newS))
		k.advanceAnchor(ctx, lastBlockTime, blockDelta)
		return nil
	}

	// Send the emission to the fee collector, where x/distribution splits it across
	// validators and delegators by consensus power in the same block. That is deliberate:
	// this inflation is a staking reward and follows the same fee_collector path the mint
	// module uses. Reordering the BeginBlocker wouldn't change it (the fee_collector balance
	// carries across blocks), and paying a different recipient would mean changing this target.
	denom := k.getDenom(ctx)
	coins := sdk.NewCoins(sdk.NewCoin(denom, reward))
	if err := k.bk.SendCoinsFromModuleToModule(
		ctx, types.ModuleName, authtypes.FeeCollectorName, coins,
	); err != nil {
		// Skip without advancing the curve or anchor, so the reward is deferred rather than
		// burned and the next block retries it.
		k.Logger(ctx).Error("svip: reward transfer failed; skipping emission", "error", err)
		return nil
	}

	// Only persist the decayed curve after the send succeeds, then bookkeep.
	k.SetScheduledRemaining(ctx, k.clampToPool(ctx, newS))
	k.AddTotalDistributed(ctx, reward)
	k.advanceAnchor(ctx, lastBlockTime, blockDelta)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"svip_reward",
		sdk.NewAttribute("amount", coins.String()),
		sdk.NewAttribute("pool_remaining", k.getPoolBalance(ctx).String()),
	))

	return nil
}

// clampToPool holds the scheduled remaining at or below the live pool balance, keeping
// the S <= pool invariant after capping and truncation dust.
func (k Keeper) clampToPool(ctx sdk.Context, s sdkmath.LegacyDec) sdkmath.LegacyDec {
	poolDec := sdkmath.LegacyNewDecFromInt(k.getPoolBalance(ctx))
	if s.GT(poolDec) {
		return poolDec
	}
	return s
}

// advanceAnchor moves LastBlockTime forward by exactly the whole seconds consumed rather
// than to now, so any sub-second remainder carries into the next block and fast blocks
// don't lose accrual.
func (k Keeper) advanceAnchor(ctx sdk.Context, lastBlockTime time.Time, blockDelta int64) {
	k.SetLastBlockTime(ctx, lastBlockTime.Add(time.Duration(blockDelta)*time.Second))
}
