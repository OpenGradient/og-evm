# CommunityPool Contract

The `CommunityPool` contract is a pooled staking vault for a single bond token.
Users deposit tokens and receive internal ownership units, while the contract
stakes principal through staking precompiles and handles rewards/withdrawals.

## Goals

- Keep pool ownership simple (`unitsOf[user] / totalUnits`).
- Separate principal accounting from reward accounting.
- Support async withdrawals for staked principal (request now, claim at maturity).
- Keep heavy validator selection logic in precompiles.

## Main Components

- **Bond token**: `bondToken` (ERC20 representation of chain bond denom).
- **Ownership units**: `unitsOf`, `totalUnits`.
- **Principal accounting**:
  - `stakeablePrincipalLedger`: liquid principal available for `stake`.
  - `totalStaked`: accounting view of delegated principal.
  - `pendingWithdrawReserve`: requested principal waiting for maturity.
  - `maturedWithdrawReserve`: matured principal reserved for claims.
- **Rewards accounting**:
  - `rewardReserve`: liquid rewards reserved for users.
  - `accRewardPerUnit` + `rewardDebt[user]`: index-based reward accrual.

## Lifecycle

### 1) Deposit

`deposit(amount)`:

- Reverts on `amount == 0`.
- Claims caller pending rewards first (to keep reward index accounting fair).
- Mints units:
  - first deposit: `mintedUnits = amount`
  - otherwise: `mintedUnits = floor(amount * totalUnits / principalAssets())`
- Reverts with `ZeroMintedUnits()` if floor rounding gives `0`.
- Transfers tokens in and increases `stakeablePrincipalLedger`.

### 2) Stake

`stake()`:

- Callable only by `owner` or `automationCaller`.
- No-op when `stakeablePrincipalLedger < minStakeAmount`.
- Calls staking precompile `delegateToBondedValidators(address(this), liquid, maxValidators)`.
- Moves delegated amount from liquid principal ledger to `totalStaked`.

### 3) Harvest and claim rewards

`harvest()`:

- Callable only by `owner` or `automationCaller`.
- Calls distribution precompile to claim validator rewards to contract balance.
- Computes `harvestedAmount = liquidAfter - liquidBefore`.
- Adds harvested rewards to `rewardReserve`.
- Updates `accRewardPerUnit` if `totalUnits > 0`.

`claimRewards()`:

- Uses reward index delta per user:  
  `pending = unitsOf[user] * accRewardPerUnit / PRECISION - rewardDebt[user]`.
- Transfers pending rewards from `rewardReserve`.
- Updates `rewardDebt[user]`.

### 4) Async withdraw

`withdraw(userUnits)`:

- Reverts on invalid unit input/balance.
- Claims caller pending rewards first.
- Computes principal out using staked-only model:  
  `amountOut = userUnits * totalStaked / totalUnits`.
- Calls staking precompile:
  `undelegateFromBondedValidators(address(this), amountOut, maxValidators)`.
- Requires exact undelegation amount and valid future completion time.
- Burns units immediately.
- Decreases `totalStaked`, increases `pendingWithdrawReserve`.
- Stores a `WithdrawRequest` with maturity.

`claimWithdraw(requestId)`:

- Checks ownership, maturity, and not already claimed.
- Moves reserve once from `pendingWithdrawReserve` to `maturedWithdrawReserve`.
- Pays out from matured reserve and marks request claimed.

## Key View Methods

- `liquidBalance()`: current token balance held by contract.
- `principalLiquid()`: currently stakeable liquid principal.
- `principalAssets()`: `principalLiquid + totalStaked`.
- `pricePerUnit()`: `principalAssets * 1e18 / totalUnits` (or `1e18` if empty).
- `totalWithdrawCommitments()`: pending + matured principal commitments.

## Invariants Enforced On State Changes

Internal `_assertReserveInvariant()` ensures:

- `rewardReserve <= liquidBalance`
- `rewardReserve + maturedWithdrawReserve <= liquidBalance`
- `stakeablePrincipalLedger + rewardReserve + maturedWithdrawReserve <= liquidBalance`

