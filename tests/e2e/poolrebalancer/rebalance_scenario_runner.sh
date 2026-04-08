#!/usr/bin/env bash
set -euo pipefail

# rebalance_scenario_runner.sh — local E2E for x/poolrebalancer with a CommunityPool contract delegator.
# Interactive / manual inspection (pending queues, watch modes), not a deterministic CI harness.
#
# Scenarios: happy_path | caps | threshold_boundary | fallback | expansion | credit_focus
# Watch: watch (queues + pool reads) | watch credit (ledger + pending undelegations)
#
# Caveat: low minStake can let stake() drain stakeablePrincipalLedger in the same block as a credit; credit_focus
# raises minStake on purpose. Undelegation maturity follows wall clock (header time vs completion), not block height.
# ============================================================================

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-"tcp://127.0.0.1:26657"}"
CHAIN_ID="${CHAIN_ID:-10740}"
KEYRING="${KEYRING:-test}"
HOME0="$BASEDIR/val0"
CHAIN_HOME="${CHAIN_HOME:-$BASEDIR}"
POOL_DELEGATOR_MODE="${POOL_DELEGATOR_MODE:-contract}"
POOL_OWNER_PK="${POOL_OWNER_PK:-0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305}" # gitleaks:allow
POOL_DEPOSITOR_PK="${POOL_DEPOSITOR_PK:-0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544}" # gitleaks:allow
MODULE_EVM="${MODULE_EVM:-0x786c305E2aAc2168BB7555Ef522c5F20a2cd0dA9}"
BOND_PRECOMPILE="${BOND_PRECOMPILE:-0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE}"
# CommunityPool minStakeAmount — credit_focus uses constructor + seed computed min (max_move*mult+1); MODE=max finishes with uint256 max setConfig.
COMMUNITY_POOL_UINT256_MAX="115792089237316195423570985008687907853269984665640564039457584007913129639935"
# credit_focus: CREDIT_FOCUS_MIN_STAKE_MODE=computed (default) → minStake = max_move * MULT + 1 in constructor + seed (uint256 max in ctor would block initial stake() after deposit).
# MODE=max → same computed ctor/seed, then final setConfig applies uint256 max (needs working owner tx).
CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER="${CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER:-4}"
CREDIT_FOCUS_MIN_STAKE_MODE="${CREDIT_FOCUS_MIN_STAKE_MODE:-computed}"
POOL_CONTRACT_ADDR="${POOL_CONTRACT_ADDR:-}"
GOV_FROM="${GOV_FROM:-mykey}"
GOV_HOME="${GOV_HOME:-}"
GOV_DEPOSIT="${GOV_DEPOSIT:-10000000ogwei}"
GOV_FEES="${GOV_FEES:-400000000000000ogwei}"
GOV_WAIT_INITIAL="${GOV_WAIT_INITIAL:-20}"
GOV_POLL_TIMEOUT="${GOV_POLL_TIMEOUT:-20}"
GOV_STATUS_TIMEOUT="${GOV_STATUS_TIMEOUT:-120}"
EVM_RPC="${EVM_RPC:-http://127.0.0.1:8545}"
POOL_SEED_DEPOSIT_AMOUNT="${POOL_SEED_DEPOSIT_AMOUNT:-200000000000000000000}"
DEFAULT_POOL_OWNER_PK="0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305"
DEFAULT_POOL_DEPOSITOR_PK="0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544"

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
USER_SET_POOL_SEED_DEPOSIT_AMOUNT=false
[[ -n "${POOL_SEED_DEPOSIT_AMOUNT+x}" ]] && USER_SET_POOL_SEED_DEPOSIT_AMOUNT=true
USER_SET_STAKING_UNBONDING_TIME=false
[[ -n "${STAKING_UNBONDING_TIME+x}" ]] && USER_SET_STAKING_UNBONDING_TIME=true
USER_SET_POLL_SAMPLES=false
[[ -n "${POLL_SAMPLES+x}" ]] && USER_SET_POLL_SAMPLES=true
USER_SET_POLL_SLEEP_SECS=false
[[ -n "${POLL_SLEEP_SECS+x}" ]] && USER_SET_POLL_SLEEP_SECS=true
USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE=false
[[ -n "${COMMUNITY_POOL_POST_SEED_MIN_STAKE+x}" && -n "${COMMUNITY_POOL_POST_SEED_MIN_STAKE}" ]] && USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE=true

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
FALLBACK_SEEN_REDELEGATION="false"
FALLBACK_UND_DEADLINE_SAMPLES="${FALLBACK_UND_DEADLINE_SAMPLES:-15}"
POOL_DEL_ADDR=""
POOL_EVM_ADDR=""
WATCH_CREDIT_MODE="false"
RESOLVED_GOV_FROM=""
RESOLVED_GOV_HOME=""
WATCH_INITIAL_DELEGATIONS_LOGGED="false"
EXPANSION_MISSING_DSTS=()
EXPANSION_OBSERVED_DSTS_TEXT=""
EXPANSION_INITIAL_DELEGATED=()

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
- Patches genesis staking + poolrebalancer non-delegator params
- Starts val0..valN, deploys CommunityPool, and sets pool delegator params at runtime
- Seeds scenario-specific CommunityPool deposit/stake state
- Polls pending queues so engineers can inspect behavior interactively

What gets patched before node start:
  staking.params:
    - unbonding_time=$STAKING_UNBONDING_TIME
    - max_entries=$STAKING_MAX_ENTRIES
  poolrebalancer.params (genesis):
    - max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS
    - rebalance_threshold_bp=$POOLREBALANCER_THRESHOLD_BP
    - max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK
    - max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP
    - use_undelegate_fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK

Runtime setup after chain start:
  - deploy CommunityPool contract (unless POOL_CONTRACT_ADDR is provided)
  - set CommunityPool automationCaller to MODULE_EVM
  - set poolrebalancer.params.pool_delegator_address to CommunityPool bech32 account

Parameter precedence:
  1) Explicit environment variables (highest priority)
  2) Scenario defaults (applied only for knobs not explicitly set)
  3) Script baseline defaults

Manual caveats: low minStake → same-block stake() can zero the ledger after a credit (not a failed credit).
  Undelegation maturity is time-based. automationCaller=MODULE_EVM and pool_delegator_address=pool contract.

Commands:
  run (default)                     Full test setup + scenario execution
  watch                             Live monitor for an already running test chain
  watch credit                      Same chain; focused on CommunityPool ledger + pending undelegation credit path
  help                              Show this help

CLI options:
  -n, --nodes <count>               Number of validators/nodes to run
  -s, --scenario <name>             Scenario name (same as SCENARIO env var)
  -p, --profile <name>              Demo profile: slow|medium|fast
  -h, --help                        Show this help

Scenarios:
  happy_path
    Goal: baseline rebalance scheduling from a heavily skewed delegation.
    Setup: contract-seeded skew (single-validator staking target + large deposit).
    Params: uses baseline defaults (unless overridden by environment).
    Watch for: pending redelegations to underweight validators.

  caps
    Goal: verify scheduling respects max_ops_per_block and max_move_per_op.
    Setup: same contract-seeded skew as happy_path, but with tight scheduling caps.
    Params: default poolrebalancer max_ops_per_block=1, max_move_per_op=1e18.
    Watch for: capped move sizes and slower progression.

  threshold_boundary
    Goal: verify tiny drift is ignored when threshold is high enough.
    Setup: seed a small contract deposit with single-validator target.
    Params: default poolrebalancer rebalance_threshold_bp=5000.
    Watch for: little or no scheduling when drift stays below threshold.

  fallback
    Goal: verify undelegation fallback is used when redelegation path is constrained.
    Setup: source-heavy contract skew + fallback enabled + low staking max_entries.
    Params: default poolrebalancer use_undelegate_fallback=true; default staking max_entries=1.
    Watch for: pending undelegations appearing alongside or after redelegations.

  expansion
    Goal: verify the target validator set can expand to bonded validators outside the initial seed set.
    Setup: five validators; pool stakes from contract across three validators only (main + two minor deposits).
    Params: max_target_validators=5, max_ops_per_block=1, max_move_per_op=1e19, larger minor seed amount.
    Watch for: redelegations with dst outside the initial three-validator delegation set (expansion_progress in logs).

  credit_focus
    Goal: stress the mature-undelegation → creditStakeableFromRebalance path with visible ledger churn.
    Setup: same skew seed as happy_path/fallback.
    Params: fallback on; staking max_entries=1 (redelegation slots exhausted quickly); unbonding_time=15s;
            max_ops_per_block=1; max_move_per_op=5e17 (many small steps); longer default poll window.
    Deploy + seed: minStakeAmount = max_move * CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER + 1 (constructor + seed setConfig).
    Optional CREDIT_FOCUS_MIN_STAKE_MODE=max sets final minStake to uint256 max. COMMUNITY_POOL_POST_SEED_MIN_STAKE
    (if set) overrides that final minStake floor.
    Watch for: pending_und waves and stakeablePrincipalLedger / totalStaked; use: watch credit in another shell.

Profiles:
  slow                              max_ops_per_block=1, capped move per op
  medium                            default balancing profile
  fast                              more ops per block, no move cap

