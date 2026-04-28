# CommunityPool contract

The `CommunityPool` contract is a pooled staking vault for a single bond token.
Users deposit tokens and receive internal ownership units, while the contract
stakes principal through staking precompiles and handles rewards and async withdrawals.

For **poolrebalancer module** configuration, ABCI ordering, and
`reconcileTotalStaked` behavior, see
[`docs/poolrebalancer/community_pool_runbook.md`](../../../docs/poolrebalancer/community_pool_runbook.md).

## Goals

- Keep pool ownership simple (`unitsOf[user] / totalUnits`).
- Separate principal accounting (liquid, bonded, withdraw reserves) from reward accounting.
- Support async withdrawals for staked principal (request now, claim at maturity).
- Keep heavy validator selection logic in precompiles.

## Main components

- **Bond token**: `bondToken` (ERC20 representation of chain bond denom).
- **Ownership units**: `unitsOf`, `totalUnits`.
- **Principal accounting**:
    - `stakeablePrincipalLedger`: liquid principal available for `stake`.
    - `totalStaked`: accounting view of **bonded** delegated principal.
    - `pendingWithdrawReserve` / `maturedWithdrawReserve`: async **user** withdraw pipeline.
- **Rewards accounting**:
    - `rewardReserve`, `accRewardPerUnit`, `rewardDebt[user]`: index-based reward accrual.

## Lifecycle

### 1) Deposit

`deposit(amount)`:

- Reverts on `amount == 0`.
- Claims caller pending rewards against the current reward index.
- Mints units:
    - first deposit: `mintedUnits = amount`
    - otherwise: `mintedUnits = floor(amount * totalUnits / principalAssets())`
- Rejects deposit when `totalUnits == 0` but `principalAssets() > 0` (`ZeroUnitsWithPrincipalAssets`), preventing orphan-accounted principal from being captured by a new first depositor.
- `principalAssets()` = `stakeablePrincipalLedger + totalStaked`.
- Reverts with `ZeroMintedUnits()` if floor rounding gives `0`.
- Transfers tokens in and increases `stakeablePrincipalLedger`.

### 2) Stake

`stake()`:

- Callable only by `owner` or `automationCaller`.
- No-op when `stakeablePrincipalLedger < minStakeAmount`.
- Calls staking precompile `delegateToBondedValidators(address(this), liquid, maxValidators)`.
- Validator choice and remainder ordering come from the staking precompile's bonded-validator query order; the poolrebalancer separately targets bonded-by-power order and corrects drift after staking.
- Moves delegated amount from `stakeablePrincipalLedger` to `totalStaked`.

### 3) Harvest and claim rewards

`harvest()`:

- Callable only by `owner` or `automationCaller`.
- Reverts with `EmptyPool()` when `totalUnits == 0`; rewards are not claimed into `rewardReserve` unless they can be distributed through the reward index.
- Calls distribution precompile to claim validator rewards to the contract balance.
- Updates `rewardReserve` and `accRewardPerUnit` for positive harvested rewards.

`claimRewards()`:

- Uses reward index delta per user; transfers from `rewardReserve`.

### 4) Async withdraw (user)

`withdraw(userUnits)`:

- Claims caller pending rewards against the current reward index before unit burn.
- `amountOut = userUnits * totalStaked / totalUnits` (**bonded principal only**).
- Conservative pre-audit guard: withdraw is allowed only when all withdraw-relevant principal is bonded.
    - Full exit rejects with `FullExitLeavesNonStakedPrincipal(uint256)` when non-staked principal remains.
    - Partial withdraw rejects with `WithdrawRequiresAllPrincipalBonded(uint256)` when non-staked principal remains.
- Calls `undelegateFromBondedValidators`; burns units; decreases `totalStaked`; increases `pendingWithdrawReserve`.

`claimWithdraw(requestId)`:

- Moves reserve to matured, then pays out after maturity.

### 5) Total reconcile (automation only)

`reconcileTotalStaked(newTotalStaked)`:

