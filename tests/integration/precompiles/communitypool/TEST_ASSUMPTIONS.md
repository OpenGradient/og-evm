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
- Owner-gated methods (`setConfig`, `syncTotalStaked`, `transferOwnership`) enforce access control.
- `stake()` and `harvest()` are restricted to `owner` or configured `automationCaller`.
- `reconcileTotalStaked` is restricted to `automationCaller` only (not `owner`).
- `syncTotalStaked` remains owner-only break-glass for bonded accounting sync.
- `principalAssets` is `stakeablePrincipalLedger + totalStaked`; `pricePerUnit` and deposit minting use that total.
- User `withdraw` sizes `amountOut` from **`totalStaked` only** (proportional to units burned).
- Conservative pre-audit policy: any `withdraw` requires all withdraw-relevant principal to be bonded.
  If `stakeablePrincipalLedger > 0`, withdraw reverts.
- Full-exit safety rule: `withdraw(userUnits == totalUnits)` reverts with
  `FullExitLeavesNonStakedPrincipal(uint256)` when `stakeablePrincipalLedger > 0`.
- Partial withdraw under non-bonded principal reverts with
  `WithdrawRequiresAllPrincipalBonded(uint256)`.
- `stake()` delegates through `staking.delegateToBondedValidators(address(this), liquid, maxValidators)`.
- The staking precompile path is atomic at transaction scope: if any internal per-validator delegate fails, no partial delegation state persists.
- Validator selection policy for `stake()` is the first `maxValidators` bonded validators in staking precompile query order.
- Poolrebalancer target selection is independently the staking keeper bonded-by-power top-`max_target_validators` set. Exact ordering equivalence is not required; rebalance is the intended drift-correction path.
- Delegation split policy is deterministic: `amount / n` base per validator and `amount % n` remainder distributed as `+1` to the first remainder validators.
- `syncTotalStaked` is accounting-only and must not create staking side effects. It updates bonded `totalStaked` only.
- Withdraw maturity lifecycle is covered end-to-end: withdraw request creation, maturity advance, `claimWithdraw` payout, request claimed flag, and reserve invariants.
- Integration asserts `claimWithdraw` return values and state transitions; exact ERC20 wallet balance-delta equality is validated in Forge tests where token flows are fully deterministic.

## Out-of-scope / covered elsewhere

- Dust deposit (`ZeroMintedUnits`) and specific `FullExitLeavesNonStakedPrincipal` edge cases are covered in Forge tests under `contracts/test/pool/`.
- Detailed staking-precompile internal atomicity and validator ordering semantics are covered in precompile and module-level tests; this integration suite validates contract behavior against chain wiring.

## Poolrebalancer assumptions

- Poolrebalancer EndBlock automation continues to run `harvest`/`stake` and bonded-only reconcile via `reconcileTotalStaked`.

## Stability notes

- Integration suites built on `network.NewUnitTestNetwork` (CommunityPool Ginkgo, poolrebalancer stub-EVM, etc.) need **`-tags=test`** (singular, not `tests`) so the `test`-tag build of `x/vm/types` provides `EVMConfigurator.ResetTestConfig`.
- Redelegation queue maturity and cleanup semantics are covered by `x/poolrebalancer/keeper` unit tests and the poolrebalancer integration suite.

- If staking precompile validator ordering or bonded-set query semantics change, tests should still hold if rebalance converges stake into the keeper target set; update expectations only if the explicit policy above changes.
- If default gas behavior changes in factory or precompiles, tx helper gas defaults may need adjustment.
- If ownership/permissions policy changes, tests must be updated to reflect the new access model.