Environment variables:
  BASEDIR                           Test chain base dir (default: $HOME/.og-evm-devnet)
  NODE_RPC                          RPC endpoint (default: tcp://127.0.0.1:26657)
  CHAIN_ID                          Chain ID (default: 10740)
  TX_FEES                           Fees for txs (default: $TX_FEES)
  CHAIN_HOME                        Home for governance tx signer (default: $CHAIN_HOME)

  VAL0_MNEMONIC ... VALN_MNEMONIC  Optional explicit mnemonics. Any missing values are auto-generated.

  POOLREBALANCER_MAX_TARGET_VALIDATORS
  SCENARIO                          happy_path|caps|threshold_boundary|fallback|expansion|credit_focus
                                    (aliases: baseline_3val, max_target_gt_bonded_3val, fallback_path_3val,
                                    target_set_expansion_5val — normalized in apply_scenario_defaults)
  VALIDATOR_COUNT                   Validators to start (default 3; expansion defaults to 5 if unset)
  DEMO_PROFILE                      slow|medium|fast tuning for rebalance visibility (default: medium)
  POOLREBALANCER_THRESHOLD_BP
  POOLREBALANCER_MAX_OPS_PER_BLOCK
  POOLREBALANCER_MAX_MOVE_PER_OP
  POOLREBALANCER_USE_UNDELEGATE_FALLBACK
  POOL_DELEGATOR_MODE               contract|eoa (default: contract)
  POOL_OWNER_PK                     Private key for deploying/configuring CommunityPool
  POOL_DEPOSITOR_PK                 Private key for CommunityPool deposit txs during seeding
  MODULE_EVM                        Poolrebalancer module EVM address
  BOND_PRECOMPILE                   Bond precompile address
  EVM_RPC                           EVM RPC endpoint for cast calls (default: http://127.0.0.1:8545)
  POOL_CONTRACT_ADDR                Optional existing CommunityPool EVM address to reuse
  POOL_SEED_DEPOSIT_AMOUNT          Main contract seed amount (raw uint, default: 200000000000000000000)
  COMMUNITY_POOL_POST_SEED_MIN_STAKE  If set and non-empty: overrides credit_focus final minStake.
  CREDIT_FOCUS_MIN_STAKE_MODE         computed (default) = max_move * multiplier + 1; max = final setConfig to uint256 max.
  CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER  Used when MODE=computed (default 4).
  GOV_FROM                          Key name used for gov submit/vote (default: mykey)
  GOV_HOME                          Home for gov signer keyring (auto-detected if empty)
  GOV_DEPOSIT                       Gov proposal deposit (default: 10000000ogwei)
  GOV_FEES                          Fees for gov submit/vote txs (default: 400000000000000ogwei)
  GOV_WAIT_INITIAL                  Initial wait before checking proposal status (default: 20)
  GOV_POLL_TIMEOUT                  Timeout waiting for params propagation seconds (default: 20)
  GOV_STATUS_TIMEOUT                Timeout waiting proposal to pass (default: 120)

  STAKING_UNBONDING_TIME            Reduce so pending queues mature quickly (default: 30s; credit_focus defaults 15s)
  STAKING_MAX_ENTRIES               Raise/lower redelegation/undelegation entry pressure (default: 100)

  POLL_SAMPLES                      Initial poll samples before giving up if no pending ops (default: 25; credit_focus 90)
  POLL_SLEEP_SECS                   Seconds between samples (default: 2)

  IMBALANCE_MAIN_DELEGATION         Large delegation to validator[0]
  IMBALANCE_MINOR_DELEGATION        Small delegations to validator[1], validator[2]
  WATCH_COMPACT                     Compact lines for `watch` (not watch credit; default: false)
  CREDIT_WATCH_USE_ENV_POOL_EVM     If true, watch credit keeps POOL_EVM_ADDR from env instead of chain
  FALLBACK_UND_DEADLINE_SAMPLES     Fallback deadline (samples) to observe pending undelegations (default: 15)

Note:
  Any variable set in the environment overrides scenario defaults when the script respects USER_SET_* flags.
  For undelegation → contract credit, read "Manual caveats" above before interpreting stakeablePrincipalLedger.

Examples:
  # Standard rebalance flow
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3 --profile medium

  # Cap-focused behavior
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario caps --nodes 3 --profile slow

  # Threshold gating (expect no scheduling for small drift)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario threshold_boundary --nodes 3

  # Fallback-focused profile
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario fallback --nodes 3 --profile slow

  # Target-set expansion (5 validators; pool initially delegated to 3)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario expansion --nodes 5 --profile medium

  # Undelegation credit path (pair with watch credit in another shell)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario credit_focus --nodes 3 --profile medium
  SCENARIO=credit_focus bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch credit

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
        if [[ "${1:-}" == "credit" ]]; then
          WATCH_CREDIT_MODE="true"
          shift
        fi
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
  local start status_url
  status_url="$(tendermint_status_url)"
  start="$(date +%s)"
  while true; do
    local h
    h="$(curl -sS --max-time 1 "$status_url" 2>/dev/null | jq -r '.result.sync_info.latest_block_height' 2>/dev/null || echo 0)"
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

tendermint_status_url() {
  local hp="${NODE_RPC#tcp://}"
  hp="${hp#http://}"
  hp="${hp#https://}"
  printf 'http://%s/status' "$hp"
}

derive_evm_rpc_from_node_rpc() {
  local node="$1"
  local hostport host port idx jsonrpc_port
  hostport="${node#tcp://}"
  hostport="${hostport#http://}"
  hostport="${hostport#https://}"
  host="${hostport%%:*}"
  port="${hostport##*:}"
  if [[ "$port" =~ ^[0-9]+$ ]] && (( port >= 26657 )) && (( (port - 26657) % 100 == 0 )); then
    idx=$(( (port - 26657) / 100 ))
    jsonrpc_port=$((8545 + (idx * 10)))
    printf 'http://%s:%s' "$host" "$jsonrpc_port"
    return 0
  fi
  return 1
}

ensure_evm_rpc_ready() {
  CURRENT_PHASE="ensure_evm_rpc_ready"
  local candidates=()
  local derived=""
  local c start now

  candidates+=("$EVM_RPC")
  if derived="$(derive_evm_rpc_from_node_rpc "$NODE_RPC" 2>/dev/null || true)"; then
    candidates+=("$derived")
  fi
  candidates+=("http://127.0.0.1:8545" "http://127.0.0.1:8555" "http://127.0.0.1:8565" "http://127.0.0.1:8575")

  echo "==> Waiting for EVM RPC readiness"
  start="$(date +%s)"
  while true; do
    for c in "${candidates[@]}"; do
      [[ -z "$c" ]] && continue
      if cast chain-id --rpc-url "$c" >/dev/null 2>&1; then
        if [[ "$EVM_RPC" != "$c" ]]; then
          echo "==> Using detected EVM RPC endpoint: $c"
        fi
        EVM_RPC="$c"
        return 0
      fi
    done
    now="$(date +%s)"
    if (( now - start > 90 )); then
      echo "error: no reachable EVM RPC endpoint found after 90s" >&2
      echo "tried: ${candidates[*]}" >&2
      return 1
    fi
    sleep 2
  done
}

dev_account_private_key_from_file() {
  local account_name="$1"
  local f="$BASEDIR/dev_accounts.txt"
  [[ -f "$f" ]] || return 1
  awk -v name="$account_name" '
    $0 ~ ("^" name ":") {in_block=1; next}
    in_block && $1=="private_key:" {print $2; exit}
    in_block && /^[^[:space:]]/ {in_block=0}
  ' "$f"
}

resolve_pool_runtime_keys() {
  if [[ "$POOL_DELEGATOR_MODE" != "contract" ]]; then
    return 0
  fi
  local dev0_pk dev1_pk
  if [[ "$POOL_OWNER_PK" == "$DEFAULT_POOL_OWNER_PK" ]]; then
    dev0_pk="$(dev_account_private_key_from_file "dev0" || true)"
    if [[ -n "$dev0_pk" ]]; then
      POOL_OWNER_PK="$dev0_pk"
      echo "==> Using generated dev0 private key as POOL_OWNER_PK"
    fi
  fi
  if [[ "$POOL_DEPOSITOR_PK" == "$DEFAULT_POOL_DEPOSITOR_PK" ]]; then
    dev1_pk="$(dev_account_private_key_from_file "dev1" || true)"
    if [[ -n "$dev1_pk" ]]; then
      POOL_DEPOSITOR_PK="$dev1_pk"
      echo "==> Using generated dev1 private key as POOL_DEPOSITOR_PK"
    fi
  fi
}

resolve_governance_signer() {
  CURRENT_PHASE="resolve_governance_signer"
  local requested_home candidate_from candidate_home
  requested_home="${GOV_HOME:-$CHAIN_HOME}"

  # 1) honor configured GOV_FROM when available
  if evmd keys show "$GOV_FROM" --keyring-backend "$KEYRING" --home "$requested_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$GOV_FROM"
    RESOLVED_GOV_HOME="$requested_home"
    echo "==> Using configured governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  # 2) fallback to val0 in validator 0 home (multi_node_startup default key naming)
  candidate_from="val0"
  candidate_home="$HOME0"
  if evmd keys show "$candidate_from" --keyring-backend "$KEYRING" --home "$candidate_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$candidate_from"
    RESOLVED_GOV_HOME="$candidate_home"
    echo "==> Falling back to governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  # 3) fallback to mykey in CHAIN_HOME for local_node-style setups
  candidate_from="mykey"
  candidate_home="$CHAIN_HOME"
  if evmd keys show "$candidate_from" --keyring-backend "$KEYRING" --home "$candidate_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$candidate_from"
    RESOLVED_GOV_HOME="$candidate_home"
    echo "==> Falling back to governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  echo "error: could not resolve a governance signer key for submit/vote" >&2
  echo "tried: GOV_FROM=$GOV_FROM home=$requested_home, val0@$HOME0, mykey@$CHAIN_HOME" >&2
  return 1
}

vote_proposal_with_validator_majority() {
  local proposal_id="$1"
  local success_votes=0
  local required_votes=$((VALIDATOR_COUNT / 2 + 1))
  local i from home vote_out vote_code vote_log

  echo "==> Voting proposal_id=$proposal_id with validator keys (target yes votes: $required_votes/$VALIDATOR_COUNT)"
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    from="val${i}"
    home="$BASEDIR/val${i}"
    if ! evmd keys show "$from" --keyring-backend "$KEYRING" --home "$home" >/dev/null 2>&1; then
      continue
    fi
    vote_out="$(evmd tx gov vote "$proposal_id" yes \
      --from "$from" --keyring-backend "$KEYRING" --home "$home" \
      --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
      --fees "$GOV_FEES" --gas auto --gas-adjustment 1.3 -y -o json 2>/dev/null || true)"
    vote_code="$(echo "$vote_out" | jq -r '.code // 0' 2>/dev/null || echo 1)"
    if [[ "$vote_code" == "0" ]]; then
      success_votes=$((success_votes + 1))
    else
      vote_log="$(echo "$vote_out" | jq -r '.raw_log // .log // empty' 2>/dev/null || true)"
      echo "warning: vote from $from failed (code=$vote_code): $vote_log" >&2
    fi
  done

  # Fallback vote path using resolved signer (for local_node-style key layouts).
  if (( success_votes < required_votes )); then
    vote_out="$(evmd tx gov vote "$proposal_id" yes \
      --from "$RESOLVED_GOV_FROM" --keyring-backend "$KEYRING" --home "$RESOLVED_GOV_HOME" \
      --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
      --fees "$GOV_FEES" --gas auto --gas-adjustment 1.3 -y -o json 2>/dev/null || true)"
    vote_code="$(echo "$vote_out" | jq -r '.code // 0' 2>/dev/null || echo 1)"
    if [[ "$vote_code" == "0" ]]; then
      success_votes=$((success_votes + 1))
    fi
  fi

  echo "governance_votes_submitted=$success_votes"
  if (( success_votes < required_votes )); then
    echo "error: insufficient successful governance votes submitted ($success_votes/$required_votes required)" >&2
    return 1
  fi
  return 0
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
    echo "set them in env or ensure validator mnemonic generation is available" >&2
    exit 1
  fi
}