- Callable only by **`automationCaller`** (not `owner`).
- Sets bonded accounting to match keeper-computed staking truth.
- Owner may use `syncTotalStaked` for owner-driven bonded-only adjustments.

## Key view methods

- `liquidBalance()`: ERC20 balance of the contract.
- `principalLiquid()`: `stakeablePrincipalLedger`.
- `principalAssets()`: `stakeablePrincipalLedger + totalStaked`.
- `pricePerUnit()`: `principalAssets * 1e18 / totalUnits` (or `1e18` if `totalUnits == 0`).
- `totalWithdrawCommitments()`: `pendingWithdrawReserve + maturedWithdrawReserve`.

## Invariants enforced on state changes

`_assertReserveInvariant()` (on deposit, stake, reward-claim, withdraw paths, etc.):

- `rewardReserve <= liquidBalance`
- `rewardReserve + maturedWithdrawReserve <= liquidBalance`
- `stakeablePrincipalLedger + rewardReserve + maturedWithdrawReserve <= liquidBalance`

`pendingWithdrawReserve` is excluded from liquid checks (principal requested for unbonding, not yet claim-ready). `reconcileTotalStaked` does **not** invoke this invariant (no balance movement).

## Admin operations

- `setConfig(...)`, `setAutomationCaller(...)`, `syncTotalStaked(...)`, `transferOwnership(...)`: **`onlyOwner`**.
- `setAutomationCaller`: configures the address that may call `reconcileTotalStaked` and (with owner) `stake` / `harvest`. In production this should be the **poolrebalancer module EVM address** (see runbook).

## Poolrebalancer EndBlock automation

The module calls the pool contract with **`msg.sender =` module EVM address** (same as `automationCaller` on the contract).

### Required configuration

1. `setAutomationCaller(<poolrebalancer_module_evm_address>)` on CommunityPool.
2. `poolrebalancer.params.pool_delegator_address =` CommunityPool account (bech32).

### EndBlock order (application)

After **staking** has finished matured unbonding payouts for the block:

1. **Strict**: complete pending redelegations.
2. **Best-effort**: **`reconcileTotalStaked`**, then **`harvest`**, then **`stake`**, then rebalance processing, then a post-rebalance **`reconcileTotalStaked`** pass on successful rebalance.

See the runbook for halting vs best-effort behavior.

### ACL summary

- `reconcileTotalStaked`: **`automationCaller` only**.
- `stake`, `harvest`: `owner` or `automationCaller`.
- `syncTotalStaked`: `owner`.

### Failure symptoms

- `Unauthorized()` on `stake` / `harvest`: `automationCaller` mismatch or wrong sender.
- `Unauthorized()` on `reconcileTotalStaked`: **owner** or any address other than `automationCaller` (unless automation was retargeted).

## Events (indexers)

- `TotalStakedReconciled(previousTotalStaked, newTotalStaked)`.

## Error model (selected)

- Permissions / inputs: `InvalidAmount`, `InvalidUnits`, `InvalidConfig`, `EmptyPool`, `Unauthorized`.
- Exit safety: `FullExitLeavesNonStakedPrincipal`.
- External: `UnexpectedUndelegatedAmount`, `InvalidCompletionTime`, `HarvestFailed`.
- Reserves: `InsufficientLiquid`, `RewardReserveInvariantViolation`, `LiquidReserveInvariantViolation`, `StakeablePrincipalInvariantViolation`.

## Test coverage

- **Foundry** (pool-focused): `contracts/test/pool/CommunityPoolWithdrawStake.t.sol` — run from `contracts/` with Forge (see `foundry.toml` and file headers).
- **Go** artifact smoke: `contracts/community_pool_test.go`.
- **Integration** (Ginkgo): `tests/integration/precompiles/communitypool/` — `test_integration.go`, `test_utils.go`, `TEST_ASSUMPTIONS.md`.
    - Integration validates request/maturity/claim outputs and invariants.
    - Exact ERC20 balance-delta equality for `claimWithdraw` is asserted in Forge (deterministic local mocks).
