# Poolrebalancer E2E Scenario Runner

This document describes how to run manual E2E observation scenarios for `x/poolrebalancer`.

## Script

- `tests/e2e/poolrebalancer/rebalance_scenario_runner.sh`

## Purpose

- Bootstraps a multi-validator test chain via `multi_node_startup.sh`
- Patches staking and poolrebalancer genesis params for the selected scenario
- Seeds scenario-specific delegation/redelegation state
- Streams validator logs and pending queue state for manual verification

This runner is intended for contributor workflows and debugging. It is not a strict CI pass/fail harness.

## Quick Start

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --help
```

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3
```

```bash
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch
```

## Supported Scenarios

- `happy_path`: baseline rebalance scheduling from a skewed delegation
- `caps`: constrained op/move settings to observe paced scheduling
- `threshold_boundary`: small drift with high threshold (often little/no scheduling)
- `fallback`: constrained redelegation conditions to observe undelegation fallback behavior
- `expansion`: 5-validator setup seeded on 3 validators to observe target-set expansion

## Parameter Precedence

When running a scenario, parameter resolution is:

1. Explicit environment variables (highest priority)
2. Scenario defaults (for knobs not explicitly set)
3. Script baseline defaults

Example:

```bash
POOLREBALANCER_MAX_TARGET_VALIDATORS=5 \
POOLREBALANCER_MAX_OPS_PER_BLOCK=100 \
bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario expansion
```

## Operational Notes

- Use `Ctrl+C` to stop. The script traps interrupts and cleans up processes it started.
- If observed behavior differs from expectation, inspect:
  - `evmd query poolrebalancer params ...`
  - `evmd query poolrebalancer pending-redelegations ...`
  - `evmd query poolrebalancer pending-undelegations ...`

## Event Signals to Watch

The rebalancer emits these event types during EndBlock processing:

- `rebalance_summary`: successful operations were scheduled in this block.
- `redelegation_started`: a redelegation was initiated and tracked.
- `undelegation_started`: an undelegation fallback operation was initiated and tracked.
- `redelegation_failed`: a candidate redelegation failed and was skipped for this pass.
- `undelegation_failed`: undelegation fallback failed and the fallback loop stopped for this pass.
- `redelegations_completed`: matured pending redelegation tracking entries were cleaned.
- `undelegations_completed`: matured pending undelegation tracking entries were cleaned.

For failure events, the `reason` attribute contains the underlying error string.

## EndBlock Failure Policy

- Cleanup phases (`CompletePendingRedelegations`, `CompletePendingUndelegations`) are strict; failures return an error.
- `ProcessRebalance` is best-effort; failures are logged and retried on the next block.