patch_genesis_poolrebalancer_params() {
  local gen0="$BASEDIR/val0/config/genesis.json"
  local tmp="$BASEDIR/val0/config/genesis.tmp.json"

  jq --argjson maxTargets "$POOLREBALANCER_MAX_TARGET_VALIDATORS" \
     --argjson thr "$POOLREBALANCER_THRESHOLD_BP" \
     --argjson maxOps "$POOLREBALANCER_MAX_OPS_PER_BLOCK" \
     --arg maxMove "$POOLREBALANCER_MAX_MOVE_PER_OP" \
     --argjson useUndel "$POOLREBALANCER_USE_UNDELEGATE_FALLBACK" \
     ' .app_state.poolrebalancer.params.max_target_validators = $maxTargets
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

evmd_debug_addr() {
  local addr="$1"
  if [[ -n "${CHAIN_HOME:-}" && -d "$CHAIN_HOME" ]]; then
    evmd debug addr "$addr" --home "$CHAIN_HOME" 2>/dev/null || evmd debug addr "$addr" 2>/dev/null || true
  else
    evmd debug addr "$addr" 2>/dev/null || true
  fi
}

resolve_evm_hex_from_bech32() {
  local bech="$1"
  local dbg hex
  dbg="$(evmd_debug_addr "$bech")"
  hex="$(printf '%s\n' "$dbg" | awk '{for(i=1;i<=NF;i++){if($i ~ /^0x[0-9a-fA-F]{40}$/){print $i; exit}}}')"
  if [[ -z "$hex" ]]; then
    # Fallback: derive from bech32 bytes using evmd debug output fields.
    hex="$(printf '%s\n' "$dbg" | awk -F': ' '/Address \(hex\)/{print $2; exit}')"
  fi
  if [[ "$hex" =~ ^[0-9a-fA-F]{40}$ ]]; then
    hex="0x$hex"
  fi
  printf '%s' "$hex"
}

pool_addr_from_cast_deploy_output() {
  local raw="$1"
  local addr=""
  if addr="$(printf '%s' "$raw" | jq -r '.contractAddress // .receipt.contractAddress // empty' 2>/dev/null)" &&
    [[ -n "$addr" && "$addr" != "null" ]]; then
    printf '%s' "$addr"
    return 0
  fi
  if [[ "$raw" =~ \"contractAddress\"[[:space:]]*:[[:space:]]*\"(0x[0-9a-fA-F]{40})\" ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

resolve_pool_bech32() {
  if [[ -z "$POOL_EVM_ADDR" ]]; then
    echo "error: resolve_pool_bech32 requires POOL_EVM_ADDR" >&2
    exit 1
  fi
  POOL_DEL_ADDR="$(evmd_debug_addr "$POOL_EVM_ADDR" | awk '/Bech32 Acc/{print $3; exit}')"
  if [[ -z "$POOL_DEL_ADDR" ]]; then
    echo "error: could not resolve bech32 address for pool contract: $POOL_EVM_ADDR" >&2
    exit 1
  fi
}

normalize_cast_uint256_output() {
  local s="${1:-}"
  s="${s//$'\r'/}"
  s="${s%%$'\n'*}"
  s="${s%% *}"
  s="${s//$'\t'/}"
  [[ "$s" =~ ^[0-9]+$ ]] && { printf '%s' "$s"; return 0; }
  # cast call usually returns hex (e.g. 0x000...001); decode so minStake / ledger reads match chain.
  if [[ "$s" =~ ^0x[0-9a-fA-F]+$ ]] && command -v python3 >/dev/null 2>&1; then
    python3 -c "print(int('$s',16))" 2>/dev/null && return 0
  fi
  return 1
}

pool_call_uint256() {
  local sig="$1"
  local raw norm
  [[ -z "${POOL_EVM_ADDR:-}" ]] && { printf 'n/a'; return 0; }
  raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "$sig" 2>/dev/null || true)"
  if norm="$(normalize_cast_uint256_output "$raw")"; then
    printf '%s' "$norm"
  else
    printf 'n/a'
  fi
}

wait_evm_nonce_settled_for_pk() {
  local pk="$1"
  local deadline_sec="${2:-45}"
  local addr pending latest t0
  addr="$(cast wallet address --private-key "$pk")"
  t0="$(date +%s)"
  while true; do
    pending="$(cast nonce --rpc-url "$EVM_RPC" --block pending "$addr" 2>/dev/null || true)"
    latest="$(cast nonce --rpc-url "$EVM_RPC" --block latest "$addr" 2>/dev/null || true)"
    [[ -z "$pending" || -z "$latest" ]] && return 0
    [[ "$pending" == "$latest" ]] && return 0
    if (( $(date +%s) - t0 > deadline_sec )); then
      return 0
    fi
    sleep 1
  done
}

_bumped_gas_price() {
  local gp gp2
  gp="$(cast gas-price --rpc-url "$EVM_RPC" 2>/dev/null || echo 1000000)"
  gp2="$(awk -v g="$gp" 'BEGIN { print int(g) * 2 }' 2>/dev/null || true)"
  [[ -z "$gp2" || "$gp2" == "0" ]] && gp2="$gp"
  printf '%s' "$gp2"
}

deposit_to_pool_once() {
  local amount="$1"
  local approve_json="${2:-/tmp/pool_seed_approve.json}"
  local deposit_json="${3:-/tmp/pool_seed_deposit.json}"
  local errf gp2
  wait_evm_nonce_settled_for_pk "$POOL_DEPOSITOR_PK" 45
  errf="$(mktemp -t pool_dep.XXXXXX)"
  if cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_EVM_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" "$POOL_EVM_ADDR" \
      "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"; then
    rm -f "$errf"
    return 0
  fi
  gp2="$(_bumped_gas_price)"
  cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" --gas-price "$gp2" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_EVM_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" --gas-price "$gp2" "$POOL_EVM_ADDR" \
      "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"
  local st=$?
  rm -f "$errf"
  return "$st"
}

coin_amount_to_uint() {
  local coin="$1"
  local amount
  amount="$(printf '%s' "$coin" | sed -E 's/^([0-9]+).*/\1/')"
  if [[ -z "$amount" || ! "$amount" =~ ^[0-9]+$ ]]; then
    echo "error: could not parse numeric amount from coin string: $coin" >&2
    return 1
  fi
  printf '%s' "$amount"
}

# Printed before deploy + wiring so every scenario run explains pool setup, timing, and active knobs.
log_pool_contract_setup_banner() {
  echo
  echo "──────────────────────────────────────────────────────────────────────────────"
  echo "CommunityPool setup  (scenario=$SCENARIO)"
  echo "  What happens next: deploy or reuse contract → set automationCaller (EVM tx) → governance"
  echo "    updates poolrebalancer.pool_delegator_address to this pool’s bech32 account (vote + propagate)."
  echo "  Deploy timing: fresh deploy is ONE cast create tx. Expect ~15–90s wall-clock on a local devnet:"
  echo "    blocks must include the tx; nonce/gas contention can add retries. Reusing POOL_CONTRACT_ADDR skips deploy."
  echo "  Gov wiring timing: proposal + majority votes + param propagation (often ~1–3 min; cap $GOV_STATUS_TIMEOUT s)."
  echo "  Typical local devnet: pool_delegator_address usually shows up around block heights ~30–32 (varies with block time / votes)."
  echo "  Params already chosen for this run (genesis + scenario):"
  echo "    poolrebalancer: max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS  threshold_bp=$POOLREBALANCER_THRESHOLD_BP"
  echo "                    max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK  max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP"
  echo "                    use_undelegate_fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK"
  echo "    staking:        unbonding_time=$STAKING_UNBONDING_TIME  max_entries=$STAKING_MAX_ENTRIES"
  echo "    validators:     VALIDATOR_COUNT=$VALIDATOR_COUNT  EVM_RPC=$EVM_RPC"
  echo "──────────────────────────────────────────────────────────────────────────────"
  echo
}

deploy_pool_contract_if_needed() {
  CURRENT_PHASE="deploy_pool_contract"
  if [[ "$POOL_DELEGATOR_MODE" != "contract" ]]; then
    echo "error: unsupported POOL_DELEGATOR_MODE=$POOL_DELEGATOR_MODE (phase1 supports contract only)" >&2
    exit 1
  fi
  if [[ -n "$POOL_CONTRACT_ADDR" ]]; then
    POOL_EVM_ADDR="$POOL_CONTRACT_ADDR"
    echo "==> Reusing existing CommunityPool (POOL_CONTRACT_ADDR=$POOL_CONTRACT_ADDR) — no deploy tx; address ready immediately."
  else
    echo "==> Deploying CommunityPool contract (contract creation via cast send --create)"
    echo "    Typical wait: one mined block + any gas-price retry; if this hangs, check validator logs and EVM RPC reachability."
    local owner bytecode ctor_args data deploy_raw deploy_err owner_balance gp2
    if ! cast chain-id --rpc-url "$EVM_RPC" >/dev/null 2>&1; then
      echo "error: EVM RPC is not reachable at $EVM_RPC (cast chain-id failed)" >&2
      exit 1
    fi
    owner="$(cast wallet address --private-key "$POOL_OWNER_PK")"
    owner_balance="$(cast balance --rpc-url "$EVM_RPC" "$owner" 2>/dev/null || echo "0")"
    if [[ "$owner_balance" =~ ^[0-9]+$ ]] && [[ "$owner_balance" == "0" ]]; then
      echo "warning: pool owner address $owner has zero EVM balance on $EVM_RPC; deploy may fail" >&2
    fi
    bytecode="$(jq -r '.bytecode // empty' "$ROOT_DIR/contracts/solidity/pool/CommunityPool.json" 2>/dev/null || true)"
    if [[ -z "$bytecode" || "$bytecode" == "null" ]]; then
      echo "error: missing CommunityPool bytecode in contracts/solidity/pool/CommunityPool.json" >&2
      exit 1
    fi
    local ctor_min_stake=1
    if [[ "$SCENARIO" == "credit_focus" ]]; then
      ctor_min_stake="$(compute_credit_focus_post_seed_min_stake)"
      if command -v python3 >/dev/null 2>&1 && [[ "$ctor_min_stake" =~ ^[0-9]+$ ]]; then
        if ! python3 -c "import sys; sys.exit(0 if int('$ctor_min_stake') > 1 else 1)"; then
          echo "error: credit_focus constructor minStakeAmount=$ctor_min_stake (fix POOLREBALANCER_MAX_MOVE_PER_OP / scenario order)" >&2
          exit 1
        fi
      elif [[ "$ctor_min_stake" == "1" ]]; then
        echo "error: credit_focus constructor minStake resolved to 1 (need python3 for large ints or valid max_move)" >&2
        exit 1
      fi
      echo "==> credit_focus: constructor minStakeAmount=$ctor_min_stake"
      echo "    Why: max_move_per_op * CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER(${CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER:-4}) + 1"
      echo "    → typical undelegation credits stay BELOW minStake so stakeablePrincipalLedger stays visible after credit;"
      echo "    EndBlock stake() only delegates when ledger >= minStake (otherwise it no-ops)."
    else
      echo "==> Constructor minStakeAmount=1 (default for this scenario): credits can be auto-staked same block if ledger reaches minStake;"
      echo "    use credit_focus or raise minStake via setConfig if you need to observe liquid ledger longer."
    fi
    ctor_args="$(cast abi-encode "constructor(address,uint32,uint32,uint256,address)" "$BOND_PRECOMPILE" 10 5 "$ctor_min_stake" "$owner")"
    data="${bytecode}${ctor_args#0x}"
    deploy_err="$(mktemp -t pool_deploy_err.XXXXXX)"
    deploy_raw="$(cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" --create "$data" 2>"$deploy_err" || true)"
    if [[ -z "$deploy_raw" ]]; then
      gp2="$(_bumped_gas_price)"
      deploy_raw="$(cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" --gas-price "$gp2" --create "$data" 2>"$deploy_err" || true)"
    fi
    if [[ -z "$deploy_raw" ]]; then
      echo "error: failed to deploy CommunityPool contract" >&2
      echo "detail: $(tr '\n' ' ' <"$deploy_err" | head -c 500)" >&2
      rm -f "$deploy_err"
      exit 1
    fi
    if ! POOL_EVM_ADDR="$(pool_addr_from_cast_deploy_output "$deploy_raw")" || [[ -z "$POOL_EVM_ADDR" ]]; then
      echo "error: could not parse CommunityPool contract address from deploy output" >&2
      echo "deploy_output: $(printf '%s' "$deploy_raw" | head -c 500)" >&2
      echo "deploy_error: $(tr '\n' ' ' <"$deploy_err" | head -c 500)" >&2
      rm -f "$deploy_err"
      exit 1
    fi
    rm -f "$deploy_err"
  fi
  resolve_pool_bech32
  echo "pool_contract_evm=$POOL_EVM_ADDR pool_delegator_bech32=$POOL_DEL_ADDR"
}

set_pool_automation_caller() {
  CURRENT_PHASE="set_pool_automation_caller"
  echo "==> Setting CommunityPool automationCaller=$MODULE_EVM (single EVM tx; poolrebalancer module must be this caller)"
  cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" \
    "$POOL_EVM_ADDR" "setAutomationCaller(address)" "$MODULE_EVM" >/dev/null
}

set_pool_delegator_param_runtime() {
  CURRENT_PHASE="set_pool_delegator_param_runtime"
  echo "==> Governance: set poolrebalancer.params.pool_delegator_address=$POOL_DEL_ADDR"
  echo "    This can take up to ~$GOV_STATUS_TIMEOUT s (vote + PROPOSAL_STATUS_PASSED + param poll) — longer than deploy on some machines."
  local gov_auth current proposal_json status current_addr t0 elapsed submit_out proposal_id before_latest after_latest submit_code submit_log vote_out vote_code vote_log
  resolve_governance_signer
  gov_auth="$(evmd query auth module-account gov --node "$NODE_RPC" -o json | jq -r '.account.value.address')"
  current="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json)"
  before_latest="$(evmd query gov proposals --node "$NODE_RPC" -o json 2>/dev/null | jq -r '[.proposals[]?.id | tonumber] | max // 0')"
  proposal_json="$(echo "$current" | jq --arg gov "$gov_auth" --arg del "$POOL_DEL_ADDR" --arg dep "$GOV_DEPOSIT" '{
    messages:[{
      "@type":"/cosmos.poolrebalancer.v1.MsgUpdateParams",
      authority:$gov,
      params:{
        pool_delegator_address:$del,
        max_target_validators:.params.max_target_validators,
        rebalance_threshold_bp:.params.rebalance_threshold_bp,
        max_ops_per_block:.params.max_ops_per_block,
        max_move_per_op:.params.max_move_per_op,
        use_undelegate_fallback:.params.use_undelegate_fallback
      }
    }],
    metadata:"",
    deposit:$dep,
    title:"Set pool delegator for rebalance scenario runner",
    summary:"Set CommunityPool account for poolrebalancer runtime scenarios.",
    expedited:false
  }')"
  submit_out="$(evmd tx gov submit-proposal <(echo "$proposal_json") \
    --from "$RESOLVED_GOV_FROM" --keyring-backend "$KEYRING" --home "$RESOLVED_GOV_HOME" \
    --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
    --fees "$GOV_FEES" --gas auto --gas-adjustment 1.5 -y -o json)"
  submit_code="$(echo "$submit_out" | jq -r '.code // 0')"
  if [[ "$submit_code" != "0" ]]; then
    submit_log="$(echo "$submit_out" | jq -r '.raw_log // .log // empty')"
    echo "error: governance submit-proposal failed (code=$submit_code)" >&2
    echo "detail: $submit_log" >&2
    exit 1
  fi
  proposal_id="$(echo "$submit_out" | jq -r '
    .proposal_id // .tx_response?.proposal_id // .tx_response?.events[]? | select(.type=="submit_proposal") | .attributes[]? | select(.key=="proposal_id" or .key=="cHJvcG9zYWxfaWQ=") | .value
  ' 2>/dev/null | tail -n 1)"
  if [[ -z "$proposal_id" || "$proposal_id" == "null" ]]; then
    t0="$(date +%s)"
    while true; do
      after_latest="$(evmd query gov proposals --node "$NODE_RPC" -o json 2>/dev/null | jq -r '[.proposals[]?.id | tonumber] | max // 0')"
      if [[ "$after_latest" =~ ^[0-9]+$ ]] && (( after_latest > before_latest )); then
        proposal_id="$after_latest"
        break
      fi
      elapsed="$(($(date +%s) - t0))"
      if (( elapsed > 30 )); then
        break
      fi
      sleep 2
    done
  fi
  if [[ -z "$proposal_id" || "$proposal_id" == "null" ]]; then
    echo "error: could not determine governance proposal id for pool delegator update" >&2
    echo "debug: before_latest=$before_latest submit_out_snippet=$(printf '%s' "$submit_out" | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g' | cut -c1-300)" >&2
    exit 1
  fi
  echo "proposal_id=$proposal_id"
  if ! vote_proposal_with_validator_majority "$proposal_id"; then
    exit 1
  fi
  sleep "$GOV_WAIT_INITIAL"
  t0="$(date +%s)"
  while true; do
    status="$(evmd query gov proposal "$proposal_id" --node "$NODE_RPC" -o json | jq -r '.proposal.status')"
    case "$status" in
      PROPOSAL_STATUS_PASSED)
        break
        ;;
      PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED|PROPOSAL_STATUS_ABORTED)
        echo "error: governance proposal reached terminal non-passed status=$status" >&2
        exit 1
        ;;
      *)
        elapsed="$(($(date +%s) - t0))"
        if (( elapsed > GOV_STATUS_TIMEOUT )); then
          echo "error: governance proposal did not pass before timeout (status=$status timeout=${GOV_STATUS_TIMEOUT}s)" >&2
          exit 1
        fi
        sleep 2
        ;;
    esac
  done
  t0="$(date +%s)"
  while true; do
    current_addr="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address // ""')"
    [[ "$current_addr" == "$POOL_DEL_ADDR" ]] && break
    elapsed="$(($(date +%s) - t0))"
    if [[ "$elapsed" -gt "$GOV_POLL_TIMEOUT" ]]; then
      echo "error: pool_delegator_address not propagated (have=$current_addr want=$POOL_DEL_ADDR)" >&2
      exit 1
    fi
    sleep 2
  done
}

