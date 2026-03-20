#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# rebalance_scenario_runner.sh
#
# Purpose:
#   Manual E2E scenario runner for x/poolrebalancer on a multi-validator test chain.
#
# Scope:
#   - Local engineer test workflow for exercising rebalance behavior.
#   - Scenario-driven integration-style validation, not a generic chain utility.
#   - Not intended as a deterministic CI harness.
#
# What this script helps observe:
#   - Pool delegator params are wired correctly in genesis.
#   - Rebalance scheduling is created as pending operations in module state.
#   - Per-block safety caps are respected:
#       * max_ops_per_block
#       * max_move_per_op
#   - Scenario-specific behavior:
#       * normal rebalance (happy_path)
#       * cap pressure behavior (caps)
#       * threshold no-op behavior (threshold_boundary)
#       * undelegation fallback behavior (fallback)
#       * dynamic target-set expansion (expansion)
#
# How to read output:
#   - "phase=..." lines show the high-level state machine in this script.
#   - "pending_red" is pending redelegations in x/poolrebalancer.
#   - "pending_und" is pending undelegations in x/poolrebalancer.
#   - Log lines are informational and meant for manual inspection.
#
# Test setup patched by this script:
#   - staking.params:
#       * unbonding_time (default: 30s)
#       * max_entries (default: 100, scenario may override)
#   - poolrebalancer.params:
#       * pool_delegator_address (dev0)
#       * max_target_validators
#       * rebalance_threshold_bp
#       * max_ops_per_block
#       * max_move_per_op
#       * use_undelegate_fallback
#
# Prerequisites:
#   - jq, curl, evmd installed and on PATH.
#   - Validator mnemonics are auto-generated for missing VAL{N}_MNEMONIC vars.
#
# Quick start:
#   bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh
#
# Watch-only mode:
#   bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch
# ============================================================================

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-"tcp://127.0.0.1:26657"}"
CHAIN_ID="${CHAIN_ID:-10740}"
KEYRING="${KEYRING:-test}"
HOME0="$BASEDIR/val0"

# -----------------------------------------------------------------------------
# Runtime knobs (env vars take precedence)
# -----------------------------------------------------------------------------
# Track which knobs were explicitly provided via environment so scenario defaults
# can apply only when not set by the user.
USER_SET_MAX_TARGET_VALIDATORS=false
[[ -n "${POOLREBALANCER_MAX_TARGET_VALIDATORS+x}" ]] && USER_SET_MAX_TARGET_VALIDATORS=true
USER_SET_THRESHOLD_BP=false
[[ -n "${POOLREBALANCER_THRESHOLD_BP+x}" ]] && USER_SET_THRESHOLD_BP=true
USER_SET_MAX_OPS_PER_BLOCK=false
[[ -n "${POOLREBALANCER_MAX_OPS_PER_BLOCK+x}" ]] && USER_SET_MAX_OPS_PER_BLOCK=true
USER_SET_MAX_MOVE_PER_OP=false
[[ -n "${POOLREBALANCER_MAX_MOVE_PER_OP+x}" ]] && USER_SET_MAX_MOVE_PER_OP=true
USER_SET_USE_UNDELEGATE_FALLBACK=false
[[ -n "${POOLREBALANCER_USE_UNDELEGATE_FALLBACK+x}" ]] && USER_SET_USE_UNDELEGATE_FALLBACK=true
USER_SET_STAKING_MAX_ENTRIES=false
[[ -n "${STAKING_MAX_ENTRIES+x}" ]] && USER_SET_STAKING_MAX_ENTRIES=true
USER_SET_IMBALANCE_MINOR_DELEGATION=false
[[ -n "${IMBALANCE_MINOR_DELEGATION+x}" ]] && USER_SET_IMBALANCE_MINOR_DELEGATION=true

SCENARIO="${SCENARIO:-happy_path}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-}"
POOLREBALANCER_MAX_TARGET_VALIDATORS="${POOLREBALANCER_MAX_TARGET_VALIDATORS:-3}"
# Demo profile controls default speed so users can observe behavior.
# slow   = very gradual progress (good for watching)
# medium = balanced default for demo
# fast   = converges quickly
DEMO_PROFILE="${DEMO_PROFILE:-medium}"
POOLREBALANCER_THRESHOLD_BP="${POOLREBALANCER_THRESHOLD_BP:-0}"
POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-2}"
POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-100000000000000000000}" # 1e20
POOLREBALANCER_USE_UNDELEGATE_FALLBACK="${POOLREBALANCER_USE_UNDELEGATE_FALLBACK:-false}"

# Tune staking params so maturity/fallback behavior is visible in test runs.
STAKING_UNBONDING_TIME="${STAKING_UNBONDING_TIME:-30s}"
STAKING_MAX_ENTRIES="${STAKING_MAX_ENTRIES:-100}"

TX_FEES="${TX_FEES:-200000000000000ogwei}" # denom will be rewritten after chain start

# Seed amounts used to create a clear imbalance (safe with default dev funding).
IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION:-200000000000000000000000ogwei}" # denom rewritten after chain start
IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION:-100ogwei}"

