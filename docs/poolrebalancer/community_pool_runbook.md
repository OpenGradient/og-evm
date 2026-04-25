# Pool rebalancer and CommunityPool: operator runbook

This document describes how the **`x/poolrebalancer`** module interacts with the **`CommunityPool`** Solidity contract, how principal is accounted for across Cosmos staking and the EVM, and what operators must configure and monitor in production.

For contract-level API and invariants, see [`contracts/solidity/pool/README.md`](../../contracts/solidity/pool/README.md).

---

## 1. Role of the module

The pool rebalancer:

1. **Tracks** module-initiated **undelegations** and **redelegations** for a configured **pool delegator** account (typically the account whose bytes map to the CommunityPool contract address).
2. On undelegation **maturity**, **credits** liquid principal back into the pool contract via **`creditStakeableFromRebalance`**, using amounts aligned with **live staking unbonding state** (including post-slash balances), not only the module’s queued coin fields.
3. **Rebalances** stake across validators according to module parameters (separate from CommunityPool user flows).
4. **Best-effort EndBlock automation** on the CommunityPool: **`reconcileStakedBuckets`**, then **`harvest`**, then **`stake`**, driven by **`CallEVM`** from the module’s EVM sender.

Strict steps can **halt the block** if they fail; best-effort steps **log errors** and retry on later blocks.

---

## 2. Glossary

| Term | Meaning |
|------|--------|
| **Pool delegator** | `params.pool_delegator_address`: the Cosmos account whose stake the module manages for rebalance paths and whose EVM address is the CommunityPool contract. |
| **Module EVM address** | `types.ModuleEVMAddress`: EVM address derived from the `poolrebalancer` **module account** (`x/auth`). This must be set as CommunityPool **`automationCaller`** for EndBlock automation. |
| **`totalStaked` (contract)** | Accounting: principal currently **bonded** in the pool’s view (delegated), excluding module rebalance unbond-in-flight. |
| **`pendingRebalanceUnbondReserve` (contract)** | Accounting: principal that **left bonded** via **module-tracked** rebalance undelegations and is still **unbonding on staking**, until it is **credited** to `stakeablePrincipalLedger` at maturity. |
| **Module undelegation queue** | KV state: pending undelegation entries keyed by completion time and delegator, plus validator index keys. Used to know *what* matured and to compute expected pending reserve. |
| **Transient credit snapshot** | Per-block transient store: sum of **staking UBD** balances (per deduped triple) for **matured** module-queue entries, filled in **BeginBlock**, consumed in **EndBlock**. |
| **Reconcile dirty flag** | Persistent key `0x31`: requests a CommunityPool **`reconcileStakedBuckets`** on the next eligible EndBlock (or periodic sweep). |

User **`withdraw()`** on the contract uses staking undelegation but **does not** go through the module queue; it affects **`totalStaked`** / withdraw reserves on the contract only, not **`pendingRebalanceUnbondReserve`**.

---

## 3. Required configuration

### 3.1 Chain / module parameters

- **`pool_delegator_address`**: Bech32 account address of the CommunityPool contract (same bytes as the contract’s EVM address). Empty is only safe when no pool-tracked pending undelegation state exists. Runtime/gov safeguards reject unsafe transitions and BeginBlock fails if matured queue rows exist while this is empty.
- **`max_target_validators`**, **`rebalance_threshold_bp`**, **`max_ops_per_block`**, **`max_move_per_op`**, **`use_undelegate_fallback`**: control **validator rebalance** behavior (independent of CommunityPool deposit/withdraw UX). Rebalancing targets the staking keeper's **bonded-by-power** order, capped by **`max_target_validators`**; CommunityPool **`stake()`** delegates through the staking precompile's bonded-validator query order, then the module corrects any drift through rebalance.

Defaults are defined in `x/poolrebalancer/types/helpers.go` (`DefaultParams`).

### 3.2 CommunityPool contract

1. **`automationCaller`** must equal **`poolrebalancer` module EVM address** (`types.ModuleEVMAddress`, documented in `x/poolrebalancer/types/keys.go` and derived in `init()` from `authtypes.NewModuleAddress(ModuleName)`).

2. The module invokes the following methods via **`CallEVM`** with **`from = ModuleEVMAddress`** and **`to = pool contract`**:

   | Method | Purpose |
   |--------|--------|
   | `reconcileStakedBuckets(uint256,uint256)` | Set **`totalStaked`** and **`pendingRebalanceUnbondReserve`** to match computed staking truth. **`onlyAutomationCaller`**. |
   | `creditStakeableFromRebalance(uint256)` | Move matured rebalance unbond from **pending reserve** into **`stakeablePrincipalLedger`**. **`onlyAutomationOrOwner`**. |
   | `harvest()` | Claim distribution rewards to the pool. **`onlyAutomationOrOwner`**. |
   | `stake()` | Delegate liquid stakeable principal. **`onlyAutomationOrOwner`**. |

