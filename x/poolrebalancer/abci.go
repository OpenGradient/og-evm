package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs at end of block: complete matured redelegations/undelegations, then process rebalance.
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
	// Rebalance is best-effort; operational failures are retried next block.
	if err := k.ProcessRebalance(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: process rebalance failed", "err", err)
		return nil
	}
	return nil
}