POLL_SAMPLES="${POLL_SAMPLES:-25}"
POLL_SLEEP_SECS="${POLL_SLEEP_SECS:-2}"
# Always-on observability/runtime behavior for CLI usage.
STREAM_VALIDATOR_LOGS="true"
KEEP_RUNNING="true"
WATCH_COMPACT="${WATCH_COMPACT:-false}"

LOG_STREAM_PIDS=()
CURRENT_PHASE="init"
SETUP_STARTED="false"
EXPANSION_MISSING_DSTS=()
EXPANSION_OBSERVED_DSTS_TEXT=""
EXPANSION_INITIAL_DELEGATED=()
FALLBACK_SEEN_REDELEGATION="false"
FALLBACK_UND_DEADLINE_SAMPLES="${FALLBACK_UND_DEADLINE_SAMPLES:-15}"

on_interrupt() {
  echo
  echo "==> Interrupt received, stopping test setup..."
  # Stop child processes spawned by this script first.
  pkill -TERM -P "$$" >/dev/null 2>&1 || true
  cleanup_on_exit
  exit 130
}

cleanup_on_exit() {
  cleanup_log_streams
  if [[ "$SETUP_STARTED" == "true" ]]; then
    stop_nodes
  fi
}

usage() {
  cat <<EOF
Usage:
  $0 [options]
  $0 watch [options]
  $0 help

Runs an E2E test scenario for x/poolrebalancer:
- Bootstraps an isolated multi-validator test chain using multi_node_startup.sh
- Patches genesis staking + poolrebalancer params
- Starts val0..valN and imports dev0 as pool delegator
- Seeds scenario-specific delegation/redelegation state
- Polls pending queues so engineers can inspect behavior interactively

What gets patched before node start:
  staking.params:
    - unbonding_time=$STAKING_UNBONDING_TIME
    - max_entries=$STAKING_MAX_ENTRIES
  poolrebalancer.params:
    - pool_delegator_address=<dev0>
    - max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS
    - rebalance_threshold_bp=$POOLREBALANCER_THRESHOLD_BP
    - max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK
    - max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP
    - use_undelegate_fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK

Parameter precedence:
  1) Explicit environment variables (highest priority)
  2) Scenario defaults (applied only for knobs not explicitly set)
  3) Script baseline defaults

Commands:
  run (default)                     Full test setup + scenario execution
  watch                             Live monitor for an already running test chain
  help                              Show this help

CLI options:
  -n, --nodes <count>               Number of validators/nodes to run
  -s, --scenario <name>             Scenario name (same as SCENARIO env var)
  -p, --profile <name>              Demo profile: slow|medium|fast
  -h, --help                        Show this help

Scenarios:
  happy_path
    Goal: baseline rebalance scheduling from a heavily skewed delegation.
    Setup: delegate mostly to validator[0], tiny amounts to validator[1]/[2].
    Params: uses baseline defaults (unless overridden by environment).
    Watch for: pending redelegations to underweight validators.

  caps
    Goal: verify scheduling respects max_ops_per_block and max_move_per_op.
    Setup: same skew as happy_path, but with tight scheduling caps.
    Params: default poolrebalancer max_ops_per_block=1, max_move_per_op=1e18.
    Watch for: capped move sizes and slower progression.

  threshold_boundary
    Goal: verify tiny drift is ignored when threshold is high enough.
    Setup: create only a small imbalance and set threshold boundary params.
    Params: default poolrebalancer rebalance_threshold_bp=5000.
    Watch for: little or no scheduling when drift stays below threshold.

  fallback
    Goal: verify undelegation fallback is used when redelegation path is constrained.
    Setup: source-heavy skew + fallback enabled + low staking max_entries + transitive blocker.
    Params: default poolrebalancer use_undelegate_fallback=true; default staking max_entries=1.
    Watch for: pending undelegations appearing alongside or after redelegations.

  expansion
    Goal: verify target set can expand to additional bonded validators.
    Setup: 5 validators total, seed initial delegation to only 3 validators.
    Params: default poolrebalancer max_target_validators=5, max_ops_per_block=1, max_move_per_op=1e19.
    Watch for: redelegations moving stake toward validators outside the initial delegated set.

Profiles:
  slow                              max_ops_per_block=1, capped move per op
  medium                            default balancing profile
  fast                              more ops per block, no move cap

