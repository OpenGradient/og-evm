package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs at end of block:
//  1) Strict cleanup of matured pending redelegations and undelegations. For matured module-tracked
//     undelegations matching PoolDelegatorAddress and bond denom, CompletePendingUndelegations calls
//     CommunityPool.creditStakeableFromRebalance (EVM) before removing queue entries so stakeablePrincipalLedger
//     can be delegated on the next step; staking EndBlock has already released liquid tokens to the delegator.
//  2) Best-effort CommunityPool automation (harvest, then stake).
//  3) Best-effort staking rebalance.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) error {
	// Keep cleanup strict to avoid queue/index drift from staking state and to avoid dropping creditable amounts.
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