configure_contract_pool_delegator() {
  log_pool_contract_setup_banner
  deploy_pool_contract_if_needed
  set_pool_automation_caller
  set_pool_delegator_param_runtime
}

set_pool_contract_config() {
  local max_retrieve="$1"
  local max_validators="$2"
  local min_stake="$3"
  local errf
  errf="$(mktemp "${TMPDIR:-/tmp}/poolrebalancer-setconfig.XXXXXX")"
  if ! cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" \
    "$POOL_EVM_ADDR" "setConfig(uint32,uint32,uint256)" "$max_retrieve" "$max_validators" "$min_stake" 2>"$errf" >/dev/null; then
    echo "error: CommunityPool setConfig failed (max_retrieve=$max_retrieve max_validators=$max_validators min_stake=$min_stake pool=$POOL_EVM_ADDR)" >&2
    cat "$errf" >&2
    rm -f "$errf"
  return 1
  fi
  rm -f "$errf"
}

verify_contract_pool_readiness() {
  CURRENT_PHASE="verify_contract_pool_readiness"
  echo "==> Verifying contract pool readiness"
  local onchain_del caller_raw caller_lc module_lc stakeable total_staked principal_assets
  onchain_del="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address // ""')"
  if [[ "$onchain_del" != "$POOL_DEL_ADDR" ]]; then
    echo "error: readiness failed; pool_delegator_address mismatch (have=$onchain_del want=$POOL_DEL_ADDR)" >&2
    exit 1
  fi
  caller_raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "automationCaller()(address)" 2>/dev/null || true)"
  caller_lc="$(printf '%s' "$caller_raw" | tr '[:upper:]' '[:lower:]')"
  module_lc="$(printf '%s' "$MODULE_EVM" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "$caller_lc" || "$caller_lc" != "$module_lc" ]]; then
    echo "error: readiness failed; automationCaller mismatch (have=$caller_raw want=$MODULE_EVM)" >&2
    exit 1
  fi

  stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
  total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
  principal_assets="$(pool_call_uint256 "principalAssets()(uint256)")"
  if [[ "$stakeable" == "n/a" || "$total_staked" == "n/a" || "$principal_assets" == "n/a" ]]; then
    echo "error: readiness failed; unable to query CommunityPool accounting views" >&2
    exit 1
  fi
  echo "pool_readiness: stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets"
}