Environment variables:
  BASEDIR                           Test chain base dir (default: $HOME/.og-evm-devnet)
  NODE_RPC                          RPC endpoint (default: tcp://127.0.0.1:26657)
  CHAIN_ID                          Chain ID (default: 10740)
  TX_FEES                           Fees for txs (default: $TX_FEES)

  VAL0_MNEMONIC ... VALN_MNEMONIC  Optional explicit mnemonics. Any missing values are auto-generated.

  POOLREBALANCER_MAX_TARGET_VALIDATORS
  SCENARIO                          happy_path|caps|threshold_boundary|fallback|expansion
  VALIDATOR_COUNT                   Number of validators to start (default: 3; scenario may override)
  DEMO_PROFILE                      slow|medium|fast tuning for rebalance visibility (default: medium)
  POOLREBALANCER_THRESHOLD_BP
  POOLREBALANCER_MAX_OPS_PER_BLOCK
  POOLREBALANCER_MAX_MOVE_PER_OP
  POOLREBALANCER_USE_UNDELEGATE_FALLBACK

  STAKING_UNBONDING_TIME            Reduce so pending queues mature quickly (default: 30s)
  STAKING_MAX_ENTRIES               Raise/lower redelegation/undelegation entry pressure (default: 100)

  IMBALANCE_MAIN_DELEGATION         Large delegation to validator[0]
  IMBALANCE_MINOR_DELEGATION        Small delegations to validator[1], validator[2]
  WATCH_COMPACT                     Compact watch output (single-line summaries, default: false)
  FALLBACK_UND_DEADLINE_SAMPLES     Fallback deadline (samples) to observe pending undelegations (default: 15)

Note:
  Any variable set explicitly in the environment overrides scenario defaults.

Examples:
  # Standard rebalance flow
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3 --profile medium

  # Cap-focused behavior
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario caps --nodes 3 --profile slow

  # Threshold gating (expect no scheduling for small drift)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario threshold_boundary --nodes 3

  # Fallback-focused profile
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario fallback --nodes 3 --profile slow

  # 5-validator expansion
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario expansion --nodes 5

  # Watch only
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch

EOF
}

parse_cli_args() {
  local subcommand=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      watch)
        subcommand="watch"
        shift
        ;;
      help)
        subcommand="help"
        shift
        ;;
      -n|--nodes)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        VALIDATOR_COUNT="$2"
        shift 2
        ;;
      -s|--scenario)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        SCENARIO="$2"
        shift 2
        ;;
      -p|--profile)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        DEMO_PROFILE="$2"
        shift 2
        ;;
      -h|--help)
        subcommand="help"
        shift
        ;;
      --)
        shift
        break
        ;;
      run)
        # Explicit no-op command for readability.
        shift
        ;;
      *)
        echo "unknown argument: $1" >&2
        return 1
        ;;
    esac
  done
  PARSED_SUBCOMMAND="$subcommand"
  return 0
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}

stop_nodes() {
  # Aggressive cleanup: multi_node_startup.sh launches `evmd start` processes directly.
  pkill -f "evmd start" >/dev/null 2>&1 || true
  pkill -f "multi_node_startup.sh" >/dev/null 2>&1 || true
  # Give the OS a moment to release RPC/P2P ports.
  sleep 1
}

