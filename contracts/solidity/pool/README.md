# CommunityPool contract

The `CommunityPool` contract is a pooled staking vault for a single bond token.
Users deposit tokens and receive internal ownership units, while the contract
stakes principal through staking precompiles and handles rewards and async withdrawals.

For **poolrebalancer module** configuration, ABCI ordering, maturity credit, and
`reconcileStakedBuckets` behavior, see
[`docs/poolrebalancer/community_pool_runbook.md`](../../../docs/poolrebalancer/community_pool_runbook.md).

## Goals

- Keep pool ownership simple (`unitsOf[user] / totalUnits`).
- Separate principal accounting (liquid, bonded, module rebalance unbond-in-flight, withdraw reserves) from reward accounting.
- Support async withdrawals for staked principal (request now, claim at maturity).
- Keep heavy validator selection logic in precompiles.

## Main components

- **Bond token**: `bondToken` (ERC20 representation of chain bond denom).
- **Ownership units**: `unitsOf`, `totalUnits`.
- **Principal accounting**:
  - `stakeablePrincipalLedger`: liquid principal available for `stake` (and increased by `creditStakeableFromRebalance`).
  - `totalStaked`: accounting view of **bonded** delegated principal (excludes module rebalance unbond-in-flight).
  - `pendingRebalanceUnbondReserve`: principal that **left bonded** via **module-tracked** rebalance undelegations and is still unbonding on-chain until maturity credit; drives `principalAssets()` together with ledger + bonded.
  - `pendingWithdrawReserve` / `maturedWithdrawReserve`: async **user** withdraw pipeline.
- **Rewards accounting**:
  - `rewardReserve`, `accRewardPerUnit`, `rewardDebt[user]`: index-based reward accrual.

## Lifecycle

### 1) Deposit

`deposit(amount)`:

- Reverts on `amount == 0`.
- Claims caller pending rewards first (fair index accounting).
- Mints units:
  - first deposit: `mintedUnits = amount`
  - otherwise: `mintedUnits = floor(amount * totalUnits / principalAssets())`
- `principalAssets()` = `stakeablePrincipalLedger + totalStaked + pendingRebalanceUnbondReserve`.
- Reverts with `ZeroMintedUnits()` if floor rounding gives `0`.
- Transfers tokens in and increases `stakeablePrincipalLedger`.

### 2) Stake

`stake()`:

- Callable only by `owner` or `automationCaller`.
- No-op when `stakeablePrincipalLedger < minStakeAmount`.
- Calls staking precompile `delegateToBondedValidators(address(this), liquid, maxValidators)`.
- Moves delegated amount from `stakeablePrincipalLedger` to `totalStaked` (does **not** change `pendingRebalanceUnbondReserve`).

### 3) Harvest and claim rewards

`harvest()`:

- Callable only by `owner` or `automationCaller`.
- Calls distribution precompile to claim validator rewards to the contract balance.
- Updates `rewardReserve` and `accRewardPerUnit` when `totalUnits > 0`.

`claimRewards()`:

- Uses reward index delta per user; transfers from `rewardReserve`.

### 4) Async withdraw (user)

`withdraw(userUnits)`:

- Claims caller pending rewards first.
- `amountOut = userUnits * totalStaked / totalUnits` (**bonded** principal only; **not** `pendingRebalanceUnbondReserve`).
- Calls `undelegateFromBondedValidators`; burns units; decreases `totalStaked`; increases `pendingWithdrawReserve`.

`claimWithdraw(requestId)`:

- Moves reserve to matured, then pays out after maturity.

### 5) Module maturity credit

`creditStakeableFromRebalance(amount)`:

- Callable only by `owner` or `automationCaller`.
- After rebalance unbonds mature and tokens are liquid on the pool, moves `amount` from `pendingRebalanceUnbondReserve` into `stakeablePrincipalLedger`.
- Requires `amount <= pendingRebalanceUnbondReserve`; keeps `principalAssets()` unchanged.
- Used by **poolrebalancer** `EndBlock` with `automationCaller =` module EVM address (see runbook).

### 6) Bucket reconciliation (automation only)

`reconcileStakedBuckets(newTotalStaked, newPendingRebalanceUnbondReserve)`:

- Callable only by **`automationCaller`** (not `owner`).
- Atomically sets bonded and module pending buckets to match off-chain / keeper-computed staking truth.
- Does **not** run `_assertReserveInvariant()` (does not move tokens); incorrect values can break `creditStakeableFromRebalance` and **halt** the chain if maturity credit exceeds pending.
- Owner may use `syncTotalStaked` for **bonded-only** adjustments, or temporarily `setAutomationCaller` for a full two-bucket repair.