wait_for_pool_stake_activation() {
  local timeout_secs="${1:-120}"
  local start now total_staked del_count
  start="$(date +%s)"
  while true; do
    total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
    del_count="$(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json 2>/dev/null | jq -r '.delegation_responses | length // 0')"
    if [[ "$total_staked" =~ ^[0-9]+$ ]] && (( total_staked > 0 )) && [[ "$del_count" =~ ^[0-9]+$ ]] && (( del_count > 0 )); then
      echo "pool_stake_activated: total_staked=$total_staked delegations_count=$del_count"
      return 0
    fi
    now="$(date +%s)"
    if (( now - start > timeout_secs )); then
      echo "error: timed out waiting for CommunityPool automated stake activation" >&2
      echo "hint: verify automationCaller, pool_delegator_address, and chain EndBlock logs" >&2
      return 1
    fi
    sleep 2
  done
}

# credit_focus post-seed minStake: strictly above most batched credit sums so stake() keeps skipping (CommunityPool.stake early-return).
compute_credit_focus_post_seed_min_stake() {
  local mm mul
  mm="${POOLREBALANCER_MAX_MOVE_PER_OP:-0}"
  mul="${CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER:-4}"
  if ! [[ "$mm" =~ ^[0-9]+$ ]]; then
    echo "$COMMUNITY_POOL_UINT256_MAX"
    return 0
  fi
  if ! [[ "$mul" =~ ^[0-9]+$ ]]; then
    mul=4
  fi
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "print(int('$mm') * int('$mul') + 1)"
  else
    echo $(( mm * mul + 1 ))
  fi
}

seed_contract_imbalance() {
  CURRENT_PHASE="seed_contract_imbalance"
  echo "==> Creating contract-driven initial imbalance (scenario=$SCENARIO)"
  local seed_max_validators=1
  local seed_amount_main seed_amount_minor
  seed_amount_main="$POOL_SEED_DEPOSIT_AMOUNT"
  seed_amount_minor="$(coin_amount_to_uint "$IMBALANCE_MINOR_DELEGATION")"
  echo "seed_plan: main=$seed_amount_main minor=$seed_amount_minor"

  case "$SCENARIO" in
    expansion)
      seed_max_validators=3
      ;;
    threshold_boundary|happy_path|caps|fallback|credit_focus)
      seed_max_validators=1
      ;;
    *)
      echo "error: unsupported SCENARIO in contract seeding: $SCENARIO" >&2
      exit 1
      ;;
  esac

  local seed_step_min_stake=1
  if [[ "$SCENARIO" == "credit_focus" ]]; then
    seed_step_min_stake="$(compute_credit_focus_post_seed_min_stake)"
    echo "credit_focus: pre-seed setConfig — minStake stays $seed_step_min_stake (same as constructor)."
    echo "  Why: we only narrow max_validators to $seed_max_validators for skewed deposit/stake; minStake unchanged so"
    echo "  accounting matches deploy and credits are still below minStake during seed."
  fi
  echo "==> Applying CommunityPool setConfig for seeding (max_retrieve=10 max_validators=$seed_max_validators minStake=$seed_step_min_stake)"
  set_pool_contract_config 10 "$seed_max_validators" "$seed_step_min_stake"

  case "$SCENARIO" in
    threshold_boundary)
      echo "==> Seeding small contract deposit: amount=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
    expansion)
      echo "==> Seeding contract deposits for expansion profile: main=$seed_amount_main minor=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_main"
      deposit_to_pool_once "$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
    happy_path|caps|fallback|credit_focus)
      echo "==> Seeding contract deposits for skew profile: main=$seed_amount_main minor=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_main"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
  esac

  wait_for_pool_stake_activation 150

  if [[ "$SCENARIO" == "expansion" ]]; then
    EXPANSION_INITIAL_DELEGATED=()
    while IFS= read -r val; do
      [[ -z "$val" ]] && continue
      EXPANSION_INITIAL_DELEGATED+=("$val")
    done < <(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json | jq -r '.delegation_responses[]?.delegation.validator_address' | head -n 3)
    echo "expansion_seeded_validators=${#EXPANSION_INITIAL_DELEGATED[@]}"
  fi

  # Restore broader staking spread for post-seed automation behavior.
  # EndBlock order: CompletePendingUndelegations → creditStakeableFromRebalance (ledger up) →
  # MaybeRunCommunityPoolAutomation → stake() delegates stakeablePrincipalLedger when >= minStakeAmount.
  # With minStakeAmount=1, the same block usually drains the ledger again, so RPC shows stakeablePrincipalLedger=0.
  # minStakeAmount gates stake(): stake() only delegates when stakeablePrincipalLedger >= minStake (strict).
  # creditStakeableFromRebalance ignores minStake; for a visible ledger after credit, keep minStake above typical
  # credit_focus final minStake: computed (default) matches constructor/seed; max = uint256 after seed (optional).
  local post_seed_min_stake="1"
  if [[ "$SCENARIO" == "credit_focus" ]]; then
    if [[ "$USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE" == "true" && "$COMMUNITY_POOL_POST_SEED_MIN_STAKE" == "1" ]]; then
      echo "error: COMMUNITY_POOL_POST_SEED_MIN_STAKE=1 defeats credit_focus (auto-stake after credit). Unset or use a large wei / uint256 max." >&2
      exit 1
    fi
    case "${CREDIT_FOCUS_MIN_STAKE_MODE:-computed}" in
      computed|"")
        post_seed_min_stake="$(compute_credit_focus_post_seed_min_stake)"
        if command -v python3 >/dev/null 2>&1 && [[ "$post_seed_min_stake" =~ ^[0-9]+$ ]]; then
          if ! python3 -c "import sys; sys.exit(0 if int('$post_seed_min_stake') > 1 else 1)"; then
            echo "error: credit_focus final minStakeAmount=$post_seed_min_stake (max_move_per_op zero?). Check POOLREBALANCER_MAX_MOVE_PER_OP." >&2
            exit 1
          fi
        elif [[ "$post_seed_min_stake" == "1" ]]; then
          echo "error: credit_focus final minStake resolved to 1" >&2
          exit 1
        fi
        ;;
      max)
        post_seed_min_stake="$COMMUNITY_POOL_UINT256_MAX"
        ;;
      *)
        echo "error: invalid CREDIT_FOCUS_MIN_STAKE_MODE=${CREDIT_FOCUS_MIN_STAKE_MODE:-} (expected computed or max)" >&2
        exit 1
        ;;
    esac
  fi
  if [[ "$USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE" == "true" ]]; then
    if [[ "$SCENARIO" == "credit_focus" ]]; then
      local cf_default
      cf_default="$(compute_credit_focus_post_seed_min_stake)"
      if [[ "$COMMUNITY_POOL_POST_SEED_MIN_STAKE" != "$COMMUNITY_POOL_UINT256_MAX" ]] && command -v python3 >/dev/null 2>&1 \
        && [[ "$COMMUNITY_POOL_POST_SEED_MIN_STAKE" =~ ^[0-9]+$ && "$cf_default" =~ ^[0-9]+$ ]] \
        && python3 -c "import sys; sys.exit(0 if int('$COMMUNITY_POOL_POST_SEED_MIN_STAKE') <= int('$cf_default') else 1)"; then
        echo "==> warning: COMMUNITY_POOL_POST_SEED_MIN_STAKE=$COMMUNITY_POOL_POST_SEED_MIN_STAKE <= credit_focus default $cf_default (max_move * ${CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER:-4} + 1); stake() may consume credited ledger same block" >&2
      fi
    fi
    post_seed_min_stake="$COMMUNITY_POOL_POST_SEED_MIN_STAKE"
  fi
  echo "==> Final CommunityPool setConfig (max_retrieve=10 max_validators=5 minStake=$post_seed_min_stake)"
  if [[ "$SCENARIO" == "credit_focus" ]]; then
    echo "credit_focus: minStakeAmount CHANGE after seed — from seed-time $seed_step_min_stake → final $post_seed_min_stake"
    echo "  What changed: max_validators 1→5 (rebalance can target more vals). minStake:"
    if [[ "$USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE" == "true" ]]; then
      echo "    → COMMUNITY_POOL_POST_SEED_MIN_STAKE override ($COMMUNITY_POOL_POST_SEED_MIN_STAKE)."
    elif [[ "${CREDIT_FOCUS_MIN_STAKE_MODE:-computed}" == "max" ]]; then
      echo "    → CREDIT_FOCUS_MIN_STAKE_MODE=max → uint256 max so stake() never consumes a post-credit ledger (observe credits cleanly)."
    else
      echo "    → stays computed ($post_seed_min_stake): still max_move*mult+1; typical credit batches stay below minStake."
    fi
    echo "  Why tweak minStake at all: creditStakeableFromRebalance ignores minStake; stake() does not — high min keeps liquid ledger visible."
  fi
  wait_evm_nonce_settled_for_pk "$POOL_OWNER_PK" 90
  set_pool_contract_config 10 5 "$post_seed_min_stake" || exit 1
  if [[ "$SCENARIO" == "credit_focus" ]]; then
    local verify_ms
    verify_ms="$(pool_call_uint256 "minStakeAmount()(uint256)")"
    if [[ "$verify_ms" != "$post_seed_min_stake" ]]; then
      echo "error: credit_focus setConfig wanted minStakeAmount=$post_seed_min_stake but on-chain read '$verify_ms'" >&2
      echo "hint: COMMUNITY_POOL_POST_SEED_MIN_STAKE may disagree; check cast tx, POOL_OWNER_PK, POOL_EVM_ADDR=$POOL_EVM_ADDR" >&2
      exit 1
    fi
    if [[ "$post_seed_min_stake" == "$COMMUNITY_POOL_UINT256_MAX" ]]; then
      echo "credit_focus_verify: on-chain minStakeAmount=uint256 max (stake() will not consume credited ledger)"
    elif [[ "$USER_SET_COMMUNITY_POOL_POST_SEED_MIN_STAKE" != "true" && "${CREDIT_FOCUS_MIN_STAKE_MODE:-computed}" != "max" ]]; then
      echo "credit_focus_verify: on-chain minStakeAmount=$verify_ms — raise CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER if batched maturities auto-stake"
    else
      echo "credit_focus_verify: on-chain minStakeAmount=$verify_ms (COMMUNITY_POOL_POST_SEED_MIN_STAKE override)"
    fi
  fi
}