3. **Owner** cannot call **`reconcileStakedBuckets`** unless they temporarily **`setAutomationCaller`** to another address they control (operational “break glass”). For **bonded-only** fixes, **`syncTotalStaked`** remains **`onlyOwner`** and does **not** update **`pendingRebalanceUnbondReserve`**.

### 3.3 Application wiring

- **Staking `EndBlock`** must run **before** **`poolrebalancer` `EndBlock`** so unbonding payouts are **liquid** on the pool account before **`creditStakeableFromRebalance`**.
- **`poolrebalancer` `BeginBlock`** should run **after** slashing/evidence **`BeginBlock`** so the maturity credit snapshot sees **post-slash** unbonding balances. (Ordering is asserted in app-level tests such as `evmd/app_begin_block_order_test.go` where present.)

---

## 4. Contract principal model (summary)

- **`principalAssets()`** = `stakeablePrincipalLedger` + `totalStaked` + `pendingRebalanceUnbondReserve` (drives **deposit** minting and **`pricePerUnit`**).
- **`withdraw()`** sizes payouts from **`totalStaked` / `totalUnits`** only; it does **not** reduce **`pendingRebalanceUnbondReserve`**.
- Module **maturity credit** reduces **`pendingRebalanceUnbondReserve`** and increases **`stakeablePrincipalLedger`** by the same amount, leaving **`principalAssets()`** unchanged.

---

## 5. ABCI flow

### 5.1 BeginBlock

**`PrepareMaturedPoolUndelegationCredits`**

- Iterates **matured** module undelegation batches (completion time ≤ block time).
- For each **deduped** triple `(pool delegator, validator, completion time)`, sums **all** staking **`UnbondingDelegation`** entry balances matching that completion (handles merged/multiple entries and slash alignment).
- Writes the **total** to **transient** store (`maturedPoolUndelegationCreditTransientKey`).
- If **`pool_delegator_address`** is unset and there are **no matured batches**, writes **zero**.
- If **`pool_delegator_address`** is unset and matured batches **exist**, returns an error and halts the block (strict invariant).

**Errors**: returned to CometBFT and **halt** the block.

### 5.2 EndBlock (strict then best-effort)

Order in `x/poolrebalancer/abci.go`:

1. **`CompletePendingRedelegations`** — **strict** (error halts EndBlock).
2. **`CompletePendingUndelegations`** — **strict** (error halts EndBlock).
3. **`MaybeReconcileCommunityPoolStakedBuckets`** — **best-effort** (log only).
4. **`MaybeRunCommunityPoolAutomation`** (`harvest`, then `stake`) — **best-effort** (log only).
5. **`ProcessRebalance`** — **best-effort** (log only).
6. If **`ProcessRebalance`** returns **nil**, **`MaybeReconcileCommunityPoolStakedBucketsSecondPass`** may run (used in tests; default off in production keeper unless test hook enabled).

**Strict path: `CompletePendingUndelegations`**

- Loads matured batches from the **module queue**.
- Reads **credit sum** from **transient** store (must exist when there are matured batches — missing snapshot **errors** and halts).
- If `creditSum > 0`:
  - Requires **EVM keeper** and **non-empty** pool delegator.
  - Calls **`creditStakeableFromRebalance(creditSum)`** on the contract **before** deleting queue entries (so a failed EVM call leaves state for retry).
  - Sets **reconcile dirty** to **true** (so buckets are realigned after credit).
- Deletes queue keys and validator index keys; emits completion event.
- Clears transient snapshot to zero for idempotency.

**Liveness note**: The contract requires **`creditSum <= pendingRebalanceUnbondReserve`**. If **`reconcileStakedBuckets`** has been failing for a long time, **pending** on-chain can lag **below** the true matured amount → **`creditStakeableFromRebalance`** **reverts** → **EndBlock halts**. Monitor **dirty flag**, **reconcile** logs, and contract **pending** vs staking UBD.

### 5.3 When is `reconcileStakedBuckets` attempted?

**`MaybeReconcileCommunityPoolStakedBuckets`** runs if:

- **Dirty** is set, **or**
- **`block_height % 20 == 0`** (periodic **sweep** for slash / drift catch-up),

and **`pool_delegator_address`** is set and **EVM keeper** is non-nil.

Logic:

1. **Expected bonded** = sum over delegations of **truncated** token value from shares for validators in **`Bonded`** status (`ComputeExpectedBondedPrincipal`).
2. **Expected pending** = for each **immature** module-queue triple (completion **strictly after** block time), sum staking UBD balances for that triple (`ComputeExpectedPendingRebalancePrincipal`). **User** undelegations **not** on the module queue are **not** included.
3. **Static call** `totalStaked` and `pendingRebalanceUnbondReserve` on the contract; if they **match** expected, **clear dirty** and skip the tx.
4. Otherwise **`CallEVM`** **`reconcileStakedBuckets(expectedBonded, expectedPending)`** with **commit=true**.
5. On failure, keep or set **dirty** for retry; errors are **logged** by `EndBlocker` only.

