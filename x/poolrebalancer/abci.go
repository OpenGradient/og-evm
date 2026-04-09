package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ABCI: matured pool undelegations are credited using a BeginBlock snapshot and EndBlock completion.
//
// BeginBlock: PrepareMaturedPoolUndelegationCredits sums slash-aligned staking UBD balances (deduped by
// delegator/validator/completion) into transient store. Ordered after slashing/evidence so same-block
// slashes are visible. Errors halt the block.
//
// EndBlock: CompletePendingUndelegations uses that snapshot for creditStakeableFromRebalance, then deletes
// queue/index entries. Missing snapshot when batches exist halts the block. Staking EndBlock runs before this
// module so liquid is available. Credit reduces pendingRebalanceUnbondReserve; a positive credit sets the
// reconcile dirty flag. MaybeReconcileCommunityPoolStakedBuckets (after completion) aligns EVM bonded/pending
// with staking; ABCI logs failures only — see docs/poolrebalancer/community_pool_runbook.md.

// BeginBlocker snapshots matured pool undelegation credits from staking state into transient store.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.PrepareMaturedPoolUndelegationCredits(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: prepare matured pool undelegation credits failed", "err", err)
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