compute_expected_credit_from_staking_snapshot() {
  local pending_json="$1"
  local expected=0
  local triples
  triples="$(echo "$pending_json" | jq -r '.undelegations[]? | "\(.validator_address)|\(.completion_time)"' | sort -u)"
  if [[ -z "$triples" ]]; then
    echo "0"
    return 0
  fi
  while IFS='|' read -r val completion; do
    [[ -z "$val" || -z "$completion" ]] && continue
    local ubd sum_for_triple completion_prefix
    completion_prefix="${completion:0:19}"
    ubd="$(evmd query staking unbonding-delegation "$POOL_DEL_ADDR" "$val" --node "$NODE_RPC" -o json 2>/dev/null || echo '{}')"
    sum_for_triple="$(echo "$ubd" | jq -r --arg p "$completion_prefix" '[.entries[]? | select((.completion_time // "")[0:19] == $p) | (.balance // "0" | tonumber)] | add // 0')"
    if [[ "$sum_for_triple" =~ ^[0-9]+$ ]]; then
      expected=$((expected + sum_for_triple))
    fi
  done <<< "$triples"
  echo "$expected"
}

# For watch credit: compare Tendermint latest_block_time to earliest pending undelegation completion (RFC3339).
# Optional $3 = stakeablePrincipalLedger (decimal string) for clearer WAIT text when ledger already credited.
credit_watch_maturity_countdown_line() {
  local bt="$1" ec="$2"
  local ledger="${3:-}"
  if [[ -z "$bt" || -z "$ec" || "$bt" == "n/a" || "$ec" == "n/a" ]]; then
    echo "credit_watch_maturity: (missing block_time or completion_time)"
    return 0
  fi
  if command -v python3 >/dev/null 2>&1; then
    BT_ISO="$bt" EC_ISO="$ec" LEDGER="${ledger:-}" python3 <<'PY'
from datetime import datetime, timezone
import os

def parse_iso(s):
    s = (s or "").strip()
    if not s:
        raise ValueError("empty timestamp")
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    return datetime.fromisoformat(s)

try:
    bt = parse_iso(os.environ.get("BT_ISO", ""))
    ec = parse_iso(os.environ.get("EC_ISO", ""))
    if bt.tzinfo is None:
        bt = bt.replace(tzinfo=timezone.utc)
    if ec.tzinfo is None:
        ec = ec.replace(tzinfo=timezone.utc)
    led = os.environ.get("LEDGER", "").strip()
    liq = int(led) if led.isdigit() else 0
    if bt >= ec:
        print("maturity: READY (undelegation head can be credited to stakeablePrincipalLedger)")
    else:
        sec = (ec - bt).total_seconds()
        if liq > 0:
            print(f"maturity: WAIT ~{sec:.0f}s (ledger may already show earlier credits)")
        else:
            print(f"maturity: WAIT ~{sec:.0f}s (ledger often flat until then)")
except Exception as e:
    print(f"credit_watch_maturity: parse failed ({e})")
PY
    return 0
  fi
  echo "credit_watch_maturity: install python3 for wait estimate"
}

# First-line context for watch / watch credit when the chain is up but setup has not wired the pool yet.
log_watch_pool_delegator_setup_hint() {
  local mode_label="${1:-watch}"
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local rule="──────────────────────────────────────────────────────────────────────────────"
  local params del
  params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || true)"
  del="$(printf '%s' "$params" | jq -r '.params.pool_delegator_address // empty' 2>/dev/null || true)"
  if [[ -n "$del" ]]; then
    return 0
  fi
  printf '%s\n' "$rule"
  echo "$mode_label: pool_delegator_address is not set on-chain yet."
  echo "If another shell is still running this script (default run flow), it may be deploying CommunityPool,"
  echo "configuring automation, and passing governance to set poolrebalancer.params.pool_delegator_address."
  echo "Rough guide: on a typical local devnet, wiring often completes around block heights ~30–32; wall-clock often ~1–3 min."
  echo "Caps: proposal status poll up to ${GOV_STATUS_TIMEOUT}s, then param propagation up to ${GOV_POLL_TIMEOUT}s."
  echo "This watch keeps polling until reads succeed."
  printf '%s\n' "$rule"
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
  CURRENT_PHASE="watch"
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local status_url last_h=""
  status_url="$(tendermint_status_url)"

  while true; do
    local h params del pr pu stakeable total_staked principal_assets reward_reserve caller_raw caller_lc module_lc automation_ready pending_red_json pending_und_json
    h="$(curl -sS --max-time 2 "$status_url" | jq -r '.result.sync_info.latest_block_height // "n/a"')"
    if [[ -z "$h" || "$h" == "n/a" || "$h" == "$last_h" ]]; then
      sleep 1
      continue
    fi
    last_h="$h"
    params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || echo '{}')"
    del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
    if [[ -z "${POOL_EVM_ADDR:-}" && -n "$del" ]]; then
      POOL_EVM_ADDR="$(resolve_evm_hex_from_bech32 "$del")"
      if [[ -n "$POOL_EVM_ADDR" ]]; then
        POOL_DEL_ADDR="$del"
      fi
    fi
    pending_red_json="$(evmd query poolrebalancer pending-redelegations --node "$node" -o json 2>/dev/null || echo '{"redelegations":[]}' )"
    pending_und_json="$(evmd query poolrebalancer pending-undelegations --node "$node" -o json 2>/dev/null || echo '{"undelegations":[]}' )"
    pr="$(echo "$pending_red_json" | jq -r '.redelegations | length // 0')"
    pu="$(echo "$pending_und_json" | jq -r '.undelegations | length // 0')"
    stakeable="n/a"
    total_staked="n/a"
    principal_assets="n/a"
    reward_reserve="n/a"
    caller_raw=""
    automation_ready="no"
    if [[ -n "${POOL_EVM_ADDR:-}" ]]; then
      stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
      total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
      principal_assets="$(pool_call_uint256 "principalAssets()(uint256)")"
      reward_reserve="$(pool_call_uint256 "rewardReserve()(uint256)")"
      caller_raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "automationCaller()(address)" 2>/dev/null || true)"
      caller_lc="$(printf '%s' "$caller_raw" | tr '[:upper:]' '[:lower:]')"
      module_lc="$(printf '%s' "$MODULE_EVM" | tr '[:upper:]' '[:lower:]')"
      if [[ -n "$caller_lc" && -n "$module_lc" && "$caller_lc" == "$module_lc" && -n "$del" && "$del" == "$POOL_DEL_ADDR" ]]; then
        automation_ready="yes"
      fi
    fi

    if [[ "$WATCH_COMPACT" == "true" ]]; then
      echo "watch phase=$CURRENT_PHASE height=$h pending_red=$pr pending_und=$pu stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets reward_reserve=$reward_reserve automation_ready=$automation_ready scenario=$SCENARIO"
    else
      echo "----- rebalance watch -----"
      echo "phase=$CURRENT_PHASE height=$h pending_red=$pr pending_und=$pu stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets reward_reserve=$reward_reserve automation_ready=$automation_ready"
      echo "$params" | jq -r '.params | {pool_delegator_address,max_target_validators,rebalance_threshold_bp,max_ops_per_block,max_move_per_op,use_undelegate_fallback}'

      if [[ -n "$del" ]]; then
        local del_json
        del_json="$(evmd query staking delegations "$del" --node "$node" -o json 2>/dev/null || echo '{"delegation_responses":[]}' )"
        echo "$del_json" | jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
        if [[ "$WATCH_INITIAL_DELEGATIONS_LOGGED" != "true" ]]; then
          local del_count
          del_count="$(echo "$del_json" | jq -r '.delegation_responses | length // 0')"
          if [[ "$del_count" =~ ^[0-9]+$ ]] && (( del_count > 0 )); then
            if [[ "$pr" == "0" && "$pu" == "0" ]]; then
              echo "pre_rebalance_initial_delegations:"
            else
              echo "initial_delegations_first_observed (pending already started):"
            fi
            echo "$del_json" | jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
            WATCH_INITIAL_DELEGATIONS_LOGGED="true"
          fi
        fi
      else
        echo "pool delegator not configured"
      fi
      echo
    fi
  done
}

