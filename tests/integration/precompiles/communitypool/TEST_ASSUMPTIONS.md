# CommunityPool Integration Test Assumptions

This document captures assumptions that the `communitypool` integration suite depends on for deterministic behavior.

## Environment assumptions

- The suite runs against the standard integration test network created by `network.NewUnitTestNetwork`.
- The chain has a valid staking bond denom and an ERC20 token pair for that denom.
- At least one active validator exists in the network validator set.

## Contract + artifact assumptions

- `contracts/solidity/pool/CommunityPool.json` matches the current `CommunityPool.sol` implementation.
- The artifact includes the owner-only `setStakeValidators(string[])` method used by staking/harvest tests.
- `contracts/community_pool.go` successfully loads that artifact via `LoadCommunityPool()`.

## Test helper assumptions

- Read-only contract checks use `QueryContract(...)` (not tx execution), so nonce state is not mutated by view calls.
- Successful tx helper (`execTxExpectSuccess`) sets a default gas limit when none is provided, to avoid estimator/limit edge cases in precompile-heavy paths (for example, `harvest`).
- Tests commit blocks (`network.NextBlock()`) between state-changing calls that require finalized state for subsequent reads/assertions.

## Behavioral assumptions under test

- Deposit/withdraw accounting uses floor rounding and must never over-mint shares.
- Dust deposits that mint zero units must revert and preserve unit state.
- Owner-gated methods (`setConfig`, `syncTotalStaked`, `transferOwnership`, `setStakeValidators`) enforce access control.
- `stake()` and `harvest()` are callable in the current implementation and are tested as operational actions, not owner-only actions.
- `stake()` uses configured bech32 validator operator addresses set through `setStakeValidators`.
- `syncTotalStaked` is accounting-only and must not create staking side effects.

## Stability notes

- If staking precompile output format for validators changes (for example, address encoding), staking-path tests may fail and need contract or test adaptation.
- If default gas behavior changes in factory or precompiles, tx helper gas defaults may need adjustment.
- If ownership/permissions policy changes (for example, restricting `stake`/`harvest`), tests must be updated to reflect the new access model.
