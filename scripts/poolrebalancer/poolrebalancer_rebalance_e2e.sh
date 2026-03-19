#!/usr/bin/env bash
set -euo pipefail

# -----------------------------------------------------------------------------
# poolrebalancer_rebalance_e2e.sh
#
# Purpose:
#   Manual local E2E test for x/poolrebalancer behavior on a 3-validator devnet.
#
# Scope:
#   - Local engineer workflow / debugging aid.
#   - Not intended as a deterministic CI test harness.
#
# Prerequisites:
#   - jq, curl, evmd installed and on PATH.
#   - VAL0_MNEMONIC / VAL1_MNEMONIC / VAL2_MNEMONIC exported.
#
# Quick start:
#   bash scripts/poolrebalancer/poolrebalancer_rebalance_e2e.sh
#
# Live monitor only:
#   bash scripts/poolrebalancer/poolrebalancer_rebalance_e2e.sh watch
# -----------------------------------------------------------------------------

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-"tcp://127.0.0.1:26657"}"
CHAIN_ID="${CHAIN_ID:-10740}"
KEYRING="${KEYRING:-test}"
HOME0="$BASEDIR/val0"

# ---- Test knobs (override via env) ----
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

# Staking params tuned so maturity/fallback behavior is visible quickly in local runs.
STAKING_UNBONDING_TIME="${STAKING_UNBONDING_TIME:-30s}"
STAKING_MAX_ENTRIES="${STAKING_MAX_ENTRIES:-100}"

TX_FEES="${TX_FEES:-200000000000000ogwei}" # denom will be rewritten after chain start

# Delegation amounts used to create a clear imbalance (safe with default dev funding).
IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION:-200000000000000000000000ogwei}" # denom rewritten after chain start
IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION:-100ogwei}"

POLL_SAMPLES="${POLL_SAMPLES:-25}"
POLL_SLEEP_SECS="${POLL_SLEEP_SECS:-2}"
STREAM_VALIDATOR_LOGS="${STREAM_VALIDATOR_LOGS:-true}"
KEEP_RUNNING="${KEEP_RUNNING:-true}"

LOG_STREAM_PIDS=()

