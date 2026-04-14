#!/usr/bin/env bash
# Shared helpers for tests/e2e/poolrebalancer/*.sh (e.g. user_flow_multikey.sh).
#
# Provides: EVM RPC discovery, bech32→hex for cast, uint256 cast helpers, approve+deposit,
#            cast send w/ receipt check, unbonding parse, user balance snapshots.
# Requires: cast, jq, evmd (where noted); python3 optional for hex/JSON edge cases.
#
set -euo pipefail

# --- CometBFT / JSON-RPC mapping (NODE_RPC tcp …:26657 → val_i JSON-RPC 8545+i*10) ---

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

# Poll common JSON-RPC ports until cast chain-id succeeds; exports EVM_RPC.
ensure_evm_rpc_ready() {
  local candidates=() derived="" c start now
  if derived="$(derive_evm_rpc_from_node_rpc "${NODE_RPC:-}" 2>/dev/null || true)"; then
    candidates+=("$derived")
  fi
  candidates+=("${EVM_RPC:-}")
  candidates+=("http://127.0.0.1:8545" "http://127.0.0.1:8555" "http://127.0.0.1:8565" "http://127.0.0.1:8575")

  echo "==> Waiting for EVM RPC readiness"
  start="$(date +%s)"
  while true; do
    for c in "${candidates[@]}"; do
      [[ -z "$c" ]] && continue
      if cast chain-id --rpc-url "$c" >/dev/null 2>&1; then
        if [[ "${EVM_RPC:-}" != "$c" ]]; then
          echo "==> Using EVM RPC endpoint: $c"
        fi
        EVM_RPC="$c"
        return 0
      fi
    done
    now="$(date +%s)"
    if (( now - start > 120 )); then
      echo "error: no reachable EVM JSON-RPC endpoint found after 120s" >&2
      echo "tried: ${candidates[*]}" >&2
      return 1
    fi
    sleep 2
  done
}

# --- Dev accounts (multi_node_startup / scenario runner dev_accounts.txt) ---

dev_account_private_key_from_file() {
  local account_name="$1"
  local f="$2"
  [[ -f "$f" ]] || return 1
  awk -v name="$account_name" '
    $0 ~ ("^" name ":") {in_block=1; next}
    in_block && $1=="private_key:" {print $2; exit}
    in_block && /^[^[:space:]]/ {in_block=0}
  ' "$f"
}

# --- Bech32 account → 0x for cast (uses CHAIN_HOME when set) ---

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
    hex="$(printf '%s\n' "$dbg" | awk -F': ' '/Address \(hex\)/{print $2; exit}')"
  fi
  if [[ "$hex" =~ ^[0-9a-fA-F]{40}$ ]]; then
    hex="0x$hex"
  fi
  printf '%s' "$hex"
}

# --- cast call helpers ---

normalize_cast_uint256_output() {
  local s="${1:-}"
  s="${s//$'\r'/}"
  s="${s%%$'\n'*}"
  s="${s%% *}"
  s="${s//$'\t'/}"
  [[ "$s" =~ ^[0-9]+$ ]] && { printf '%s' "$s"; return 0; }
  if [[ "$s" =~ ^0x[0-9a-fA-F]+$ ]] && command -v python3 >/dev/null 2>&1; then
    python3 -c "print(int('$s',16))" 2>/dev/null && return 0
  fi
  return 1
}

# View call without extra args; returns decimal string or n/a.
pool_evm_call_uint256() {
  local pool="$1"
  local rpc="$2"
  local sig="$3"
  local raw norm
  [[ -z "$pool" ]] && { printf 'n/a'; return 0; }
  raw="$(cast call --rpc-url "$rpc" "$pool" "$sig" 2>/dev/null || true)"
  if norm="$(normalize_cast_uint256_output "$raw")"; then
    printf '%s' "$norm"
  else
    printf 'n/a'
  fi
}

pool_evm_call_uint256_args() {
  local pool="$1"
  local rpc="$2"
  local sig="$3"
  shift 3
  local raw norm
  [[ -z "$pool" ]] && { printf 'n/a'; return 0; }
  raw="$(cast call --rpc-url "$rpc" "$pool" "$sig" "$@" 2>/dev/null || true)"
  if norm="$(normalize_cast_uint256_output "$raw")"; then
    printf '%s' "$norm"
  else
    printf 'n/a'
  fi
}

# Normalize cast balance output to decimal wei (decimal or 0x hex).
normalize_cast_balance_wei() {
  local s="${1:-}"
  s="${s//$'\r'/}"
  s="${s%%$'\n'*}"
  s="${s//[[:space:]]/}"
  [[ -z "$s" ]] && { printf 'n/a'; return 0; }
  [[ "$s" =~ ^[0-9]+$ ]] && { printf '%s' "$s"; return 0; }
  if [[ "$s" =~ ^0x[0-9a-fA-F]+$ ]] && command -v python3 >/dev/null 2>&1; then
    python3 -c "print(int('$s',16))" 2>/dev/null && return 0
  fi
  printf '%s' "$s"
}