cleanup_log_streams() {
  if (( ${#LOG_STREAM_PIDS[@]} == 0 )); then
    return 0
  fi
  for pid in "${LOG_STREAM_PIDS[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  LOG_STREAM_PIDS=()
}

start_validator_log_streams() {
  mkdir -p "$BASEDIR/logs"
  for v in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    local f="$BASEDIR/logs/val${v}.log"
    touch "$f"
    tail -n 0 -F "$f" | sed -u "s/^/[val${v}] /" &
    LOG_STREAM_PIDS+=("$!")
  done
}

wait_for_height() {
  local timeout_secs="${1:-30}"
  local start
  start="$(date +%s)"
  while true; do
    local h
    h="$(curl -sS --max-time 1 http://127.0.0.1:26657/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height' 2>/dev/null || echo 0)"
    if [[ "$h" != "0" ]]; then
      echo "$h"
      return 0
    fi
    if (( $(date +%s) - start > timeout_secs )); then
      echo "timed out waiting for height > 0" >&2
      return 1
    fi
    sleep 1
  done
}

dev0_address_from_file() {
  awk '/^dev0:/{f=1} f && $1=="address:"{print $2; exit}' "$BASEDIR/dev_accounts.txt"
}

dev0_mnemonic_from_file() {
  awk '/^dev0:/{f=1} f && $1=="mnemonic:"{for(i=2;i<=NF;i++){printf (i==2?"":" ") $i} print ""; exit}' "$BASEDIR/dev_accounts.txt"
}

auto_generate_validator_mnemonic() {
  local idx="$1"
  local tmp_home
  local key_name
  local out
  local mnemonic

  tmp_home="$(mktemp -d "${TMPDIR:-/tmp}/poolrebalancer-mnemonic-${idx}-XXXXXX")"
  key_name="autoval${idx}"
  out="$(evmd keys add "$key_name" --keyring-backend test --algo eth_secp256k1 --home "$tmp_home" 2>&1)"
  mnemonic="$(echo "$out" | awk 'NF{line=$0} END{print line}')"
  rm -rf "$tmp_home"

  if [[ -z "$mnemonic" ]]; then
    echo "failed to auto-generate mnemonic for validator $idx" >&2
    return 1
  fi
  echo "$mnemonic"
}

resolve_mnemonics() {
  local missing=()
  local need="$VALIDATOR_COUNT"

  for i in $(seq 0 $((need - 1))); do
    local name="VAL${i}_MNEMONIC"
    local current="${!name:-}"
    if [[ -z "$current" ]]; then
      current="$(auto_generate_validator_mnemonic "$i" || true)"
      if [[ -n "$current" ]]; then
        export "$name=$current"
      fi
    fi
    if [[ -z "${!name:-}" ]]; then
      missing+=("$name")
    fi
  done

  if (( ${#missing[@]} > 0 )); then
    echo "missing required mnemonics: ${missing[*]}" >&2
    echo "set them in env or ensure \$BASEDIR/dev_accounts.txt contains dev0..dev$((need - 1)) entries" >&2
    exit 1
  fi
}

patch_genesis_poolrebalancer_params() {
  local del_addr="$1"
  local gen0="$BASEDIR/val0/config/genesis.json"
  local tmp="$BASEDIR/val0/config/genesis.tmp.json"

  jq --arg del "$del_addr" \
     --argjson maxTargets "$POOLREBALANCER_MAX_TARGET_VALIDATORS" \
     --argjson thr "$POOLREBALANCER_THRESHOLD_BP" \
     --argjson maxOps "$POOLREBALANCER_MAX_OPS_PER_BLOCK" \
     --arg maxMove "$POOLREBALANCER_MAX_MOVE_PER_OP" \
     --argjson useUndel "$POOLREBALANCER_USE_UNDELEGATE_FALLBACK" \
     ' .app_state.poolrebalancer.params.pool_delegator_address = $del
       | .app_state.poolrebalancer.params.max_target_validators = $maxTargets
       | .app_state.poolrebalancer.params.rebalance_threshold_bp = $thr
       | .app_state.poolrebalancer.params.max_ops_per_block = $maxOps
       | .app_state.poolrebalancer.params.max_move_per_op = $maxMove
       | .app_state.poolrebalancer.params.use_undelegate_fallback = $useUndel
     ' "$gen0" > "$tmp"

  mv "$tmp" "$gen0"
  for v in $(seq 1 $((VALIDATOR_COUNT - 1))); do
    cp "$gen0" "$BASEDIR/val${v}/config/genesis.json"
  done

  evmd genesis validate-genesis --home "$BASEDIR/val0" >/dev/null
}

patch_genesis_staking_params() {
  local gen0="$BASEDIR/val0/config/genesis.json"
  local tmp="$BASEDIR/val0/config/genesis.tmp.json"

  jq --arg unbond "$STAKING_UNBONDING_TIME" \
     --argjson maxEntries "$STAKING_MAX_ENTRIES" \
     ' .app_state.staking.params.unbonding_time = $unbond
       | .app_state.staking.params.max_entries = $maxEntries
     ' "$gen0" > "$tmp"

  mv "$tmp" "$gen0"
}

import_dev0_key() {
  local mnemonic="$1"
  (evmd keys delete dev0 -y --keyring-backend "$KEYRING" --home "$HOME0" >/dev/null 2>&1) || true
  echo "$mnemonic" | evmd keys add dev0 --recover --keyring-backend "$KEYRING" --home "$HOME0" >/dev/null
}

wait_tx_included() {
  local txhash="$1"
  # First try CometBFT /tx; it becomes available as soon as tx is committed.
  local rpc_http="http://127.0.0.1:26657"
  for _ in $(seq 1 40); do
    local resp
    resp="$(curl -sS --max-time 1 "${rpc_http}/tx?hash=0x${txhash}" 2>/dev/null || true)"
    # Not-found still returns JSON. Treat as committed only when .result.tx_result exists.
    if echo "$resp" | jq -e '.result.tx_result' >/dev/null 2>&1; then
      # Committed does not mean successful; require code=0.
      local code
      code="$(echo "$resp" | jq -r '.result.tx_result.code' 2>/dev/null || echo 1)"
      if [[ "$code" == "0" ]]; then
        return 0
      fi
      echo "tx committed but failed (code=$code): $txhash" >&2
      echo "$resp" | jq -r '.result.tx_result.log' >&2 || true
      return 1
    fi
    # Fallback query path (depends on tx indexing config).
    if evmd query tx "$txhash" --node "$NODE_RPC" -o json >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "tx not found after waiting: $txhash" >&2
  echo "check logs: $BASEDIR/logs/val0.log" >&2
  return 1
}

delegate_with_wait() {
  local valoper="$1"
  local amount="$2"
  # Some CLI builds return txhash even on CheckTx failure; always inspect `.code`.
  for attempt in 1 2 3; do
    local out
    out="$(evmd tx staking delegate "$valoper" "$amount" \
      --from dev0 \
      --keyring-backend "$KEYRING" \
      --home "$HOME0" \
      --chain-id "$CHAIN_ID" \
      --node "$NODE_RPC" \
      --gas auto --gas-adjustment 1.3 \
      --fees "$TX_FEES" \
      -b sync -y -o json)"

    local code
    code="$(echo "$out" | jq -r '.code // 0')"
    local txhash
    txhash="$(echo "$out" | jq -r '.txhash')"

    if [[ "$code" != "0" ]]; then
      local log
      log="$(echo "$out" | jq -r '.raw_log // .log // empty')"
      echo "delegate failed (attempt=$attempt code=$code): $log" >&2

      # Common transient: sequence mismatch while previous tx is still propagating.
      if echo "$log" | grep -qi "account sequence mismatch"; then
        sleep 2
        continue
      fi
      return 1
    fi

    echo "delegate $amount -> $valoper txhash=$txhash"
    wait_tx_included "$txhash" >/dev/null
    return 0
  done

  echo "delegate failed after retries: $amount -> $valoper" >&2
  return 1
}

redelegate_with_wait() {
  local src_valoper="$1"
  local dst_valoper="$2"
  local amount="$3"
  # Some CLI builds return txhash even on CheckTx failure, so always inspect `.code`.
  for attempt in 1 2 3; do
    local out
    out="$(evmd tx staking redelegate "$src_valoper" "$dst_valoper" "$amount" \
      --from dev0 \
      --keyring-backend "$KEYRING" \
      --home "$HOME0" \
      --chain-id "$CHAIN_ID" \
      --node "$NODE_RPC" \
      --gas auto --gas-adjustment 1.3 \
      --fees "$TX_FEES" \
      -b sync -y -o json 2>&1 || true)"

    local code
    code="$(echo "$out" | jq -r '.code // 0' 2>/dev/null || echo 1)"
    local txhash
    txhash="$(echo "$out" | jq -r '.txhash // empty' 2>/dev/null || true)"

    if [[ "$code" != "0" ]]; then
      local log
      log="$(echo "$out" | jq -r '.raw_log // .log // empty' 2>/dev/null || true)"
      if [[ -z "$log" ]]; then
        log="$out"
      fi
      if echo "$log" | rg -qi "redelegation to this validator already in progress"; then
        echo "redelegate precondition already satisfied: incoming redelegation exists for $dst_valoper"
        return 0
      fi
      echo "redelegate failed (attempt=$attempt code=$code): $log" >&2
      if echo "$log" | rg -qi "account sequence mismatch"; then
        sleep 2
        continue
      fi
      return 1
    fi

    echo "redelegate $amount $src_valoper -> $dst_valoper txhash=$txhash"
    wait_tx_included "$txhash" >/dev/null
    return 0
  done

  echo "redelegate failed after retries: $amount $src_valoper -> $dst_valoper" >&2
  return 1
}

check_pending_invariants() {
  local json="$1"
  local cap="$2"
  local max_ops="$3"

  # Important nuance:
  # pending-redelegations query returns primary records that can merge multiple ops
  # sharing (delegator, denom, dst, completionTime). With max_ops_per_block > 1,
  # a merged record amount can exceed max_move_per_op even if each individual op respected the cap.
  # So strict cap checking is only sound when max_ops_per_block == 1.
  if [[ "$cap" != "0" && "$max_ops" == "1" ]]; then
    local badAmt
    badAmt="$(echo "$json" | jq -r --argjson cap "$cap" '[.redelegations[] | (.amount.amount|tonumber) > $cap] | any')"
    if [[ "$badAmt" != "false" ]]; then
      echo "warning: found pending amount > max_move_per_op" >&2
      return 1
    fi
  elif [[ "$cap" != "0" && "$max_ops" != "1" ]]; then
    echo "note: skipping strict max_move_per_op check on merged primary entries (max_ops_per_block=$max_ops)"
  fi

  # Transitive safety: a source validator must not also be an in-flight destination.
  local badTrans
  badTrans="$(echo "$json" | jq -r '([.redelegations[].src_validator_address] | unique) as $srcs | ([.redelegations[].dst_validator_address] | unique) as $dsts | ([ $srcs[] | . as $s | (($dsts | index($s)) != null) ] | any)')"
  if [[ "$badTrans" != "false" ]]; then
    echo "warning: transitive safety violated (src appears in dst set)" >&2
    return 1
  fi
}

watch_rebalance_status() {
  # Read-only watch mode for an already running test chain.
  # Use this to inspect params/pending queues without re-running setup.
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local interval="${POLL_SLEEP_SECS:-2}"

  while true; do
    local h params del pr pu
    h="$(curl -sS http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height // "n/a"')"
    params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || echo '{}')"
    del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
    pr="$(evmd query poolrebalancer pending-redelegations --node "$node" -o json 2>/dev/null | jq -r '.redelegations | length // 0')"
    pu="$(evmd query poolrebalancer pending-undelegations --node "$node" -o json 2>/dev/null | jq -r '.undelegations | length // 0')"

    if [[ "$WATCH_COMPACT" == "true" ]]; then
      echo "watch phase=$CURRENT_PHASE height=$h pending_red=$pr pending_und=$pu scenario=$SCENARIO"
    else
      echo "----- rebalance watch -----"
      echo "phase=$CURRENT_PHASE height=$h pending_red=$pr pending_und=$pu"
      echo "$params" | jq -r '.params | {pool_delegator_address,max_target_validators,rebalance_threshold_bp,max_ops_per_block,max_move_per_op,use_undelegate_fallback}'

      if [[ -n "$del" ]]; then
        evmd query staking delegations "$del" --node "$node" -o json 2>/dev/null | \
          jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
      else
        echo "pool delegator not configured"
      fi
      echo
    fi
    sleep "$interval"
  done
}

setup_localnet() {
  CURRENT_PHASE="setup_localnet"
  SETUP_STARTED="true"
  echo "==> Stopping any existing test chain"
  stop_nodes

  echo "==> Generating test genesis ($VALIDATOR_COUNT validators) at $BASEDIR"
  # multi_node_startup.sh is verbose during init; silence setup noise here.
  (cd "$ROOT_DIR" && VALIDATOR_COUNT="$VALIDATOR_COUNT" GENERATE_GENESIS=true ./multi_node_startup.sh -y >/dev/null 2>&1)

  POOL_DEL_ADDR="$(dev0_address_from_file)"
  POOL_DEL_MNEMONIC="$(dev0_mnemonic_from_file)"
}

configure_genesis_params() {
  CURRENT_PHASE="configure_genesis"
  echo "==> Pool delegator (dev0) = $POOL_DEL_ADDR"
  echo "==> SCENARIO=$SCENARIO VALIDATOR_COUNT=$VALIDATOR_COUNT DEMO_PROFILE=$DEMO_PROFILE threshold_bp=$POOLREBALANCER_THRESHOLD_BP max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK"
  echo "==> Patching genesis staking params (unbonding_time + max_entries)"
  patch_genesis_staking_params
  echo "==> Patching genesis poolrebalancer params"
  patch_genesis_poolrebalancer_params "$POOL_DEL_ADDR"
}

start_validators() {
  CURRENT_PHASE="start_validators"
  echo "==> Starting validators"
  mkdir -p "$BASEDIR/logs"
  for v in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    (cd "$ROOT_DIR" && VALIDATOR_COUNT="$VALIDATOR_COUNT" START_VALIDATOR=true NODE_NUMBER="$v" ./multi_node_startup.sh >"$BASEDIR/logs/val${v}.log" 2>&1 &)
  done
  if [[ "$STREAM_VALIDATOR_LOGS" == "true" ]]; then
    echo "==> Streaming validator logs (val0..val$((VALIDATOR_COUNT - 1)))"
    start_validator_log_streams
  fi
}

wait_chain_ready() {
  CURRENT_PHASE="wait_chain_ready"
  echo "==> Waiting for block production"
  local h
  h="$(wait_for_height 60)"
  echo "height=$h"

  # Resolve chain bond denom and rewrite amount knobs to match this network.
  BOND_DENOM="$(evmd query staking params --node "$NODE_RPC" -o json | jq -r '.params.bond_denom // .bond_denom')"
  if [[ -z "$BOND_DENOM" || "$BOND_DENOM" == "null" ]]; then
    echo "error: could not determine bond_denom from staking params" >&2
    exit 1
  fi
  echo "bond_denom=$BOND_DENOM"
  TX_FEES="${TX_FEES%ogwei}${BOND_DENOM}"
  IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION%ogwei}${BOND_DENOM}"
  IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION%ogwei}${BOND_DENOM}"
}

seed_initial_imbalance() {
  CURRENT_PHASE="seed_initial_imbalance"
  echo "==> Importing dev0 key into keyring"
  import_dev0_key "$POOL_DEL_MNEMONIC"

  echo "==> Creating delegation imbalance (scenario=$SCENARIO)"
  local vals v0 v1 v2
  vals="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '.validators[:3] | .[] | .operator_address')"
  v0="$(echo "$vals" | sed -n '1p')"
  v1="$(echo "$vals" | sed -n '2p')"
  v2="$(echo "$vals" | sed -n '3p')"

  case "$SCENARIO" in
    happy_path|caps|expansion)
      delegate_with_wait "$v0" "$IMBALANCE_MAIN_DELEGATION"
      delegate_with_wait "$v1" "$IMBALANCE_MINOR_DELEGATION"
      delegate_with_wait "$v2" "$IMBALANCE_MINOR_DELEGATION"
      if [[ "$SCENARIO" == "expansion" ]]; then
        EXPANSION_INITIAL_DELEGATED=("$v0" "$v1" "$v2")
      fi
      ;;
    threshold_boundary)
      # Keep drift tiny so threshold gating can suppress scheduling.
      delegate_with_wait "$v0" "$IMBALANCE_MINOR_DELEGATION"
      ;;
    fallback)
      # Keep source-heavy skew; fallback path is enabled by scenario defaults.
      delegate_with_wait "$v0" "$IMBALANCE_MAIN_DELEGATION"
      delegate_with_wait "$v1" "$IMBALANCE_MINOR_DELEGATION"
      delegate_with_wait "$v2" "$IMBALANCE_MINOR_DELEGATION"
      # Force fallback sooner:
      # create an in-flight incoming redelegation to v0, which blocks using v0
      # as a redelegation source via transitive safety
      # (HasImmatureRedelegationTo(src=v0)).
      # With v0 as the main overweight source, fallback undelegation appears quickly.
      redelegate_with_wait "$v1" "$v0" "$IMBALANCE_MINOR_DELEGATION"
      ;;
    *)
      echo "error: unsupported SCENARIO in seed_initial_imbalance: $SCENARIO" >&2
      exit 1
      ;;
  esac
}

