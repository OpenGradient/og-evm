# CommunityPool Integration Test Assumptions

This document captures assumptions that the `communitypool` integration suite depends on for deterministic behavior.

## Environment assumptions

- The suite runs against the standard integration test network created by `network.NewUnitTestNetwork`.
- The chain has a valid staking bond denom and an ERC20 token pair for that denom.
- At least one active validator exists in the network validator set.

## Contract + artifact assumptions

- `contracts/solidity/pool/CommunityPool.json` matches the current `CommunityPool.sol` implementation.
- `contracts/community_pool.go` successfully loads that artifact via `LoadCommunityPool()`.

## Test helper assumptions

- Read-only contract checks use `QueryContract(...)` (not tx execution), so nonce state is not mutated by view calls.
- Successful tx helper (`execTxExpectSuccess`) sets a default gas limit when none is provided, to avoid estimator/limit edge cases in precompile-heavy paths (for example, `harvest`).
- Tests commit blocks (`network.NextBlock()`) between state-changing calls that require finalized state for subsequent reads/assertions.

## Behavioral assumptions under test

- Deposit/withdraw accounting uses floor rounding and must never over-mint shares.
- Dust deposits that mint zero units must revert and preserve unit state.
- Owner-gated methods (`setConfig`, `syncTotalStaked`, `transferOwnership`) enforce access control.
- `stake()` and `harvest()` are restricted to `owner` or configured `automationCaller`.
- `creditStakeableFromRebalance` is restricted to `owner` or `automationCaller` (same as `stake` / `harvest`). The poolrebalancer module uses `CallEVM` from the module EVM account, which must therefore be allowed to call it (typically `automationCaller` is set to that address).
- `reconcileStakedBuckets` is restricted to `automationCaller` only (not `owner`). On-chain bucket repair from staking truth is expected to use that caller (again, usually the module EVM address).
- `principalAssets` is `stakeablePrincipalLedger + totalStaked + pendingRebalanceUnbondReserve`; `pricePerUnit` and deposit minting use that total.
- User `withdraw` sizes `amountOut` from **`totalStaked` only** (proportional to units burned). It does **not** reduce `pendingRebalanceUnbondReserve`; that bucket tracks module rebalance unbond-in-flight until credited or reconciled.
- `stake()` delegates through `staking.delegateToBondedValidators(address(this), liquid, maxValidators)`.
- The staking precompile path is atomic at transaction scope: if any internal per-validator delegate fails, no partial delegation state persists.
- Validator selection policy for `stake()` is the first `maxValidators` bonded validators in staking precompile/keeper order.
- Delegation split policy is deterministic: `amount / n` base per validator and `amount % n` remainder distributed as `+1` to the first remainder validators.
- `syncTotalStaked` is accounting-only and must not create staking side effects. It updates bonded `totalStaked` only; it does not set `pendingRebalanceUnbondReserve` (full bucket sync is `reconcileStakedBuckets`).

## Poolrebalancer stub + matured-credit assumptions

Some specs construct a `poolrebalancerkeeper.Keeper` with a stub `EVMKeeper` and call `poolrebalancer.BeginBlocker` / `EndBlocker` directly:

- **Matured undelegation credits** (`creditStakeableFromRebalance`) rely on a **transient-store snapshot** written in **`BeginBlocker`** (`PrepareMaturedPoolUndelegationCredits`). Calling **`EndBlocker` alone** in a context where matured module-queue batches exist but **`BeginBlocker` did not run that block** can fail (missing snapshot). Full `network.NextBlock()` runs the app’s ordered Begin/EndBlock for all modules, which is why some scenarios advance blocks instead of only invoking `EndBlocker`.
- For ordering details and operator behavior on a real node, see `docs/poolrebalancer/community_pool_runbook.md`.

## Where keeper and integration tests cover poolrebalancer safety

- **Failed credit before queue cleanup** (EVM execution revert or `CallEVM` transport error): `CompletePendingUndelegations` must retain module queue rows, validator index keys, and the BeginBlock transient credit sum; `CommunityPoolReconcileDirty` is set only after a **successful** credit. Exercised in [`x/poolrebalancer/keeper/undelegation_test.go`](../../../../x/poolrebalancer/keeper/undelegation_test.go) (`TestCompletePendingUndelegations_RetainsQueueOnCreditVMFailure`, `TestCompletePendingUndelegations_RetainsQueueOnCreditCallEVMError`, `TestCompletePendingUndelegations_RetryAfterCreditVMFailureSucceeds`).
- **Unset `PoolDelegatorAddress`**: `PrepareMaturedPoolUndelegationCredits` writes a zero transient sum; `CompletePendingUndelegations` still removes matured module-queue entries and does **not** call `creditStakeableFromRebalance`. Exercised by `TestPrepareAndComplete_PoolDelegatorEmpty_SkipsCreditAndClearsMaturedQueue` in the same keeper file.
- **Integration cross-check**: when `EndBlocker` fails on matured batches **before** any EVM credit (missing transient snapshot), the spec *fails poolrebalancer EndBlock alone on matured undelegations then clears via full block progression* in [`test_integration.go`](./test_integration.go) asserts unchanged CommunityPool views (`pendingRebalanceUnbondReserve`, `stakeablePrincipalLedger`, `totalStaked`, `principalAssets`).

## Stability notes

- Integration suites built on `network.NewUnitTestNetwork` (CommunityPool Ginkgo, poolrebalancer stub-EVM, etc.) need **`-tags=test`** (singular, not `tests`) so the `test`-tag build of `x/vm/types` provides `EVMConfigurator.ResetTestConfig`.
- Two UBD entries sharing `CompletionTime` but differing `CreationHeight`: logic is covered in `x/poolrebalancer/keeper` unit tests. Real-staking coverage lives in **`tests/integration/x/poolrebalancer`** (`TestUndelegationMultiEntry_SameCompletionDifferentCreationHeight`), using short genesis `UnbondingTime` and `NextBlockAfter(0)` on the second leg so both entries share the same completion instant.

- If staking precompile validator ordering or bonded-set query semantics change, staking-path tests may fail and need expectation updates.
- If default gas behavior changes in factory or precompiles, tx helper gas defaults may need adjustment.
- If ownership/permissions policy changes, tests must be updated to reflect the new access model.