Note: `pendingWithdrawReserve` is excluded from liquid-reserve checks because it
represents principal already requested for unbonding, not immediately liquid.

## Admin Operations

- `setConfig(maxRetrieve, maxValidators, minStakeAmount)` (`maxValidators > 0`).
- `setAutomationCaller(newAutomationCaller)` to configure scheduler/module caller.
- `syncTotalStaked(newTotalStaked)` to reconcile accounting drift (e.g. slashing).
- `transferOwnership(newOwner)`.

All admin methods are `onlyOwner`.

## PoolRebalancer EndBlock Automation

`CommunityPool.stake()` and `CommunityPool.harvest()` can be called by the
`poolrebalancer` module during `EndBlock`.

### Required Configuration

1. Set CommunityPool automation caller to the poolrebalancer module EVM address:

- `setAutomationCaller(<poolrebalancer_module_evm_address>)`

2. Set poolrebalancer params so the pool delegator is the CommunityPool contract:

- `pool_delegator_address = <community_pool_contract_address_as_bech32_acc>`

Both are required. If either is wrong, EndBlock automation will not execute
successfully.

### Why This Is Required

- `stake()` and `harvest()` are protected by `onlyAutomationOrOwner`.
- EndBlock calls run with `msg.sender = poolrebalancer ModuleEVMAddress`.
- The module targets the contract at `pool_delegator_address` (address bytes
  mapped to EVM address).

### Operational Checks

Before enabling automation:

- `automationCaller` on the contract equals poolrebalancer module EVM address.
- `poolrebalancer.params.pool_delegator_address` equals the CommunityPool
  contract address (bech32 account form).

### Failure Symptoms

- `Unauthorized()` revert from `stake()` or `harvest()`:
  - `automationCaller` does not match module EVM address.
- Automation logs failures and state does not move:
  - `pool_delegator_address` is wrong or not the pool contract.

### Notes

- EndBlock automation is best-effort; errors are logged and retried in later
  blocks.
- Automation and rebalance are independent best-effort steps in EndBlock.

### `creditStakeableFromRebalance` (poolrebalancer / module undelegations)

When the **poolrebalancer** module **undelegates** the pool delegator on-chain
(in a path that does **not** go through `withdraw()`), bonded principal drops
but contract `totalStaked` would otherwise stay too high until reconciled.

`creditStakeableFromRebalance(amount)` fixes that **after** unbonded tokens have
landed as liquid on the pool: it increases `stakeablePrincipalLedger` and
decreases `totalStaked` by the same `amount`, so `principalAssets()` stays
consistent. It enforces `amount <= totalStaked` and the usual liquid/ledger
invariants.

**Who may call it:** `owner` or `automationCaller` (same ACL as `stake` /
`harvest`). In production, **`automationCaller`** should be the poolrebalancer
**module EVM address**; the keeper invokes this via **`CallEVM`** using that
sender.

**EndBlock order (application):** the **staking** module completes matured
unbonding entries and pays out **before** poolrebalancer `EndBlock` runs. The
rebalancer then **`CompletePendingUndelegations`** (strict: EVM credit, then
queue delete), then best-effort **`harvest` / `stake`** automation. So the
payout is already in the pool balance when `creditStakeableFromRebalance` runs.

## Error Model (selected)

- Input/permission: `InvalidAmount`, `InvalidUnits`, `InvalidConfig`, `Unauthorized`.
- External trust boundaries:
  - `UnexpectedUndelegatedAmount`
  - `InvalidCompletionTime`
  - `HarvestFailed`
- Reserve/invariant failures:
  - `InsufficientLiquid`
  - `RewardReserveInvariantViolation`
  - `LiquidReserveInvariantViolation`
  - `StakeablePrincipalInvariantViolation`

## Test Coverage

Integration tests for this contract are under:

- `tests/integration/precompiles/communitypool/test_integration.go`
- `tests/integration/precompiles/communitypool/test_utils.go`
- `tests/integration/precompiles/communitypool/TEST_ASSUMPTIONS.md`

They cover core lifecycle flow, custom-error paths, ownership/config transitions,
accounting invariants, and event assertions for critical operations.