run_sanity_checks() {
  CURRENT_PHASE="run_sanity_checks"
  echo "==> Sanity checks (params + delegations)"
  local onchain_del
  onchain_del="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address')"
  if [[ "$onchain_del" != "$POOL_DEL_ADDR" ]]; then
    echo "error: poolrebalancer params.pool_delegator_address mismatch" >&2
    echo "  expected: $POOL_DEL_ADDR" >&2
    echo "  got:      $onchain_del" >&2
    exit 1
  fi

  local del_count
  del_count="$(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json | jq -r '.delegation_responses | length')"
  echo "delegations_count=$del_count"

  local bonded_count
  bonded_count="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '[.validators[] | select(.status=="BOND_STATUS_BONDED")] | length')"
  echo "bonded_validators=$bonded_count"
  if (( bonded_count == 0 )); then
    echo "error: no bonded validators found; cannot rebalance" >&2
    exit 1
  fi

  if [[ "$SCENARIO" == "expansion" ]]; then
    if (( bonded_count < 5 )); then
      echo "error: expected at least 5 bonded validators for scenario=target_set_expansion_5val, got $bonded_count" >&2
      exit 1
    fi

    if (( ${#EXPANSION_INITIAL_DELEGATED[@]} != 3 )); then
      echo "error: expansion initial seeded validator set is not ready (expected 3 validators)" >&2
      exit 1
    fi

    local bonded_json
    bonded_json="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '[.validators[] | select(.status=="BOND_STATUS_BONDED") | .operator_address] | unique')"
    local seeded_json
    seeded_json="$(printf '%s\n' "${EXPANSION_INITIAL_DELEGATED[@]}" | jq -R . | jq -s 'unique')"
    EXPANSION_MISSING_DSTS=()
    while IFS= read -r val; do
      [[ -z "$val" ]] && continue
      EXPANSION_MISSING_DSTS+=("$val")
    done < <(jq -n -r --argjson bonded "$bonded_json" --argjson delegated "$seeded_json" '$bonded - $delegated | .[]')

    if (( ${#EXPANSION_MISSING_DSTS[@]} < 2 )); then
      echo "error: expansion expects at least 2 bonded validators outside initial pool delegation set, got ${#EXPANSION_MISSING_DSTS[@]}" >&2
      exit 1
    fi

    EXPANSION_OBSERVED_DSTS_TEXT=""
    echo "scenario_check: bonded_validators=$bonded_count initial_seeded=${#EXPANSION_INITIAL_DELEGATED[@]} missing_targets=${#EXPANSION_MISSING_DSTS[@]}"
  fi
}

update_expansion_observed_dsts() {
  local pending_json="$1"
  if [[ "$SCENARIO" != "expansion" ]]; then
    return 0
  fi

  local dst target
  while IFS= read -r dst; do
    [[ -z "$dst" ]] && continue
    for target in "${EXPANSION_MISSING_DSTS[@]}"; do
      if [[ "$dst" == "$target" ]]; then
        if ! printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | rg -F -x --quiet "$dst"; then
          if [[ -n "$EXPANSION_OBSERVED_DSTS_TEXT" ]]; then
            EXPANSION_OBSERVED_DSTS_TEXT="${EXPANSION_OBSERVED_DSTS_TEXT}"$'\n'"$dst"
          else
            EXPANSION_OBSERVED_DSTS_TEXT="$dst"
          fi
        fi
        break
      fi
    done
  done < <(echo "$pending_json" | jq -r '.redelegations[]?.dst_validator_address')
}

expansion_observed_count() {
  local count=0
  local target
  for target in "${EXPANSION_MISSING_DSTS[@]}"; do
    if printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | rg -F -x --quiet "$target"; then
      count=$((count + 1))
    fi
  done
  echo "$count"
}

observe_and_monitor() {
  CURRENT_PHASE="observe_and_monitor"
  echo "==> Observing pending operations (scenario=$SCENARIO)"
  # Poll loop used for observation:
  # - collect pending queue state
  # - wait until any pending operations appear
  # - validate generic invariants
  for i in $(seq 1 "$POLL_SAMPLES"); do
    local height pending pendingUndel
    height="$(curl -s http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height')"
    local j
    j="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json)"
    update_expansion_observed_dsts "$j"
    pending="$(echo "$j" | jq -r '.redelegations | length')"
    pendingUndel="$(evmd query poolrebalancer pending-undelegations --node "$NODE_RPC" -o json | jq -r '.undelegations | length')"
    if [[ "$WATCH_COMPACT" == "true" ]]; then
      echo "sample=$i phase=$CURRENT_PHASE height=$height pending_red=$pending pending_und=$pendingUndel scenario=$SCENARIO"
    else
      echo "sample=$i phase=$CURRENT_PHASE height=$height pending_red=$pending pending_und=$pendingUndel"
    fi
    if [[ "$SCENARIO" == "expansion" ]]; then
      local seen expected
      seen="$(expansion_observed_count)"
      expected="${#EXPANSION_MISSING_DSTS[@]}"
      echo "expansion_progress: observed_new_destinations=$seen/$expected"
    elif [[ "$SCENARIO" == "fallback" ]]; then
      if (( pending > 0 )); then
        FALLBACK_SEEN_REDELEGATION="true"
      fi
      echo "fallback_progress: seen_redelegation=$FALLBACK_SEEN_REDELEGATION undelegations=$pendingUndel deadline_sample=$FALLBACK_UND_DEADLINE_SAMPLES"
    fi

    if (( pending > 0 || pendingUndel > 0 )); then
      if (( pending > 0 )); then
        check_pending_invariants "$j" "$POOLREBALANCER_MAX_MOVE_PER_OP" "$POOLREBALANCER_MAX_OPS_PER_BLOCK"
      fi
      echo "info: pending operations observed; continuing monitor"
      if [[ "$KEEP_RUNNING" != "true" ]]; then
        exit 0
      fi
      CURRENT_PHASE="steady_monitor"
      echo "==> KEEP_RUNNING=true, continuing in monitor mode (Ctrl+C to stop)"
      while true; do
        local monitorHeight monitorRed monitorUnd
        monitorHeight="$(curl -sS http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height')"
        monitorRed="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json | jq -r '.redelegations | length')"
        monitorUnd="$(evmd query poolrebalancer pending-undelegations --node "$NODE_RPC" -o json | jq -r '.undelegations | length')"
        if [[ "$WATCH_COMPACT" == "true" ]]; then
          echo "monitor phase=$CURRENT_PHASE height=$monitorHeight pending_red=$monitorRed pending_und=$monitorUnd scenario=$SCENARIO"
        else
          echo "monitor phase=$CURRENT_PHASE height=$monitorHeight pending_red=$monitorRed pending_und=$monitorUnd"
        fi
        sleep "$POLL_SLEEP_SECS"
      done
    fi
    sleep "$POLL_SLEEP_SECS"
  done

  echo "info: no pending operations observed within polling window" >&2
  echo "note: this can be expected when drift is below threshold or the system is already balanced" >&2
  exit 0
}

apply_scenario_defaults() {
  # Scenario defaults encode engineer-friendly test behavior.
  # They are applied only when the corresponding env var was not explicitly set.
  case "$SCENARIO" in
    # Canonical scenarios
    happy_path)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      ;;
    caps)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=1000000000000000000; fi
      ;;
    threshold_boundary)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_THRESHOLD_BP" != "true" ]]; then POOLREBALANCER_THRESHOLD_BP=5000; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=2; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=100000000000000000000; fi
      ;;
    fallback)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_USE_UNDELEGATE_FALLBACK" != "true" ]]; then POOLREBALANCER_USE_UNDELEGATE_FALLBACK=true; fi
      # Small cap + single-op profile makes fallback behavior easy to observe.
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=1000000000000000000; fi
      # Tight staking entry limit blocks repeated redelegations quickly and
      # makes fallback undelegations appear sooner in local runs.
      if [[ "$USER_SET_STAKING_MAX_ENTRIES" != "true" ]]; then STAKING_MAX_ENTRIES=1; fi
      ;;
    expansion)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=5; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      # Make expansion visually clearer:
      # - start with a meaningful baseline on initially delegated validators
      # - move in smaller steps so newly introduced validators ramp up gradually
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=10000000000000000000; fi
      if [[ "$USER_SET_IMBALANCE_MINOR_DELEGATION" != "true" ]]; then IMBALANCE_MINOR_DELEGATION=1000000000000000000000ogwei; fi
      ;;
    # Backward-compatible aliases
    baseline_3val)
      SCENARIO="happy_path"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      ;;
    max_target_gt_bonded_3val)
      SCENARIO="happy_path"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      ;;
    fallback_path_3val)
      SCENARIO="fallback"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_USE_UNDELEGATE_FALLBACK" != "true" ]]; then POOLREBALANCER_USE_UNDELEGATE_FALLBACK=true; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=1000000000000000000; fi
      ;;
    target_set_expansion_5val)
      SCENARIO="expansion"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=5; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      ;;
    *)
      echo "invalid SCENARIO: $SCENARIO" >&2
      echo "expected: happy_path|caps|threshold_boundary|fallback|expansion" >&2
      exit 1
      ;;
  esac
}

