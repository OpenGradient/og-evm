package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ABCI: previous-block slash signals are snapshot in BeginBlock for EndBlock use.
//
// BeginBlock: PreparePreviousBlockSlashedValidators snapshots relevant validators with
// distribution slash events at height blockHeight-1. Ordered after slashing/evidence so
// recent slash state is visible before EndBlock rebalance consumes it. Errors halt the block.
//
// EndBlock: CompletePendingRedelegations removes matured tracking rows. ProcessRebalance then
// uses the slash snapshot to avoid same-block destinations into recently slashed validators and
// to prioritize moving stake away from them. MaybeReconcileCommunityPoolStakedBuckets runs before
// automation/rebalance; MaybeReconcileCommunityPoolStakedBucketsSecondPass may run after successful
// ProcessRebalance when test hook enables it. ABCI logs reconcile/automation/rebalance failures only — see
// docs/poolrebalancer/community_pool_runbook.md.

// BeginBlocker snapshots previous-block slash signals into transient store.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.PreparePreviousBlockSlashedValidators(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: prepare previous-block slashed validators failed", "err", err)
		return err
	}
	return nil
}

// EndBlocker: (1) CompletePendingRedelegations — strict (halt on error).
// (2) pre-reconcile, (3) automation, (4) rebalance (+ optional second reconcile) — best-effort (log only).
func EndBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.CompletePendingRedelegations(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: complete pending redelegations failed", "err", err)
		return err
	}
	if err := k.MaybeReconcileCommunityPoolStakedBuckets(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: community pool total staked reconcile failed", "err", err)
	}
	if err := k.MaybeRunCommunityPoolAutomation(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: community pool automation failed", "err", err)
	}
	if err := k.ProcessRebalance(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: process rebalance failed", "err", err)
	} else {
		if err := k.MaybeReconcileCommunityPoolStakedBucketsSecondPass(ctx); err != nil {
			ctx.Logger().Error("poolrebalancer: community pool staked buckets second reconcile failed", "err", err)
		}
	}
	return nil
}