# Multi-line snapshot for E2E logs (native wallet, bond ERC20, pool unitsOf).
log_user_withdraw_snapshot() {
  local rpc="$1"
  local pool_evm="$2"
  local bond_precompile="$3"
  local user_addr="$4"
  local name="$5"
  local label="$6"
  local liquid_native bond_bal units
  liquid_native="$(normalize_cast_balance_wei "$(cast balance --rpc-url "$rpc" "$user_addr" 2>/dev/null || true)")"
  bond_bal="$(pool_evm_call_uint256_args "$bond_precompile" "$rpc" "balanceOf(address)(uint256)" "$user_addr")"
  units="$(pool_evm_call_uint256_args "$pool_evm" "$rpc" "unitsOf(address)(uint256)" "$user_addr")"
  echo "    snapshot[$label] $name"
  echo "      addr                 $user_addr"
  echo "      liquid_native_wei    $liquid_native   (EOA native balance)"
  echo "      bond_token_wei       $bond_bal   (bond ERC20 in wallet)"
  echo "      pool_units           $units   (CommunityPool unitsOf)"
}

# --- cast send: wait for nonce, parse JSON receipt, check status ---

wait_evm_nonce_settled_for_pk() {
  local pk="$1"
  local rpc="$2"
  local deadline_sec="${3:-45}"
  local addr pending latest t0
  addr="$(cast wallet address --private-key "$pk")"
  t0="$(date +%s)"
  while true; do
    pending="$(cast nonce --rpc-url "$rpc" --block pending "$addr" 2>/dev/null || true)"
    latest="$(cast nonce --rpc-url "$rpc" --block latest "$addr" 2>/dev/null || true)"
    [[ -z "$pending" || -z "$latest" ]] && return 0
    [[ "$pending" == "$latest" ]] && return 0
    if (( $(date +%s) - t0 > deadline_sec )); then
      return 0
    fi
    sleep 1
  done
}

# Receipt .status is 0x1 / 0x01 when successful.
cast_receipt_success() {
  local json="$1"
  local status
  status="$(echo "$json" | jq -r '.status // empty')"
  [[ "$status" == "0x1" || "$status" == "0x01" ]]
}

# cast --json stdout may include non-JSON lines before the receipt; decode first JSON value from first '{'.
cast_stdout_to_receipt_json() {
  local raw="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import json, sys
s = sys.stdin.read()
start = s.find('{')
if start < 0:
    sys.exit(1)
obj, _ = json.JSONDecoder().raw_decode(s[start:])
sys.stdout.write(json.dumps(obj))
" <<<"$raw" 2>/dev/null && return 0
  fi
  local line json_line=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^\{ ]] && json_line="$line"
  done <<<"$(printf '%s\n' "$raw")"
  [[ -n "${json_line:-}" ]] && printf '%s' "$json_line" && return 0
  return 1
}

cast_send_expect_success() {
  local rpc="$1"
  local pk="$2"
  local target="$3"
  local sig="$4"
  shift 4
  local errf raw json
  wait_evm_nonce_settled_for_pk "$pk" "$rpc" 45
  errf="$(mktemp -t cast_ok.XXXXXX)"
  if [[ -n "${CAST_SEND_GAS_LIMIT:-}" ]]; then
    raw="$(cast send --json --rpc-url "$rpc" --private-key "$pk" --gas-limit "${CAST_SEND_GAS_LIMIT}" "$target" "$sig" "$@" 2>"$errf")" || {
      cat "$errf" >&2
      rm -f "$errf"
      return 1
    }
  else
    raw="$(cast send --json --rpc-url "$rpc" --private-key "$pk" "$target" "$sig" "$@" 2>"$errf")" || {
      cat "$errf" >&2
      rm -f "$errf"
      return 1
    }
  fi
  rm -f "$errf"
  if ! json="$(cast_stdout_to_receipt_json "$raw")"; then
    echo "error: cast send did not return parseable JSON receipt (target=$target sig=$sig)" >&2
    echo "$raw" >&2
    return 1
  fi
  if ! echo "$json" | jq -e '.status' >/dev/null 2>&1; then
    echo "error: receipt JSON missing .status (target=$target sig=$sig)" >&2
    echo "$json" >&2
    return 1
  fi
  if cast_receipt_success "$json"; then
    return 0
  fi
  echo "error: transaction reverted (status=$(echo "$json" | jq -r .status)) target=$target sig=$sig" >&2
  return 1
}

# Standard pool onboarding: approve bond for pool, then deposit(uint256).
approve_and_deposit() {
  local pk="$1"
  local pool_evm="$2"
  local bond_precompile="$3"
  local amount="$4"
  local rpc="$5"
  cast_send_expect_success "$rpc" "$pk" "$bond_precompile" \
    "approve(address,uint256)" "$pool_evm" "$amount" \
    && cast_send_expect_success "$rpc" "$pk" "$pool_evm" "deposit(uint256)" "$amount"
}

# staking params unbonding_time string (e.g. 30s, 1h30m) → integer seconds.
parse_unbonding_seconds() {
  local raw="${1:-30s}"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import re, sys
raw = sys.argv[1].strip()
if not raw:
    print(30)
    sys.exit(0)
secs = 0
for n, u in re.findall(r'(\d+)(h|m|s)', raw):
    n = int(n)
    if u == 'h': secs += n * 3600
    elif u == 'm': secs += n * 60
    else: secs += n
print(secs if secs else 30)
" "$raw" 2>/dev/null && return 0
  fi
  echo 30
}
