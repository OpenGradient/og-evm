# Pool Rebalancer + CommunityPool Runbook

This runbook documents the **redelegation-only** rebalancer model and the
corresponding CommunityPool automation behavior.

For contract API details, see
[`contracts/solidity/pool/README.md`](../../contracts/solidity/pool/README.md).

## Model

- Rebalancing is performed through **redelegations only**.
- EndBlock automation for CommunityPool uses:
    - `reconcileTotalStaked(uint256)` (automation caller only),
    - `harvest()`,
    - `stake()`.
- User withdrawals remain unchanged and still use the contract withdraw cycle.

## Required Configuration

- `poolrebalancer.params.pool_delegator_address` must be the CommunityPool
  bech32 account.
- CommunityPool `automationCaller` must be set to the poolrebalancer module EVM
  address.
- Rebalancer tuning params:
  `max_target_validators`, `rebalance_threshold_bp`, `max_ops_per_block`,
  `max_move_per_op`.

## Accounting Invariants

- `totalStaked` is the bonded delegated principal.
- `stakeablePrincipalLedger` is liquid principal available for stake.
- `principalAssets()` is expected to remain:
  `stakeablePrincipalLedger + totalStaked`.
- `withdraw()` is guarded when `stakeablePrincipalLedger > 0`, so users cannot
  withdraw while principal is still liquid and not fully bonded.

## EndBlock Flow

1. Complete matured pending redelegation tracking.
2. Best-effort CommunityPool reconcile via `reconcileTotalStaked`.
3. Best-effort CommunityPool automation (`harvest` then `stake`).
4. Best-effort `ProcessRebalance`.
5. Optional post-rebalance best-effort second reconcile pass via `reconcileTotalStaked`
   (enabled by test hook, disabled by default in production keeper).

Strict failures only apply to strict keeper phases. Reconcile/automation/rebalance
remain best-effort and are retried on later blocks.

## Monitoring

Primary signals:

- Module logs around `process rebalance`, `community pool reconcile`,
  and `community pool automation`.
- `evmd query poolrebalancer pending-redelegations`.
- CommunityPool views:
    - `totalStaked()`
    - `stakeablePrincipalLedger()`
    - `principalAssets()`

Common issues:

- `Unauthorized` reverts on pool automation calls: automation caller mismatch.
- Reconcile drift: compare delegated bonded total vs `totalStaked` and verify
  `reconcileTotalStaked` is being called from the module EVM address.

## Related Files

- `x/poolrebalancer/abci.go`
- `x/poolrebalancer/keeper/rebalance.go`
- `x/poolrebalancer/keeper/community_pool_reconcile.go`
- `x/poolrebalancer/keeper/community_pool_reconcile_abci.go`
- `x/poolrebalancer/keeper/community_pool.go`
- `contracts/solidity/pool/CommunityPool.sol`
