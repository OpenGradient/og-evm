#!/usr/bin/env bash
#
# CommunityPool + poolrebalancer EndBlock smoke test (local devnet).
#
# Flow: local_node.sh → deploy CommunityPool → set automation caller → gov sets
# pool_delegator_address → one dev1 deposit → assert EndBlock stakes it → watch (metrics +
# periodic auto-deposit every WATCH_AUTO_DEPOSIT_EVERY_BLOCKS once automation is ready).
# Params are set via governance (not genesis) so the contract exists before automation runs.
#
# Usage: ./pool_automation_local_setup.sh [run|watch|help]
#   run   — full flow + watch
#   watch — metrics each new block; auto-deposit on interval when automationReady (see env)
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# --- config (override with env only when needed) ---
CHAIN_HOME="${CHAIN_HOME:-$HOME/.og-evm-devnet}"
NODE="${NODE:-tcp://127.0.0.1:26657}"
RPC="${RPC:-http://127.0.0.1:8545}"
CHAIN_ID="${CHAIN_ID:-10740}"

PK_DEV0="${PK_DEV0:-0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305}" # gitleaks:allow
PK_DEV1="${PK_DEV1:-0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544}" # gitleaks:allow

MODULE_EVM="${MODULE_EVM:-0x786c305E2aAc2168BB7555Ef522c5F20a2cd0dA9}"
BOND_PRECOMPILE="${BOND_PRECOMPILE:-0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE}"

GOV_WAIT_INITIAL="${GOV_WAIT_INITIAL:-32}"
GOV_POLL_TIMEOUT="${GOV_POLL_TIMEOUT:-10}"

# Prove-step deposit during `run` (ogwei).
DEPOSIT_AMOUNT="${DEPOSIT_AMOUNT:-1000000000000}"

# Watch: while automationReady, deposit this many ogwei every N new blocks (0 = disable).
WATCH_AUTO_DEPOSIT_EVERY_BLOCKS="${WATCH_AUTO_DEPOSIT_EVERY_BLOCKS:-10}"
WATCH_AUTO_DEPOSIT_AMOUNT="${WATCH_AUTO_DEPOSIT_AMOUNT:-1000000000000}"

STATE_FILE="${STATE_FILE:-/tmp/pool_automation_state.env}"
CHAIN_LOG_FILE="${CHAIN_LOG_FILE:-/tmp/pool_automation_local_node.log}"

STARTED_BY_SCRIPT=false
CHAIN_LOG_TAIL_PID=""
CLEANUP_RAN=false
POOL_ADDR="${POOL_ADDR:-}"
POOL_BECH32="${POOL_BECH32:-}"

require_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

log() { echo "[$(date '+%H:%M:%S')] ==> $*"; }
log_detail() { echo "[$(date '+%H:%M:%S')]     $*"; }

normalize_cast_uint256_output() {
  local s="${1:-}"
  s="${s//$'\r'/}"
  s="${s%%$'\n'*}"
  s="${s%% *}"
  s="${s//$'\t'/}"
  [[ "$s" =~ ^[0-9]+$ ]] && { printf '%s' "$s"; return 0; }
  return 1
}

pool_call_uint256() {
  local sig="$1" raw norm
  [[ -z "${POOL_ADDR:-}" ]] && { printf 'n/a'; return; }
  raw="$(cast call --rpc-url "$RPC" "$POOL_ADDR" "$sig" 2>/dev/null || true)"
  if norm="$(normalize_cast_uint256_output "$raw")"; then
    printf '%s' "$norm"
  else
    printf 'n/a'
  fi
}

wait_evm_nonce_settled_for_pk() {
  local pk="$1" deadline_sec="${2:-45}"
  local addr pending latest t0
  addr="$(cast wallet address --private-key "$pk")"
  t0="$(date +%s)"
  while true; do
    pending="$(cast nonce --rpc-url "$RPC" --block pending "$addr" 2>/dev/null || true)"
    latest="$(cast nonce --rpc-url "$RPC" --block latest "$addr" 2>/dev/null || true)"
    [[ -z "$pending" || -z "$latest" ]] && return 0
    [[ "$pending" == "$latest" ]] && return 0
    if (( $(date +%s) - t0 > deadline_sec )); then
      log_detail "evm nonce settle timeout (proceeding)"
      return 0
    fi
    sleep 1
  done
}