main() {
  trap on_interrupt INT TERM
  trap cleanup_on_exit EXIT
  PARSED_SUBCOMMAND=""
  if ! parse_cli_args "$@"; then
    usage
    exit 1
  fi

  if [[ "$PARSED_SUBCOMMAND" == "watch" ]]; then
    watch_rebalance_status
    exit 0
  fi
  if [[ "$PARSED_SUBCOMMAND" == "help" ]]; then
    usage
    exit 0
  fi

  require_bin jq
  require_bin curl
  require_bin evmd

  apply_scenario_defaults
  if [[ ! "$VALIDATOR_COUNT" =~ ^[0-9]+$ ]] || (( VALIDATOR_COUNT < 1 )); then
    echo "invalid --nodes/VALIDATOR_COUNT: $VALIDATOR_COUNT (expected positive integer)" >&2
    exit 1
  fi

  case "$DEMO_PROFILE" in
    slow)
      POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-1}"
      POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-10000000000000000000}" # 1e19
      ;;
    medium)
      # Defaults already set above.
      ;;
    fast)
      POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-10}"
      POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-0}" # no cap
      ;;
    *)
      echo "invalid DEMO_PROFILE: $DEMO_PROFILE (expected: slow|medium|fast)" >&2
      exit 1
      ;;
  esac

  # Execution flow:
  # 1) test chain setup and genesis patching
  # 2) scenario seeding
  # 3) sanity checks
  # 4) observe-and-monitor loop + steady monitor
  resolve_mnemonics
  setup_localnet
  configure_genesis_params
  start_validators
  wait_chain_ready
  seed_initial_imbalance
  run_sanity_checks
  observe_and_monitor
}

main "$@"

