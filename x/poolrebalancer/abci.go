package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ABCI: matured pool undelegations and previous-block slash signals are snapshot in BeginBlock for EndBlock use.
//
// BeginBlock: PrepareMaturedPoolUndelegationCredits sums slash-aligned staking UBD balances (deduped by
// delegator/validator/completion) into transient store. PreparePreviousBlockSlashedValidators snapshots relevant
// validators with distribution slash events at height blockHeight-1. Ordered after slashing/evidence so recent
// slash state is visible before EndBlock rebalance consumes it. Errors halt the block.
//
// EndBlock: CompletePendingUndelegations uses that snapshot for creditStakeableFromRebalance, then deletes
// queue/index entries. Missing snapshot when batches exist halts the block. Staking EndBlock runs before this
// module so liquid is available. Credit reduces pendingRebalanceUnbondReserve; a positive credit sets the
// reconcile dirty flag. ProcessRebalance then uses the slash snapshot to avoid same-block destinations into
// recently slashed validators and to prioritize moving stake away from them. MaybeReconcileCommunityPoolStakedBuckets
// (after completion) aligns EVM bonded/pending with staking; ABCI logs failures only — see
// docs/poolrebalancer/community_pool_runbook.md.

// BeginBlocker snapshots matured pool undelegation credits and previous-block slash signals into transient store.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.PrepareMaturedPoolUndelegationCredits(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: prepare matured pool undelegation credits failed", "err", err)
		return err
	}
	if err := k.PreparePreviousBlockSlashedValidators(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: prepare previous-block slashed validators failed", "err", err)
		return err
	}
	return nil
}

// EndBlocker: (1) CompletePendingRedelegations and CompletePendingUndelegations — strict (halt on error).
// (2)–(4) reconcile, automation, rebalance (+ optional second reconcile) — best-effort (log only).
func EndBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.CompletePendingRedelegations(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: complete pending redelegations failed", "err", err)
		return err
	}
	if err := k.CompletePendingUndelegations(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: complete pending undelegations failed", "err", err)
		return err
	}
	if err := k.MaybeReconcileCommunityPoolStakedBuckets(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: community pool staked buckets reconcile failed", "err", err)
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
