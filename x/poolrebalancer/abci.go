package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ABCI — undelegation credit reconciliation
//
// Across a block, pool-tracked matured undelegations are credited using a two-phase flow:
//
//  1) BeginBlock: PrepareMaturedPoolUndelegationCredits reads staking UnbondingDelegation entry balances
//     (post-slash, deduped per delegator/validator/completion time) and writes the summed credit to the
//     module transient store. The app orders this module after slashing and evidence BeginBlock so same-block
//     downtime and equivocation slashes are reflected in those balances. Errors here are returned to the
//     caller and halt the block.
//
//  2) EndBlock: CompletePendingUndelegations reads that transient snapshot to decide the EVM credit amount,
//     then removes matured queue and index entries. If there are matured undelegation batches but the
//     snapshot is missing (BeginBlock did not run this block), completion errors strictly. After a successful
//     completion, the transient entry is reset. Staking EndBlock runs before poolrebalancer EndBlock so
//     liquid payout is available before crediting.
//
// Test coverage map (high level):
//
//   • Prepare + Complete, deduped triples, slash-aligned amounts vs queue, strict Begin before End:
//     x/poolrebalancer/keeper (undelegation tests), x/poolrebalancer/abci_test.go.
//
//   • CommunityPool.creditStakeableFromRebalance with real VM + full blocks:
//     tests/integration/precompiles/communitypool (Ginkgo: maturity, duplicate-queue dedupe,
//     EndBlock without prior BeginBlock).
//
//   • Rebalance scheduling with stub EVM (no real contract):
//     tests/integration/x/poolrebalancer. For matured module-tracked undelegations use
//     RunBeginThenEndBlock so BeginBlock fills the transient credit snapshot.
//
//   • BeginBlock order: slashing and evidence before poolrebalancer:
//     evmd/app_begin_block_order_test.go.
//
//   • No integration test drives evidence/downtime slash → UBD maturity → contract credit end-to-end
//     (hard to make deterministic). Unit tests PrepareMaturedPoolUndelegationCredits_UsesStakingBalanceForSlashAlignment
//     and CompletePendingUndelegations_CreditsSlashAlignedNotQueueBalance lock the intended amounts.
//
//   • Foundry (optional): contracts/test/pool/CommunityPoolCredit.t.sol — see file header for command.

// BeginBlocker snapshots matured pool undelegation credits from staking state into transient store.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.PrepareMaturedPoolUndelegationCredits(ctx); err != nil {
		ctx.Logger().Error("poolrebalancer: prepare matured pool undelegation credits failed", "err", err)
		return err
	}
	return nil
}

// EndBlocker runs at end of block:
//  1. Strict cleanup: CompletePendingRedelegations, then CompletePendingUndelegations. For matured
//     undelegations, CommunityPool.creditStakeableFromRebalance (EVM) uses the BeginBlock transient
//     snapshot (slash-aligned staking balances), not queued module balances; credit runs before queue/index
//     deletes so a failed EVM call preserves state for retry.
//  2. Best-effort CommunityPool automation (harvest, then stake).
//  3. Best-effort staking rebalance.
//
// Errors from (1) are returned and halt EndBlock; failures in (2)–(3) are logged and non-halting where noted.
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