usage() {
  cat <<EOF
Usage: $0 [watch]

Runs a local E2E sanity test for x/poolrebalancer:
- Generates a 3-validator localnet using multi_node_startup.sh
- Patches genesis to enable poolrebalancer for dev0
- Starts val0/val1/val2
- Delegates an imbalanced stake distribution from dev0
- Verifies poolrebalancer schedules pending redelegations capped by max_move_per_op

Subcommands:
  watch                             Live monitor (height/params/pending/delegations) without restarting localnet

Environment variables:
  BASEDIR                           Localnet base dir (default: $HOME/.og-evm-devnet)
  NODE_RPC                          RPC endpoint (default: tcp://127.0.0.1:26657)
  CHAIN_ID                          Chain ID (default: 10740)
  TX_FEES                           Fees for txs (default: $TX_FEES)

  VAL0_MNEMONIC / VAL1_MNEMONIC / VAL2_MNEMONIC
                                   Required for genesis generation. Provide 24-word BIP39 mnemonics.

  POOLREBALANCER_MAX_TARGET_VALIDATORS
  DEMO_PROFILE                      slow|medium|fast tuning for rebalance visibility (default: medium)
  POOLREBALANCER_THRESHOLD_BP
  POOLREBALANCER_MAX_OPS_PER_BLOCK
  POOLREBALANCER_MAX_MOVE_PER_OP
  POOLREBALANCER_USE_UNDELEGATE_FALLBACK

  STAKING_UNBONDING_TIME            Reduce so pending queues mature quickly (default: 30s)
  STAKING_MAX_ENTRIES             Raise to allow more in-flight undelegations (default: 100)

  IMBALANCE_MAIN_DELEGATION         Large delegation to validator[0]
  IMBALANCE_MINOR_DELEGATION        Small delegations to validator[1], validator[2]
  STREAM_VALIDATOR_LOGS             Stream val0/val1/val2 logs to stdout (default: false)
  KEEP_RUNNING                      Keep monitoring after PASS (default: false)

EOF
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
  for v in 0 1 2; do
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
  cp "$gen0" "$BASEDIR/val1/config/genesis.json"
  cp "$gen0" "$BASEDIR/val2/config/genesis.json"

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
  # Some CLI builds return txhash even on CheckTx failure, so always inspect `.code`.
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

assert_pending_invariants() {
  local json="$1"
  local cap="$2"
  local max_ops="$3"

  # Important nuance:
  # pending-redelegations query returns primary records that can merge multiple ops
  # sharing (delegator, denom, dst, completionTime). With max_ops_per_block > 1,
  # a merged record amount can exceed max_move_per_op even if each individual op respected the cap.
  # So strict cap assertion is only sound when max_ops_per_block == 1.
  if [[ "$cap" != "0" && "$max_ops" == "1" ]]; then
    local badAmt
    badAmt="$(echo "$json" | jq -r --argjson cap "$cap" '[.redelegations[] | (.amount.amount|tonumber) > $cap] | any')"
    if [[ "$badAmt" != "false" ]]; then
      echo "FAIL: found pending amount > max_move_per_op" >&2
      return 1
    fi
  elif [[ "$cap" != "0" && "$max_ops" != "1" ]]; then
    echo "note: skipping strict max_move_per_op check on merged primary entries (max_ops_per_block=$max_ops)"
  fi

  # Transitive safety: no source validator should also appear as destination in-flight.
  local badTrans
  badTrans="$(echo "$json" | jq -r '([.redelegations[].src_validator_address] | unique) as $srcs | ([.redelegations[].dst_validator_address] | unique) as $dsts | ([ $srcs[] | . as $s | (($dsts | index($s)) != null) ] | any)')"
  if [[ "$badTrans" != "false" ]]; then
    echo "FAIL: transitive safety violated (src appears in dst set)" >&2
    return 1
  fi
}

watch_rebalance_status() {
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local interval="${POLL_SLEEP_SECS:-2}"

  while true; do
    local h params del pr pu
    h="$(curl -sS http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height // "n/a"')"
    params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || echo '{}')"
    del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
    pr="$(evmd query poolrebalancer pending-redelegations --node "$node" -o json 2>/dev/null | jq -r '.redelegations | length // 0')"
    pu="$(evmd query poolrebalancer pending-undelegations --node "$node" -o json 2>/dev/null | jq -r '.undelegations | length // 0')"

    echo "----- rebalance watch -----"
    echo "height=$h pending_red=$pr pending_und=$pu"
    echo "$params" | jq -r '.params | {pool_delegator_address,max_target_validators,rebalance_threshold_bp,max_ops_per_block,max_move_per_op,use_undelegate_fallback}'

    if [[ -n "$del" ]]; then
      evmd query staking delegations "$del" --node "$node" -o json 2>/dev/null | \
        jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
    else
      echo "pool delegator not configured"
    fi
    echo
    sleep "$interval"
  done
}

main() {
  if [[ "${1:-}" == "watch" ]]; then
    watch_rebalance_status
    exit 0
  fi

  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage; exit 0
  fi

  require_bin jq
  require_bin curl
  require_bin evmd

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

  if [[ -z "${VAL0_MNEMONIC:-}" || -z "${VAL1_MNEMONIC:-}" || -z "${VAL2_MNEMONIC:-}" ]]; then
    echo "VAL0_MNEMONIC/VAL1_MNEMONIC/VAL2_MNEMONIC must be set" >&2
    exit 1
  fi

  echo "==> Stopping any existing localnet"
  stop_nodes
  trap cleanup_log_streams EXIT

  echo "==> Generating genesis (3 validators) at $BASEDIR"
  # multi_node_startup.sh is verbose during init; silence setup noise here.
  (cd "$ROOT_DIR" && GENERATE_GENESIS=true ./multi_node_startup.sh -y >/dev/null 2>&1)

  local del_addr
  del_addr="$(dev0_address_from_file)"
  local del_mnemonic
  del_mnemonic="$(dev0_mnemonic_from_file)"

  echo "==> Pool delegator (dev0) = $del_addr"
  echo "==> DEMO_PROFILE=$DEMO_PROFILE threshold_bp=$POOLREBALANCER_THRESHOLD_BP max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP fallback=$POOLREBALANCER_USE_UNDELEGATE_FALLBACK"
  echo "==> Patching genesis staking params (unbonding_time + max_entries)"
  patch_genesis_staking_params
  echo "==> Patching genesis poolrebalancer params"
  patch_genesis_poolrebalancer_params "$del_addr"

  echo "==> Starting validators"
  mkdir -p "$BASEDIR/logs"
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=0 ./multi_node_startup.sh >"$BASEDIR/logs/val0.log" 2>&1 &)
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=1 ./multi_node_startup.sh >"$BASEDIR/logs/val1.log" 2>&1 &)
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=2 ./multi_node_startup.sh >"$BASEDIR/logs/val2.log" 2>&1 &)
  if [[ "$STREAM_VALIDATOR_LOGS" == "true" ]]; then
    echo "==> Streaming validator logs (val0/val1/val2)"
    start_validator_log_streams
  fi

  echo "==> Waiting for block production"
  local h
  h="$(wait_for_height 60)"
  echo "height=$h"

  # Resolve chain bond denom and rewrite amount knobs to match this network.
  local bond_denom
  bond_denom="$(evmd query staking params --node "$NODE_RPC" -o json | jq -r '.params.bond_denom // .bond_denom')"
  if [[ -z "$bond_denom" || "$bond_denom" == "null" ]]; then
    echo "FAIL: could not determine bond_denom from staking params" >&2
    exit 1
  fi
  echo "bond_denom=$bond_denom"
  TX_FEES="${TX_FEES%ogwei}${bond_denom}"
  IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION%ogwei}${bond_denom}"
  IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION%ogwei}${bond_denom}"

  echo "==> Importing dev0 key into keyring"
  import_dev0_key "$del_mnemonic"

  echo "==> Creating delegation imbalance"
  local vals v0 v1 v2
  vals="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '.validators[:3] | .[] | .operator_address')"
  v0="$(echo "$vals" | sed -n '1p')"
  v1="$(echo "$vals" | sed -n '2p')"
  v2="$(echo "$vals" | sed -n '3p')"

  delegate_with_wait "$v0" "$IMBALANCE_MAIN_DELEGATION"
  delegate_with_wait "$v1" "$IMBALANCE_MINOR_DELEGATION"
  delegate_with_wait "$v2" "$IMBALANCE_MINOR_DELEGATION"

  echo "==> Sanity checks (params + delegations)"
  local onchain_del
  onchain_del="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address')"
  if [[ "$onchain_del" != "$del_addr" ]]; then
    echo "FAIL: poolrebalancer params.pool_delegator_address mismatch" >&2
    echo "  expected: $del_addr" >&2
    echo "  got:      $onchain_del" >&2
    exit 1
  fi

  local del_count
  del_count="$(evmd query staking delegations "$del_addr" --node "$NODE_RPC" -o json | jq -r '.delegation_responses | length')"
  echo "delegations_count=$del_count"

  local bonded_count
  bonded_count="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '[.validators[] | select(.status=="BOND_STATUS_BONDED")] | length')"
  echo "bonded_validators=$bonded_count"
  if (( bonded_count == 0 )); then
    echo "FAIL: no bonded validators found; cannot rebalance" >&2
    exit 1
  fi

  echo "==> Observing pending redelegations"
  for i in $(seq 1 "$POLL_SAMPLES"); do
    local height pending
    height="$(curl -s http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height')"
    local j
    j="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json)"
    pending="$(echo "$j" | jq -r '.redelegations | length')"
    echo "sample=$i height=$height pending_redelegations=$pending"

    if (( pending > 0 )); then
      assert_pending_invariants "$j" "$POOLREBALANCER_MAX_MOVE_PER_OP" "$POOLREBALANCER_MAX_OPS_PER_BLOCK"
      local pendingUndel
      pendingUndel="$(evmd query poolrebalancer pending-undelegations --node "$NODE_RPC" -o json | jq -r '.undelegations | length')"
      echo "pending_undelegations=$pendingUndel"
      echo "PASS: pending redelegations observed and invariants hold"
      if [[ "$KEEP_RUNNING" != "true" ]]; then
        exit 0
      fi
      echo "==> KEEP_RUNNING=true, continuing in monitor mode (Ctrl+C to stop)"
      while true; do
        local monitorHeight monitorRed monitorUnd
        monitorHeight="$(curl -sS http://127.0.0.1:26657/status | jq -r '.result.sync_info.latest_block_height')"
        monitorRed="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json | jq -r '.redelegations | length')"
        monitorUnd="$(evmd query poolrebalancer pending-undelegations --node "$NODE_RPC" -o json | jq -r '.undelegations | length')"
        echo "monitor height=$monitorHeight pending_red=$monitorRed pending_und=$monitorUnd"
        sleep "$POLL_SLEEP_SECS"
      done
    fi
    sleep "$POLL_SLEEP_SECS"
  done

  echo "FAIL: did not observe any pending redelegations within polling window" >&2
  echo "Diagnostics:" >&2
  evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq '.' >&2 || true
  evmd query staking validators --node "$NODE_RPC" -o json | jq -r '[.validators[] | {op:.operator_address,status:.status}]' >&2 || true
  exit 1
}

main "$@"

