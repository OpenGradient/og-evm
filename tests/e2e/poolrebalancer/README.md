# Poolrebalancer E2E Scripts

Manual E2E scripts for `x/poolrebalancer` and CommunityPool contract flows.

## Scripts

- `tests/e2e/poolrebalancer/rebalance_scenario_runner.sh`: boots local devnet, deploys/wires CommunityPool, seeds rebalance scenarios, and monitors pending redelegations.
- `tests/e2e/poolrebalancer/user_flow_multikey.sh`: CommunityPool multi-account journey (approve, deposit, withdraw, claimWithdraw, optional claimRewards).
- `tests/e2e/poolrebalancer/community_pool_edge_cases.sh`: phase-driven pass/fail checks for ACL, reconcile, withdraw sizing, liquidity/maturity, dust, and rewards.
- `tests/e2e/poolrebalancer/lib/pool_e2e_common.sh`: shared helpers (RPC readiness, cast send wrappers, uint parsing, snapshots, account key lookup).

## Quick Start

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --help
```

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3 --profile medium
```

Print the full command reference at any time:

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh help
```

Watch mode for an already-running chain:

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch
```

Run user flow on an already-running and wired chain:

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh user_flow_multikey
```

Run edge-case phases (default phase is `auth` if none provided):

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh community_pool_edge_cases
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh community_pool_edge_cases all
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh community_pool_edge_cases auth,drift,withdraw_sizing
```

## Supported Scenarios

- `happy_path`: baseline scheduling from skewed delegation.
- `caps`: verifies `max_ops_per_block` and `max_move_per_op`.
- `threshold_boundary`: verifies high threshold suppresses small drift scheduling.
- `expansion`: with five validators, verifies destination expansion beyond initially delegated validators.

Aliases normalized by the runner:

- `baseline_3val` -> `happy_path`
- `max_target_gt_bonded_3val` -> `happy_path` with higher target count
- `target_set_expansion_5val` -> `expansion`

## Commands

- default: full setup + seed + observation loop
- `watch`: params, pending redelegations, and pool reads
- `user_flow_multikey`: run CommunityPool multi-account flow on an already running/wired chain
- `community_pool_edge_cases [phase-spec] [options]`: run edge-case assertions on an already running/wired chain (`phase-spec` is one token such as `all` or `auth,drift,withdraw_sizing`)
- `help` / `--help`

## Operational Notes

- `NODE_RPC` controls CometBFT RPC (default `tcp://127.0.0.1:26657`).
- `EVM_RPC` controls `cast` RPC (default `http://127.0.0.1:8545`).
- Full run mode deploys (or reuses) CommunityPool, sets `automationCaller`, then sets `poolrebalancer.params.pool_delegator_address` through governance.
- `user_flow_multikey` and `community_pool_edge_cases` do not start validators or run governance; they wait for an already-wired chain unless `POOL_CONTRACT_ADDR` is provided.
- Scenario defaults are applied only for knobs not explicitly set in environment variables.
- `community_pool_edge_cases` default phase behavior:
    - no positional arg + no `COMMUNITY_POOL_EDGE_PHASES`: runs `auth`
    - positional phase arg: overrides `COMMUNITY_POOL_EDGE_PHASES`
    - `all`: expands to `auth,drift,withdraw_sizing,liquidity,dust,rewards`
- If behavior is unexpected, inspect:
    - `evmd query poolrebalancer params ...`
    - `evmd query poolrebalancer pending-redelegations ...`

## Useful CLI Options

Common runner options:

- `--scenario`, `--nodes`, `--profile`
- `--stress-profile` (`100users`/`stress100`) for `user_flow_multikey`
- `--user-count`, `--withdraw-users`
- `--flow-mode` (`serial`/`parallel`)
- `--deposit-concurrency`, `--withdraw-concurrency`, `--claim-concurrency`, `--claim-rewards-concurrency`
- `--batch-delay-ms`

## Event Signals

Common rebalance signals emitted in EndBlock:

- `rebalance_summary`
- `redelegation_started`
- `redelegation_failed`
- `redelegations_completed`