_bumped_gas_price() {
  local gp gp2
  gp="$(cast gas-price --rpc-url "$RPC" 2>/dev/null || echo 1000000)"
  gp2="$(awk -v g="$gp" 'BEGIN { print int(g) * 2 }' 2>/dev/null || true)"
  [[ -z "$gp2" || "$gp2" == 0 ]] && gp2="$gp"
  printf '%s' "$gp2"
}

# One approve + one deposit from PK_DEV1 (prove_endblock_stake + watch_loop auto-deposit).
dev1_deposit_once() {
  local amount="$1" approve_json="$2" deposit_json="$3"
  local errf gp2
  wait_evm_nonce_settled_for_pk "$PK_DEV1" 45
  errf="$(mktemp -t pool_dep.XXXXXX)"
  if cast send --json --rpc-url "$RPC" --private-key "$PK_DEV1" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$RPC" --private-key "$PK_DEV1" "$POOL_ADDR" \
    "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"; then
    rm -f "$errf"
    return 0
  fi
  log_detail "deposit failed, retry with bumped gas: $(tr '\n' ' ' <"$errf" | head -c 200)"
  gp2="$(_bumped_gas_price)"
  cast send --json --rpc-url "$RPC" --private-key "$PK_DEV1" --gas-price "$gp2" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$RPC" --private-key "$PK_DEV1" --gas-price "$gp2" "$POOL_ADDR" \
    "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"
  local st=$?
  rm -f "$errf"
  return "$st"
}

stop_existing_local_node() {
  local pids
  pids="$(lsof -nP -iTCP:26657 -sTCP:LISTEN 2>/dev/null | awk 'NR>1 {print $2}' || true)"
  if [[ -n "$pids" ]]; then
    log "stopping existing local node process(es): $pids"
    # shellcheck disable=SC2086
    kill $pids || true
    sleep 2
  fi
}

cleanup() {
  [[ "$CLEANUP_RAN" == "true" ]] && return 0
  CLEANUP_RAN=true
  [[ -n "$CHAIN_LOG_TAIL_PID" ]] && kill "$CHAIN_LOG_TAIL_PID" >/dev/null 2>&1 || true
  if [[ "$STARTED_BY_SCRIPT" == "true" ]]; then
    log "stopping local chain started by this script"
    stop_existing_local_node
  fi
}

stop_script_on_signal() {
  cleanup
  exit 130
}

cosmos_query_node() {
  local n="${NODE:-tcp://127.0.0.1:26657}"
  case "$n" in
    tcp://*) printf '%s' "http://${n#tcp://}" ;;
    http://*|https://*) printf '%s' "$n" ;;
    *) printf '%s' "http://$n" ;;
  esac
}

comet_status_url() { printf '%s/status' "$(cosmos_query_node)"; }

wait_for_chain() {
  local timeout_secs="${1:-60}" start h
  start="$(date +%s)"
  log "waiting for chain (timeout ${timeout_secs}s)"
  while true; do
    h="$(curl -sS --max-time 1 "$(comet_status_url)" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' || echo "0")"
    [[ "$h" != "0" ]] && { log "chain live height=$h"; return 0; }
    if (( $(date +%s) - start > timeout_secs )); then
      echo "timed out waiting for chain" >&2
      return 1
    fi
    sleep 1
  done
}

start_chain_log_stream() {
  touch "$CHAIN_LOG_FILE"
  tail -n 0 -F "$CHAIN_LOG_FILE" | sed -u 's/^/[chain] /' &
  CHAIN_LOG_TAIL_PID=$!
}

evmd_debug_addr() {
  local addr="$1"
  if [[ -n "${CHAIN_HOME:-}" && -d "$CHAIN_HOME" ]]; then
    evmd debug addr "$addr" --home "$CHAIN_HOME" 2>/dev/null || evmd debug addr "$addr" 2>/dev/null || true
  else
    evmd debug addr "$addr" 2>/dev/null || true
  fi
}

