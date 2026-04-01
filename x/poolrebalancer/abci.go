package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs at end of block:
// 1) strict cleanup of matured pending entries,
// 2) best-effort CommunityPool automation (harvest/stake),
// 3) best-effort staking rebalance.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) error {
	// Keep cleanup strict to avoid queue/index drift from staking state.
	if err := k.CompletePendingRedelegations(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: complete pending redelegations failed", "err", err)
		return err
	}
	if err := k.CompletePendingUndelegations(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: complete pending undelegations failed", "err", err)
		return err
	}
	// Community pool automation is best-effort; operational failures are retried next block.
	if err := k.MaybeRunCommunityPoolAutomation(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: community pool automation failed", "err", err)
	}
	// Rebalance is best-effort; operational failures are retried next block.
	if err := k.ProcessRebalance(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: process rebalance failed", "err", err)
		return nil
	}
	return nil
}