## Key view methods

- `liquidBalance()`: ERC20 balance of the contract.
- `principalLiquid()`: `stakeablePrincipalLedger`.
- `principalAssets()`: `stakeablePrincipalLedger + totalStaked + pendingRebalanceUnbondReserve`.
- `pricePerUnit()`: `principalAssets * 1e18 / totalUnits` (or `1e18` if `totalUnits == 0`).
- `totalWithdrawCommitments()`: `pendingWithdrawReserve + maturedWithdrawReserve`.
- `pendingRebalanceUnbondReserve()`: module rebalance unbond-in-flight (principal accounting).

## Invariants enforced on state changes

`_assertReserveInvariant()` (on deposit, stake, credit, withdraw paths, etc.):

- `rewardReserve <= liquidBalance`
- `rewardReserve + maturedWithdrawReserve <= liquidBalance`
- `stakeablePrincipalLedger + rewardReserve + maturedWithdrawReserve <= liquidBalance`

`pendingWithdrawReserve` is excluded from liquid checks (principal requested for unbonding, not yet claim-ready). `reconcileStakedBuckets` does **not** invoke this invariant (no balance movement).

## Admin operations

- `setConfig(...)`, `setAutomationCaller(...)`, `syncTotalStaked(...)`, `transferOwnership(...)`: **`onlyOwner`**.
- `setAutomationCaller`: configures the address that may call `reconcileStakedBuckets` and (with owner) `stake` / `harvest` / `creditStakeableFromRebalance`. In production this should be the **poolrebalancer module EVM address** (see runbook).

## Poolrebalancer EndBlock automation

The module calls the pool contract with **`msg.sender =` module EVM address** (same as `automationCaller` on the contract).

### Required configuration

1. `setAutomationCaller(<poolrebalancer_module_evm_address>)` on CommunityPool.
2. `poolrebalancer.params.pool_delegator_address =` CommunityPool account (bech32).

### EndBlock order (application)

After **staking** has finished matured unbonding payouts for the block:

1. **Strict**: complete pending redelegations and **complete pending undelegations** (may call `creditStakeableFromRebalance`, then remove module queue entries).
2. **Best-effort**: **`reconcileStakedBuckets`** (if dirty or sweep block), then **`harvest`**, then **`stake`**, then rebalance processing (and optional second reconcile in test builds).

See the runbook for halting vs best-effort behavior and **liveness** requirements on `pendingRebalanceUnbondReserve`.

### ACL summary

| Function | Who may call |
|----------|----------------|
| `reconcileStakedBuckets` | **`automationCaller` only** |
| `stake`, `harvest`, `creditStakeableFromRebalance` | `owner` or `automationCaller` |
| `syncTotalStaked` | `owner` |

### Failure symptoms

- `Unauthorized()` on `stake` / `harvest` / `creditStakeableFromRebalance`: `automationCaller` mismatch or wrong sender.
- `Unauthorized()` on `reconcileStakedBuckets`: **owner** or any address other than `automationCaller` (unless automation was retargeted).

## Events (indexers)

- `CreditStakeableFromRebalance(amount, stakeablePrincipalLedgerAfter, pendingRebalanceUnbondReserveAfter)` — third field added for pending tracking; update decoders if you consumed the old two-field layout.
- `StakedBucketsReconciled(previousTotalStaked, newTotalStaked, previousPending, newPending)`.

## Error model (selected)

- Permissions / inputs: `InvalidAmount`, `InvalidUnits`, `InvalidConfig`, `Unauthorized`.
- External: `UnexpectedUndelegatedAmount`, `InvalidCompletionTime`, `HarvestFailed`.
- Reserves: `InsufficientLiquid`, `RewardReserveInvariantViolation`, `LiquidReserveInvariantViolation`, `StakeablePrincipalInvariantViolation`.

## Test coverage

- **Foundry** (pool-focused): `contracts/test/pool/CommunityPoolCredit.t.sol`, `CommunityPoolWithdrawStake.t.sol` — run from `contracts/` with Forge (see `foundry.toml` and file headers).
- **Go** artifact smoke: `contracts/community_pool_test.go`.
- **Integration** (Ginkgo): `tests/integration/precompiles/communitypool/` — `test_integration.go`, `test_utils.go`, `TEST_ASSUMPTIONS.md`.