watch_credit_contract_status() {
  CURRENT_PHASE="watch_credit"
  # visual separator for readability between heights
  local CREDIT_WATCH_RULE="──────────────────────────────────────────────────────────────────────────────"
  # Stale POOL_EVM_ADDR in the shell points cast at the wrong deployment (common minStakeAmount=1 symptom).
  if [[ "${CREDIT_WATCH_USE_ENV_POOL_EVM:-}" != "true" ]]; then
    POOL_EVM_ADDR=""
    POOL_DEL_ADDR=""
  fi
  printf '%s\n' "$CREDIT_WATCH_RULE"
  echo " watch credit  |  pool address from chain (CREDIT_WATCH_USE_ENV_POOL_EVM=true to pin env)"
  printf '%s\n' "$CREDIT_WATCH_RULE"

  local last_h="" baseline_done="false"
  local base_s="" base_ts="" base_pa=""
  local staking_expected_credit="" prev_pu=""
  local prev_earliest_comp=""
  local prev_stakeable=""
  local credit_watch_diag_min1_done="false"
  local credit_watch_pool_logged="false"
  local staking_unbond_param=""
  local status_url node
  status_url="$(tendermint_status_url)"
  node="${NODE_RPC:-tcp://127.0.0.1:26657}"

  while true; do
    local h params del pr pu stakeable total_staked principal_assets pending_und_json pending_red_json
    local status_json bt_iso earliest_comp
    status_json="$(curl -sS --max-time 2 "$status_url")"
    h="$(echo "$status_json" | jq -r '.result.sync_info.latest_block_height // "n/a"')"
    bt_iso="$(echo "$status_json" | jq -r '.result.sync_info.latest_block_time // "n/a"')"
    if [[ -z "$h" || "$h" == "n/a" || "$h" == "$last_h" ]]; then
      sleep 1
      continue
    fi
    last_h="$h"

    params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || echo '{}')"
    del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
    if [[ -z "${POOL_EVM_ADDR:-}" && -n "$del" ]]; then
      POOL_EVM_ADDR="$(resolve_evm_hex_from_bech32 "$del")"
      if [[ -n "$POOL_EVM_ADDR" ]]; then
        POOL_DEL_ADDR="$del"
      fi
    fi
    if [[ "$credit_watch_pool_logged" != "true" && -n "${POOL_EVM_ADDR:-}" && -n "${POOL_DEL_ADDR:-}" ]]; then
      credit_watch_pool_logged="true"
      echo "pool EVM=$POOL_EVM_ADDR delegator=$POOL_DEL_ADDR rpc=$EVM_RPC"
    fi

    pending_red_json="$(evmd query poolrebalancer pending-redelegations --node "$node" -o json 2>/dev/null || echo '{"redelegations":[]}' )"
    pending_und_json="$(evmd query poolrebalancer pending-undelegations --node "$node" -o json 2>/dev/null || echo '{"undelegations":[]}' )"
    pr="$(echo "$pending_red_json" | jq -r '.redelegations | length // 0')"
    pu="$(echo "$pending_und_json" | jq -r '.undelegations | length // 0')"
    earliest_comp="$(echo "$pending_und_json" | jq -r '
      [.undelegations[]? | .completion_time // empty]
      | map(select(type == "string"))
      | sort
      | .[0] // "n/a"
    ')"
    if [[ -z "$staking_unbond_param" ]]; then
      staking_unbond_param="$(evmd query staking params --node "$node" -o json 2>/dev/null | jq -r '.params.unbonding_time // .unbonding_time // "n/a"')"
    fi

    stakeable="n/a"
    total_staked="n/a"
    principal_assets="n/a"
    local min_stake_amt="n/a"
    if [[ -n "${POOL_EVM_ADDR:-}" ]]; then
      stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
      total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
      principal_assets="$(pool_call_uint256 "principalAssets()(uint256)")"
      min_stake_amt="$(pool_call_uint256 "minStakeAmount()(uint256)")"
    fi

    if [[ "$stakeable" =~ ^[0-9]+$ ]]; then
      if [[ -n "$prev_stakeable" && "$prev_stakeable" =~ ^[0-9]+$ ]] && (( stakeable != prev_stakeable )); then
        local st_delta
        st_delta=$(( stakeable - prev_stakeable ))
        printf '%s\n' "$CREDIT_WATCH_RULE"
        echo "*** stakeablePrincipalLedger changed height=$h: $prev_stakeable -> $stakeable (delta $st_delta) ***"
        if (( st_delta > 0 )); then
          echo "    (likely creditStakeableFromRebalance or deposit)"
        elif (( st_delta < 0 )); then
          echo "    (often stake())"
        fi
        printf '%s\n' "$CREDIT_WATCH_RULE"
      fi
      prev_stakeable="$stakeable"
    fi

    if [[ "$baseline_done" != "true" && "$stakeable" =~ ^[0-9]+$ && "$total_staked" =~ ^[0-9]+$ && "$principal_assets" =~ ^[0-9]+$ ]]; then
      base_s="$stakeable"
      base_ts="$total_staked"
      base_pa="$principal_assets"
      baseline_done="true"
      printf '%s\n' "$CREDIT_WATCH_RULE"
      echo " baseline captured height=$h stakeablePrincipalLedger=$base_s totalStaked=$base_ts principalAssets=$base_pa pending_red=$pr pending_und=$pu"
      printf '%s\n' "$CREDIT_WATCH_RULE"
    fi

    if [[ -z "$prev_pu" ]]; then
      prev_pu="$pu"
    fi
    if [[ "$pu" =~ ^[0-9]+$ && "$prev_pu" =~ ^[0-9]+$ ]]; then
      if (( pu > 0 && prev_pu == 0 )); then
        staking_expected_credit="$(compute_expected_credit_from_staking_snapshot "$pending_und_json")"
        echo "expected_credit_wei=$staking_expected_credit (staking unbonding snapshot when pending_und turned on)"
      fi
      if (( pu == 0 && prev_pu > 0 )); then
        local ds dts dpa
        ds="n/a"
        dts="n/a"
        dpa="n/a"
        if [[ "$baseline_done" == "true" && "$stakeable" =~ ^[0-9]+$ && "$total_staked" =~ ^[0-9]+$ && "$principal_assets" =~ ^[0-9]+$ ]]; then
          ds=$(( stakeable - base_s ))
          dts=$(( total_staked - base_ts ))
          dpa=$(( principal_assets - base_pa ))
        fi
        echo "pending_und cleared height=$h stakeablePrincipalLedger=$stakeable totalStaked=$total_staked principalAssets=$principal_assets vs_baseline d_stakeable=$ds d_totalStaked=$dts d_principal=$dpa expected_credit_was=${staking_expected_credit:-n/a}"
        staking_expected_credit=""
      fi
    fi
    if [[ -n "${prev_earliest_comp:-}" && "$prev_earliest_comp" != "n/a" && "$earliest_comp" != "n/a" && "$earliest_comp" != "$prev_earliest_comp" ]]; then
      echo "undelegation queue head advanced pending_und=$pu"
    fi
    prev_pu="$pu"
    if [[ "$pu" =~ ^[0-9]+$ && "$pu" -gt 0 ]]; then
      prev_earliest_comp="$earliest_comp"
    else
      prev_earliest_comp=""
    fi

    local ds2="n/a" dts2="n/a" dpa2="n/a"
    if [[ "$baseline_done" == "true" && "$stakeable" =~ ^[0-9]+$ && "$total_staked" =~ ^[0-9]+$ && "$principal_assets" =~ ^[0-9]+$ ]]; then
      ds2=$(( stakeable - base_s ))
      dts2=$(( total_staked - base_ts ))
      dpa2=$(( principal_assets - base_pa ))
    fi
    local und_compact
    und_compact="$(echo "$pending_und_json" | jq -c '[.undelegations[]? | {v:.validator_address, amt:.balance.amount, den:.balance.denom}]' 2>/dev/null || echo '[]')"

    printf '%s\n' "$CREDIT_WATCH_RULE"
    echo " height $h"
    echo " stakeablePrincipalLedger=$stakeable | totalStaked=$total_staked | principalAssets=$principal_assets | minStake=$min_stake_amt"
    echo " pending_red=$pr | pending_und=$pu | unbonding_time=$staking_unbond_param"
    if [[ "$baseline_done" == "true" && "$ds2" =~ ^-?[0-9]+$ ]]; then
      echo " vs_baseline: delta_stakeable=$ds2  delta_totalStaked=$dts2  delta_principal=$dpa2"
    fi
    if [[ "$pu" =~ ^[0-9]+$ ]] && (( pu > 0 )); then
      echo " undelegations: $und_compact"
      [[ -n "$staking_expected_credit" && "$staking_expected_credit" != "0" ]] && echo " expected_credit_wei=$staking_expected_credit (compare to ledger jump after maturity)"
      credit_watch_maturity_countdown_line "$bt_iso" "$earliest_comp" "$stakeable"
      if [[ "$min_stake_amt" == "1" && "$credit_watch_diag_min1_done" != "true" ]]; then
        credit_watch_diag_min1_done="true"
        local exp_ms
        exp_ms="$(compute_credit_focus_post_seed_min_stake)"
        echo " diag: minStake=1 — wrong pool/config; credit_focus expects ~$exp_ms wei or uint256 max."
      fi
    fi
    printf '%s\n' "$CREDIT_WATCH_RULE"
    echo
    sleep 1
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
  resolve_pool_runtime_keys

}