start_local_node() {
  stop_existing_local_node
  : >"$CHAIN_LOG_FILE"
  log "starting local_node.sh"
  pushd "$ROOT_DIR" >/dev/null
  ./local_node.sh -y >"$CHAIN_LOG_FILE" 2>&1 &
  popd >/dev/null
  STARTED_BY_SCRIPT=true
  start_chain_log_stream
  wait_for_chain 120
}

pool_addr_from_cast_deploy_output() {
  local raw="$1" addr line txh
  [[ -z "$raw" ]] && return 1
  if addr="$(printf '%s' "$raw" | jq -r '.contractAddress // .receipt.contractAddress // empty' 2>/dev/null)" &&
    [[ -n "$addr" && "$addr" != "null" ]]; then
    printf '%s' "$addr"
    return 0
  fi
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*\{ ]] || continue
    if addr="$(printf '%s' "$line" | jq -r '.contractAddress // .receipt.contractAddress // empty' 2>/dev/null)" &&
      [[ -n "$addr" && "$addr" != "null" ]]; then
      printf '%s' "$addr"
      return 0
    fi
  done <<< "$raw"
  if [[ "$raw" =~ \"contractAddress\"[[:space:]]*:[[:space:]]*\"(0x[0-9a-fA-F]{40})\" ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  txh="$(printf '%s' "$raw" | tr -d '[:space:]')"
  if [[ ${#txh} -eq 66 && "$txh" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    addr="$(cast receipt "$txh" --rpc-url "$RPC" contractAddress 2>/dev/null || true)"
    addr="$(printf '%s' "$addr" | tr -d '[:space:]')"
    if [[ -n "$addr" && "$addr" != "null" && "$addr" != "0x0000000000000000000000000000000000000000" ]]; then
      printf '%s' "$addr"
      return 0
    fi
  fi
  return 1
}

deploy_pool() {
  log "deploying CommunityPool"
  local owner bytecode ctor_args data deploy_out deploy_err deploy_raw
  owner="$(cast wallet address --private-key "$PK_DEV0")"
  bytecode="$(jq -r '.bytecode // empty' "$ROOT_DIR/contracts/solidity/pool/CommunityPool.json" 2>/dev/null || true)"
  if [[ -z "$bytecode" || "$bytecode" == "null" ]]; then
    echo "missing bytecode: compile pool and refresh CommunityPool.json" >&2
    exit 1
  fi
  ctor_args="$(cast abi-encode "constructor(address,uint32,uint32,uint256,address)" "$BOND_PRECOMPILE" 10 5 1 "$owner")"
  data="${bytecode}${ctor_args#0x}"
  deploy_out="$(mktemp -t pool_deploy_out.XXXXXX)"
  deploy_err="$(mktemp -t pool_deploy_err.XXXXXX)"
  if ! cast send --json --rpc-url "$RPC" --private-key "$PK_DEV0" --create "$data" >"$deploy_out" 2>"$deploy_err"; then
    log_detail "deploy retry after: $(tr '\n' ' ' <"$deploy_err" | head -c 300)"
    sleep 2
    cast send --json --rpc-url "$RPC" --private-key "$PK_DEV0" --create "$data" >"$deploy_out" 2>"$deploy_err" || {
      cat "$deploy_err" >&2
      rm -f "$deploy_out" "$deploy_err"
      exit 1
    }
  fi
  deploy_raw="$(cat "$deploy_out")"
  rm -f "$deploy_out" "$deploy_err"
  if ! POOL_ADDR="$(pool_addr_from_cast_deploy_output "$deploy_raw")" || [[ -z "$POOL_ADDR" ]]; then
    echo "could not parse contract address from deploy output" >&2
    exit 1
  fi
  POOL_BECH32="$(evmd_debug_addr "$POOL_ADDR" | rg 'Bech32 Acc' | awk '{print $3}')"
  log "pool $POOL_ADDR / $POOL_BECH32"
}

configure_automation() {
  log "configure automation (caller + gov)"
  cast send --json --rpc-url "$RPC" --private-key "$PK_DEV0" "$POOL_ADDR" \
    "setAutomationCaller(address)" "$MODULE_EVM" >/tmp/pool_set_automation.json
  local gov_auth current proposal_json
  gov_auth="$(evmd query auth module-account gov --node "$NODE" -o json | jq -r '.account.value.address')"
  current="$(evmd query poolrebalancer params --node "$NODE" -o json)"
  proposal_json="$(echo "$current" | jq --arg gov "$gov_auth" --arg del "$POOL_BECH32" '{
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
    deposit:"10000000ogwei",
    title:"Set pool delegator for automation",
    summary:"Set CommunityPool account for EndBlock automation.",
    expedited:false
  }')"
  evmd tx gov submit-proposal <(echo "$proposal_json") \
    --from mykey --keyring-backend test --home "$CHAIN_HOME" \
    --chain-id "$CHAIN_ID" --node "$NODE" \
    --fees 200000000000000ogwei --gas auto --gas-adjustment 1.5 \
    -y -o json >/tmp/pool_gov_submit.json
  for _ in 1 2 3; do
    evmd tx gov vote 1 yes \
      --from mykey --keyring-backend test --home "$CHAIN_HOME" \
      --chain-id "$CHAIN_ID" --node "$NODE" \
      --fees 200000000000000ogwei --gas auto --gas-adjustment 1.3 \
      -y -o json >/tmp/pool_gov_vote.json 2>/dev/null && break
    sleep 2
  done
  log "waiting gov (${GOV_WAIT_INITIAL}s)"
  sleep "$GOV_WAIT_INITIAL"
  local status
  status="$(evmd query gov proposal 1 --node "$NODE" -o json | jq -r '.proposal.status')"
  [[ "$status" == "PROPOSAL_STATUS_PASSED" ]] || {
    echo "gov proposal not passed: $status" >&2
    exit 1
  }
  local t0 elapsed current_addr
  t0="$(date +%s)"
  while true; do
    current_addr="$(evmd query poolrebalancer params --node "$NODE" -o json 2>/dev/null | jq -r '.params.pool_delegator_address // ""')"
    [[ "$current_addr" == "$POOL_BECH32" ]] && break
    elapsed="$(($(date +%s) - t0))"
    if [[ "$elapsed" -gt "$GOV_POLL_TIMEOUT" ]]; then
      echo "param not propagated (have: $current_addr want: $POOL_BECH32)" >&2
      exit 1
    fi
    sleep 2
  done
  log "pool_delegator_address set"
}

# Exactly one dev1 deposit; EndBlock should stake it.
prove_endblock_stake() {
  log "single deposit test (${DEPOSIT_AMOUNT} ogwei) + wait for stake"
  local before_stakeable before_total after_stakeable after_total timeout elapsed
  before_stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
  before_total="$(pool_call_uint256 "totalStaked()(uint256)")"
  [[ "$before_stakeable" != "n/a" && "$before_total" != "n/a" ]] || {
    echo "cannot read pool over RPC" >&2
    exit 1
  }
  dev1_deposit_once "$DEPOSIT_AMOUNT" /tmp/pool_approve.json /tmp/pool_deposit.json || {
    echo "deposit failed" >&2
    exit 1
  }
  timeout=30
  elapsed=0
  while [[ $elapsed -lt $timeout ]]; do
    after_stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
    after_total="$(pool_call_uint256 "totalStaked()(uint256)")"
    if [[ "$after_stakeable" == "0" ]] || { [[ "$after_total" =~ ^[0-9]+$ ]] && [[ "$after_total" -gt "$before_total" ]]; }; then
      log "PASS: automation staked deposit"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  if [[ "$after_stakeable" =~ ^[0-9]+$ && "$after_total" =~ ^[0-9]+$ && "$after_stakeable" != "0" && "$after_total" == "$before_total" ]]; then
    echo "FAIL: stake() did not run (check params, automationCaller, logs)" >&2
    exit 1
  fi
  log "PASS (partial / slow): see totals stakeable=$after_stakeable totalStaked=$after_total"
}

write_state_file() {
  cat >"$STATE_FILE" <<EOF
POOL_ADDR=$POOL_ADDR
POOL_BECH32=$POOL_BECH32
RPC=$RPC
NODE=$NODE
CHAIN_ID=$CHAIN_ID
CHAIN_HOME=$CHAIN_HOME
EOF
  log "state → $STATE_FILE"
}

query_pool_delegator_bech32() {
  local qn ed_raw
  for qn in "$(cosmos_query_node)" "$NODE"; do
    ed_raw="$(evmd query poolrebalancer params --node "$qn" -o json 2>/dev/null | jq -r '.params.pool_delegator_address // ""' 2>/dev/null || true)"
    [[ "$ed_raw" == "null" ]] && ed_raw=""
    [[ -n "$ed_raw" ]] && { printf '%s' "$ed_raw"; return 0; }
  done
  printf ''
  return 1
}

try_resolve_pool_from_chain() {
  local del hex_out params_json dbg_out qn
  del=""
  for qn in "$(cosmos_query_node)" "$NODE"; do
    if params_json="$(evmd query poolrebalancer params --node "$qn" -o json 2>/dev/null)" && [[ -n "$params_json" ]]; then
      del="$(echo "$params_json" | jq -r '.params.pool_delegator_address // ""' 2>/dev/null || true)"
      [[ "$del" == "null" ]] && del=""
      [[ -n "$del" ]] && break
    fi
  done
  [[ -n "$del" ]] || return 1
  POOL_BECH32="$del"
  dbg_out="$(evmd_debug_addr "$POOL_BECH32")"
  hex_out="$(echo "$dbg_out" | rg -o '0x[0-9a-fA-F]{40}' | head -1)"
  [[ -n "$hex_out" && "$hex_out" =~ ^0x[0-9a-fA-F]{40}$ ]] || return 1
  POOL_ADDR="$hex_out"
  return 0
}

try_complete_pool_pair() {
  if [[ -n "${POOL_ADDR:-}" && "$POOL_ADDR" =~ ^0x[0-9a-fA-F]{40}$ && -z "${POOL_BECH32:-}" ]]; then
    POOL_BECH32="$(evmd_debug_addr "$POOL_ADDR" | rg 'Bech32 Acc' | awk '{print $3}' | head -1)"
    [[ -n "$POOL_BECH32" ]] && return 0
    return 1
  fi
  if [[ -n "${POOL_BECH32:-}" && -z "${POOL_ADDR:-}" ]]; then
    local dbg_out hex_out
    dbg_out="$(evmd_debug_addr "$POOL_BECH32")"
    hex_out="$(echo "$dbg_out" | rg -o '0x[0-9a-fA-F]{40}' | head -1)"
    [[ -n "$hex_out" && "$hex_out" =~ ^0x[0-9a-fA-F]{40}$ ]] && { POOL_ADDR="$hex_out"; return 0; }
    return 1
  fi
  return 0
}

hydrate_pool_for_watch() {
  [[ -n "${POOL_ADDR:-}" && -n "${POOL_BECH32:-}" ]] && return 0
  if [[ -f "$STATE_FILE" ]]; then
    log_detail "load $STATE_FILE"
    # shellcheck disable=SC1090
    source "$STATE_FILE"
  fi
  try_complete_pool_pair || true
  if [[ -z "${POOL_ADDR:-}" || -z "${POOL_BECH32:-}" ]]; then
    try_resolve_pool_from_chain && log "resolved pool from chain params"
  fi
  try_complete_pool_pair || true
  [[ -n "${POOL_ADDR:-}" && -n "${POOL_BECH32:-}" ]]
}

addr_lc() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

watch_loop() {
  hydrate_pool_for_watch || log_detail "pool unknown yet — use $STATE_FILE or run first"
  log "watch | new blocks | RPC=$RPC | auto-deposit every ${WATCH_AUTO_DEPOSIT_EVERY_BLOCKS} blocks when automationReady (amount=${WATCH_AUTO_DEPOSIT_AMOUNT} ogwei; 0=off) | Ctrl+C"
  local last_h="" h stakeable totalStaked rewardReserve pool_del caller_raw automation_ok mod_lc cl
  local blocks_while_ready=0
  while true; do
    h="$(curl -sS --max-time 1 "$(comet_status_url)" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // ""' || true)"
    [[ -z "$h" || "$h" == "0" ]] && { sleep 1; continue; }
    [[ "$h" == "$last_h" ]] && { sleep 1; continue; }
    last_h="$h"
    hydrate_pool_for_watch || true
    stakeable="n/a"
    totalStaked="n/a"
    rewardReserve="n/a"
    if [[ -n "${POOL_ADDR:-}" ]]; then
      stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
      totalStaked="$(pool_call_uint256 "totalStaked()(uint256)")"
      rewardReserve="$(pool_call_uint256 "rewardReserve()(uint256)")"
    fi
    pool_del="$(query_pool_delegator_bech32 || true)"
    caller_raw=""
    [[ -n "${POOL_ADDR:-}" ]] && caller_raw="$(cast call --rpc-url "$RPC" "$POOL_ADDR" "automationCaller()(address)" 2>/dev/null || true)"
    mod_lc="$(addr_lc "$MODULE_EVM")"
    cl="$(addr_lc "$caller_raw")"
    automation_ok="no"
    [[ -n "${POOL_BECH32:-}" && -n "$pool_del" && "$pool_del" == "$POOL_BECH32" && -n "$caller_raw" && "$cl" == "$mod_lc" ]] && automation_ok="yes"

    if [[ "$automation_ok" == "yes" ]]; then
      blocks_while_ready=$((blocks_while_ready + 1))
    else
      blocks_while_ready=0
    fi

    if [[ "$automation_ok" == "yes" && "${WATCH_AUTO_DEPOSIT_AMOUNT:-0}" != "0" &&
      -n "${POOL_ADDR:-}" && "${WATCH_AUTO_DEPOSIT_EVERY_BLOCKS:-10}" -ge 1 &&
      $((blocks_while_ready % WATCH_AUTO_DEPOSIT_EVERY_BLOCKS)) -eq 0 ]]; then
      log "watch: auto-deposit ${WATCH_AUTO_DEPOSIT_AMOUNT} ogwei at block ${h} (interval every ${WATCH_AUTO_DEPOSIT_EVERY_BLOCKS} blocks while automationReady)"
      if dev1_deposit_once "$WATCH_AUTO_DEPOSIT_AMOUNT" /tmp/pool_watch_approve.json /tmp/pool_watch_deposit.json; then
        log "watch: auto-deposit submitted successfully (${WATCH_AUTO_DEPOSIT_AMOUNT} ogwei)"
      else
        log "watch: auto-deposit failed (see log_detail above); will retry on next interval boundary"
      fi
    fi

    echo "[$(date '+%H:%M:%S')] block=$h pool=${POOL_ADDR:-?} stakeable=$stakeable totalStaked=$totalStaked rewardReserve=$rewardReserve automationReady=$automation_ok"
    sleep 1
  done
}

usage() {
  cat <<EOF
Usage: $0 run | watch | help

  run    Start devnet, deploy pool, gov, one test deposit, then watch.
  watch  Metrics each new block; optional periodic auto-deposit when automationReady.

Env (optional): CHAIN_HOME NODE RPC CHAIN_ID PK_DEV0 PK_DEV1 MODULE_EVM BOND_PRECOMPILE
  GOV_WAIT_INITIAL GOV_POLL_TIMEOUT DEPOSIT_AMOUNT
  WATCH_AUTO_DEPOSIT_EVERY_BLOCKS WATCH_AUTO_DEPOSIT_AMOUNT (0 disables watch auto-deposit)
  STATE_FILE CHAIN_LOG_FILE
EOF
}

show_help() {
  usage
  cat <<'EOF'

The run command sends one prove deposit (DEPOSIT_AMOUNT). Watch then logs each block and,
while automationReady, sends WATCH_AUTO_DEPOSIT_AMOUNT ogwei every WATCH_AUTO_DEPOSIT_EVERY_BLOCKS
new blocks (defaults 1e12 every 10; set WATCH_AUTO_DEPOSIT_AMOUNT=0 to disable).
EOF
}

main() {
  require_bin jq
  require_bin curl
  require_bin evmd
  require_bin cast
  require_bin rg
  trap cleanup EXIT
  trap stop_script_on_signal INT TERM
  case "${1:-run}" in
    run)
      log "run: chain → deploy → gov → one deposit → watch"
      start_local_node
      deploy_pool
      configure_automation
      prove_endblock_stake
      write_state_file
      watch_loop
      ;;
    watch)
      watch_loop
      ;;
    help|-h|--help)
      show_help
      ;;
    *)
      echo "unknown: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
