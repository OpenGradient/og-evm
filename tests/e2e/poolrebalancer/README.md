# Poolrebalancer E2E Scenario Runner

This document describes how to run manual E2E observation scenarios for `x/poolrebalancer`. For the full option and environment reference, run `--help` on the script below.

## Script

- `tests/e2e/poolrebalancer/rebalance_scenario_runner.sh`

## Purpose

- Bootstraps a multi-validator test chain via `multi_node_startup.sh`
- Patches staking and poolrebalancer **genesis** params for the selected scenario (`pool_delegator_address` is **not** in genesis; it is set after start)
- After validators are up: deploys or reuses a **CommunityPool** contract, sets `automationCaller` to the poolrebalancer module EVM address, and passes **governance** to set `poolrebalancer.params.pool_delegator_address` to the pool account
- Seeds scenario-specific **contract** deposit and staking imbalance state
- Streams validator logs and polls pending queues for manual verification

This runner is intended for contributor workflows and debugging. It is not a strict CI pass/fail harness.

## Quick start

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --help
```

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3 --profile medium
```

**Read-only watch** (chain already running from another shell):

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch
```

**Undelegation → ledger credit path** (CommunityPool `stakeablePrincipalLedger`, maturity hints; pair with a `credit_focus` run):

```bash
SCENARIO=credit_focus bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch credit
```

## Supported scenarios

- `happy_path`: baseline rebalance scheduling from a skewed delegation
- `caps`: constrained op/move settings to observe paced scheduling
- `threshold_boundary`: small drift with high threshold (often little or no scheduling)
- `fallback`: constrained redelegation conditions to observe undelegation fallback
- `expansion`: five validators, pool initially seeded on three, to observe target-set expansion (`--nodes 5`; scenario defaults apply if count unset)
- `credit_focus`: short unbonding, tight caps, and elevated CommunityPool `minStakeAmount` so mature-undelegation credits stay visible; use **`watch credit`** in a second shell

**Aliases** (normalized inside the script): `baseline_3val`, `max_target_gt_bonded_3val`, `fallback_path_3val`, `target_set_expansion_5val` → see `--help` / `apply_scenario_defaults` in the script.

## Commands

| Command | Meaning |
|--------|---------|
| *(default)* | Full setup + seed + observation loop |
| `watch` | Params, pending queues, and pool reads |
| `watch credit` | Ledger-focused view for credit / maturity (requires `cast`; `evmd query` + Tendermint time) |
| `help` / `--help` | Usage |

## Parameter precedence

1. Explicit environment variables (when the script tracks them for scenario merging)
2. Scenario defaults (only for knobs **not** treated as user-set; the script uses `USER_SET_*` flags internally)
3. Script baseline defaults

Example:

```bash
POOLREBALANCER_MAX_TARGET_VALIDATORS=5 \
POOLREBALANCER_MAX_OPS_PER_BLOCK=100 \
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario expansion --nodes 5
```

Exact variable names are listed in `bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --help`.

## Operational notes

- Use **Ctrl+C** to stop. The script traps interrupts and cleans up processes it started (when it started the chain).
- **`NODE_RPC`**: Comet/Tendermint RPC (default `tcp://127.0.0.1:26657`). Watch and observe loops derive the status URL from this; keep it aligned with the chain you are inspecting.
- **`EVM_RPC`**: Used for `cast` against CommunityPool. For non-local RPC hosts, set **`EVM_RPC`** (and optionally `NODE_RPC`) consistently with your devnet.
- **Startup timing**: On a typical local devnet, `pool_delegator_address` often appears around **block heights ~30–32** after chain start (deploy + gov vote + propagation vary). If you open `watch` / `watch credit` early, the script prints a short hint when the param is not set yet.
- **Manual caveats**: Very low CommunityPool `minStakeAmount` can let `stake()` consume `stakeablePrincipalLedger` in the same block as a credit—**not** the same as a failed credit path. Undelegation maturity is **time-based** (header time vs completion), not a fixed block count. `credit_focus` configures `minStake` explicitly to make credits easier to read.
- **`CREDIT_WATCH_USE_ENV_POOL_EVM`**: When `true`, `watch credit` keeps `POOL_EVM_ADDR` from the environment instead of resolving from on-chain `pool_delegator_address`.
- If behavior is unexpected, inspect:
  - `evmd query poolrebalancer params ...`
  - `evmd query poolrebalancer pending-redelegations ...`
  - `evmd query poolrebalancer pending-undelegations ...`

## Event signals to watch

The rebalancer emits these event types during EndBlock processing:

- `rebalance_summary`: successful operations were scheduled in this block.
- `redelegation_started`: a redelegation was initiated and tracked.
- `undelegation_started`: an undelegation fallback operation was initiated and tracked.
- `redelegation_failed`: a candidate redelegation failed and was skipped for this pass.
- `undelegation_failed`: undelegation fallback failed and the fallback loop stopped for this pass.
- `redelegations_completed`: matured pending redelegation tracking entries were cleaned.
- `undelegations_completed`: matured pending undelegation tracking entries were cleaned.

For failure events, the `reason` attribute contains the underlying error string.

## EndBlock failure policy

- Cleanup phases (`CompletePendingRedelegations`, `CompletePendingUndelegations`) are strict; failures return an error.
- `ProcessRebalance` is best-effort; failures are logged and retried on the next block.