configure_genesis_params() {
  CURRENT_PHASE="configure_genesis"
  echo "==> Pool delegator mode = $POOL_DELEGATOR_MODE"
  echo "==> SCENARIO=$SCENARIO VALIDATOR_COUNT=$VALIDATOR_COUNT DEMO_PROFILE=$DEMO_PROFILE threshold_bp=$POOLREBALANCER_THRESHOLD_BP max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK"
  echo "==> Patching genesis staking params (unbonding_time + max_entries)"
  patch_genesis_staking_params
  echo "==> Patching genesis poolrebalancer params (pool_delegator_address configured at runtime)"
  patch_genesis_poolrebalancer_params
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
  ensure_evm_rpc_ready
}

seed_initial_imbalance() {
  CURRENT_PHASE="seed_initial_imbalance"
  if [[ "$POOL_DELEGATOR_MODE" == "contract" ]]; then
    seed_contract_imbalance
    return 0
  fi
  echo "error: unsupported POOL_DELEGATOR_MODE=$POOL_DELEGATOR_MODE" >&2
  exit 1
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
  if [[ "$POOL_DELEGATOR_MODE" == "contract" && "$del_count" == "0" ]]; then
    echo "error: contract delegator has zero delegations after seeding; rebalance cannot run" >&2
    exit 1
  fi

  if [[ "$SCENARIO" == "expansion" ]]; then
    if (( bonded_count < 5 )); then
      echo "error: expansion expects at least 5 bonded validators, got $bonded_count (use --nodes 5)" >&2
      exit 1
    fi

    if (( ${#EXPANSION_INITIAL_DELEGATED[@]} != 3 )); then
      echo "error: expansion seed did not produce 3 initial delegations (got ${#EXPANSION_INITIAL_DELEGATED[@]})" >&2
      exit 1
    fi

    local bonded_json
    bonded_json="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -c '[.validators[] | select(.status=="BOND_STATUS_BONDED") | .operator_address] | unique')"
    local seeded_json
    seeded_json="$(printf '%s\n' "${EXPANSION_INITIAL_DELEGATED[@]}" | jq -R . | jq -s -c 'unique')"
    EXPANSION_MISSING_DSTS=()
    while IFS= read -r val; do
      [[ -z "$val" ]] && continue
      EXPANSION_MISSING_DSTS+=("$val")
    done < <(jq -n -r --argjson bonded "$bonded_json" --argjson delegated "$seeded_json" '($bonded - $delegated)[]')

    if (( ${#EXPANSION_MISSING_DSTS[@]} < 2 )); then
      echo "error: expansion expects at least 2 bonded validators outside the initial pool delegation set (got ${#EXPANSION_MISSING_DSTS[@]})" >&2
      exit 1
    fi

    EXPANSION_OBSERVED_DSTS_TEXT=""
    echo "scenario_check expansion: bonded=$bonded_count initial_seeded=${#EXPANSION_INITIAL_DELEGATED[@]} extra_targets=${#EXPANSION_MISSING_DSTS[@]}"
  fi

  if [[ "$SCENARIO" == "credit_focus" && "$POOL_DELEGATOR_MODE" == "contract" ]]; then
    local ms_hex evm_hex
    evm_hex="${POOL_EVM_ADDR:-}"
    if [[ -z "$evm_hex" ]]; then
      evm_hex="$(resolve_evm_hex_from_bech32 "$POOL_DEL_ADDR")"
    fi
    if [[ -n "$evm_hex" ]]; then
      POOL_EVM_ADDR="$evm_hex"
      ms_hex="$(pool_call_uint256 "minStakeAmount()(uint256)")"
      echo "credit_focus_observability: CommunityPool minStakeAmount=$ms_hex"
      if [[ "$ms_hex" == "1" || "$ms_hex" == "0" ]]; then
        echo "error: credit_focus expects minStakeAmount > 1 after final setConfig (got $ms_hex). This matches intermediate seed-only config — unset COMMUNITY_POOL_POST_SEED_MIN_STAKE, stop nodes, re-run:  $0 --scenario credit_focus --nodes \$N" >&2
        exit 1
      fi
      if [[ "$ms_hex" == "$COMMUNITY_POOL_UINT256_MAX" ]]; then
        echo "  => min stake is uint256 max: EndBlock stake() should no-op; watch credit should show stakeablePrincipalLedger jump after unbonding matures."
      elif [[ "$ms_hex" =~ ^[0-9]+$ ]] && (( ${#ms_hex} >= 70 )); then
        echo "  => min stake is huge: EndBlock stake() should no-op."
      elif [[ "$ms_hex" =~ ^[0-9]+$ ]]; then
        echo "  => credit_focus minStake=$ms_hex: stake() only when stakeablePrincipalLedger >= minStake; raise CREDIT_FOCUS_MIN_STAKE_MOVE_MULTIPLIER if credits auto-restake."
      fi
    fi
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
        if ! printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | grep -Fxq "$dst" 2>/dev/null; then
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
    if printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | grep -Fxq "$target" 2>/dev/null; then
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
    height="$(curl -sS --max-time 2 "$(tendermint_status_url)" | jq -r '.result.sync_info.latest_block_height')"
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
    elif [[ "$SCENARIO" == "credit_focus" ]]; then
      if (( pending > 0 )); then
        FALLBACK_SEEN_REDELEGATION="true"
      fi
      echo "credit_focus: pending_red=$pending pending_und=$pendingUndel unbonding=$STAKING_UNBONDING_TIME"
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
      if [[ "$SCENARIO" == "credit_focus" ]]; then
        echo "hint: other shell — SCENARIO=credit_focus $0 watch credit"
      fi
      while true; do
        local monitorHeight monitorRed monitorUnd
        monitorHeight="$(curl -sS --max-time 2 "$(tendermint_status_url)" | jq -r '.result.sync_info.latest_block_height')"
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
      if [[ "$USER_SET_POOL_SEED_DEPOSIT_AMOUNT" != "true" ]]; then POOL_SEED_DEPOSIT_AMOUNT=500000000000000000000; fi
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
    credit_focus)
      # Undelegation-heavy profile: fallback on, tiny staking entry cap, short unbonding, small per-op moves
      # so redelegation slots fill fast and most work is undelegate → mature → creditStakeableFromRebalance.
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_USE_UNDELEGATE_FALLBACK" != "true" ]]; then POOLREBALANCER_USE_UNDELEGATE_FALLBACK=true; fi
      if [[ "$USER_SET_STAKING_MAX_ENTRIES" != "true" ]]; then STAKING_MAX_ENTRIES=1; fi
      if [[ "$USER_SET_STAKING_UNBONDING_TIME" != "true" ]]; then STAKING_UNBONDING_TIME=15s; fi
      if [[ "$USER_SET_THRESHOLD_BP" != "true" ]]; then POOLREBALANCER_THRESHOLD_BP=0; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=500000000000000000; fi
      if [[ "$USER_SET_POLL_SAMPLES" != "true" ]]; then POLL_SAMPLES=90; fi
      if [[ "$USER_SET_POLL_SLEEP_SECS" != "true" ]]; then POLL_SLEEP_SECS=2; fi
      ;;
    expansion)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=5; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
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
      echo "expected: happy_path|caps|threshold_boundary|fallback|expansion|credit_focus" >&2
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
    require_bin jq
    require_bin curl
    require_bin evmd
    # Match main() tuning so compute_credit_focus_post_seed_min_stake / hints align with seeded chains.
    apply_scenario_defaults
    case "${DEMO_PROFILE:-medium}" in
      slow)
        POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-1}"
        POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-10000000000000000000}"
        ;;
      medium) ;;
      fast)
        POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-10}"
        POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-0}"
        ;;
      *)
        echo "invalid DEMO_PROFILE: $DEMO_PROFILE (expected: slow|medium|fast)" >&2
        exit 1
        ;;
    esac
    local watch_mode_label="watch"
    if [[ "$WATCH_CREDIT_MODE" == "true" ]]; then
      watch_mode_label="watch credit"
    fi
    log_watch_pool_delegator_setup_hint "$watch_mode_label"
    if [[ "$WATCH_CREDIT_MODE" == "true" ]]; then
      require_bin cast
      ensure_evm_rpc_ready
      watch_credit_contract_status
    else
      watch_rebalance_status
    fi
    exit 0
  fi
  if [[ "$PARSED_SUBCOMMAND" == "help" ]]; then
    usage
    exit 0
  fi

  require_bin jq
  require_bin curl
  require_bin evmd
  require_bin cast

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
  # 2) contract deployment + runtime param wiring
  # 3) readiness and scenario seeding
  # 4) sanity checks and scenario-specific observers
  resolve_mnemonics
  setup_localnet
  configure_genesis_params
  start_validators
  wait_chain_ready
  configure_contract_pool_delegator
  verify_contract_pool_readiness
  seed_initial_imbalance
  run_sanity_checks
  observe_and_monitor
}

main "$@"