**Dirty flag** is set when:

- **`BeginTrackedUndelegation`** / **`BeginTrackedRedelegation`** run for the **pool delegator** (staking layout changed).
- **`CompletePendingUndelegations`** performs a **positive** credit.

**Empty-pool harvest**: CommunityPool **`harvest()`** reverts with **`EmptyPool()`** when **`totalUnits == 0`**. EndBlock automation reads **`totalUnits`** first and skips **`harvest`** / **`stake`** while the pool is empty, preventing rewards from entering **`rewardReserve`** without an index owner.

**Delegation scan bound**: keeper paths that compute bonded stake use a centralized delegation scan and fail closed if the staking keeper returns exactly the scan limit. That boundary means the account may have more delegations than the response contains, so reconciliation/rebalance refuses to use a possibly truncated view instead of silently undercounting stake.

---

## 6. Maturity credit vs BeginBlock snapshot

The **credit amount** is the **sum prepared in BeginBlock** from **staking UBD** balances at that point in the block, **not** the raw `Balance` field stored in the module queue entry (which can diverge after **slashing**).

If staking no longer has a matching UBD entry for a queued triple, **Prepare** / **Complete** can **error** and halt — that indicates **desync** between module queue and staking and must be investigated.

---

## 7. EVM / ABI

The module embeds a **minimal** ABI in `x/poolrebalancer/types/communitypool_abi.json` for:

`stake`, `harvest`, `creditStakeableFromRebalance`, `reconcileStakedBuckets`, `totalStaked`, `pendingRebalanceUnbondReserve`.

The full artifact used elsewhere (e.g. Go contract tests) is `contracts/solidity/pool/CommunityPool.json`. **Keep method selectors aligned** when changing the contract.

---

## 8. Monitoring and troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| `Unauthorized` on automation txs | **`automationCaller`** ≠ module EVM address, or wrong **`from`** in `CallEVM`. |
| `EmptyPool` during direct `harvest` | Pool has **zero units**; EndBlock automation skips harvest until deposits create units. |
| `poolrebalancer: community pool staked buckets reconcile failed` (recurring) | EVM gas, contract revert, or **`ComputeExpectedCommunityPoolStakedBuckets`** error (e.g. missing UBD for queued triple). |
| `delegation scan reached maxRetrieve` | Pool delegator has reached the keeper scan boundary; reduce fragmentation or add true paginated keeper support before relying on reconciliation/rebalance accounting. |
| `complete pending undelegations failed` / block halt | **Credit** reverted: **`pendingRebalanceUnbondReserve` < creditSum**, missing transient snapshot with matured batches, nil EVM, empty pool delegator. |
| Contract **`totalStaked`** wrong but pending OK | Use **`syncTotalStaked`** (owner) for **bonded-only** fix; full two-bucket fix needs **automation** **`reconcileStakedBuckets`** (or temporary automation caller). |
| Deposit / pricePerUnit “wrong” after rebalance | **`principalAssets`** includes **`pendingRebalanceUnbondReserve`**; large pending increases denominator for new mints until credit clears pending. |

**Logs** (Cosmos SDK logger, module `poolrebalancer`): look for `prepare matured pool undelegation credits`, `complete pending undelegations`, `community pool staked buckets reconcile`, `community pool automation`, `process rebalance`.

---

## 9. Test map

| Area | Location |
|------|----------|
| Begin/End undelegation + credit ordering | `x/poolrebalancer/abci_test.go`, `x/poolrebalancer/keeper/undelegation_test.go` |
| Expected buckets + immature queue iteration | `x/poolrebalancer/keeper/community_pool_reconcile_test.go`, `community_pool_reconcile_abci_test.go` |
| CommunityPool Solidity (credit, reconcile ACL, withdraw/stake vs pending) | `contracts/test/pool/CommunityPoolCredit.t.sol`, `CommunityPoolWithdrawStake.t.sol` |
| Ginkgo EVM integration | `tests/integration/precompiles/communitypool/` (see `TEST_ASSUMPTIONS.md`) |

---

## 10. Related code entrypoints

- ABCI: `x/poolrebalancer/abci.go`
- Maturity prepare/complete: `x/poolrebalancer/keeper/undelegation.go`
- Expected buckets: `x/poolrebalancer/keeper/community_pool_reconcile.go`
- Reconcile + dirty store: `x/poolrebalancer/keeper/community_pool_reconcile_abci.go`
- EVM calls: `x/poolrebalancer/keeper/community_pool.go`
- Params: `x/poolrebalancer/keeper/params.go`, `x/poolrebalancer/types/helpers.go`
- Store keys: `x/poolrebalancer/types/keys.go`
