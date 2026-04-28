#!/usr/bin/env bash
# Multi-account CommunityPool E2E: deposit → (optional) withdraw / claimWithdraw → (optional) claimRewards.
# Needs: dev_accounts.txt, pool_delegator_address on chain or POOL_CONTRACT_ADDR. See tests/e2e/poolrebalancer/README.md.
# Shared helpers: lib/pool_e2e_common.sh (RPC, cast, approve+deposit).
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/lib/pool_e2e_common.sh"

ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"

# --- Paths & chain endpoints ---
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-tcp://127.0.0.1:26657}"
CHAIN_ID="${CHAIN_ID:-10740}"
EVM_RPC="${EVM_RPC:-http://127.0.0.1:8545}"
BOND_PRECOMPILE="${BOND_PRECOMPILE:-0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE}"
CHAIN_HOME="${CHAIN_HOME:-$BASEDIR/val0}"

# --- Pool & accounts ---
POOL_CONTRACT_ADDR="${POOL_CONTRACT_ADDR:-}"
# Optional opt-in stress profile. Empty preserves existing behavior.
USER_FLOW_STRESS_PROFILE="${USER_FLOW_STRESS_PROFILE:-}"
USER_COUNT_SET_BY_ENV=0
DEPOSIT_INTERVAL_SECS_SET_BY_ENV=0
FAIL_FAST_SET_BY_ENV=0
WITHDRAW_USERS_SET_BY_ENV=0
WITHDRAW_SUBMIT_RETRIES_SET_BY_ENV=0
CLAIM_POLL_MAX_ATTEMPTS_SET_BY_ENV=0
USER_FLOW_MODE_SET_BY_ENV=0
DEPOSIT_CONCURRENCY_SET_BY_ENV=0
WITHDRAW_CONCURRENCY_SET_BY_ENV=0
CLAIM_CONCURRENCY_SET_BY_ENV=0
CLAIM_REWARDS_CONCURRENCY_SET_BY_ENV=0
BATCH_DELAY_MS_SET_BY_ENV=0
if [[ -n "${USER_COUNT+x}" ]]; then USER_COUNT_SET_BY_ENV=1; fi
if [[ -n "${DEPOSIT_INTERVAL_SECS+x}" ]]; then DEPOSIT_INTERVAL_SECS_SET_BY_ENV=1; fi
if [[ -n "${FAIL_FAST+x}" ]]; then FAIL_FAST_SET_BY_ENV=1; fi
if [[ -n "${WITHDRAW_USERS+x}" ]]; then WITHDRAW_USERS_SET_BY_ENV=1; fi
if [[ -n "${WITHDRAW_SUBMIT_RETRIES+x}" ]]; then WITHDRAW_SUBMIT_RETRIES_SET_BY_ENV=1; fi
if [[ -n "${CLAIM_POLL_MAX_ATTEMPTS+x}" ]]; then CLAIM_POLL_MAX_ATTEMPTS_SET_BY_ENV=1; fi
if [[ -n "${USER_FLOW_MODE+x}" ]]; then USER_FLOW_MODE_SET_BY_ENV=1; fi
if [[ -n "${DEPOSIT_CONCURRENCY+x}" ]]; then DEPOSIT_CONCURRENCY_SET_BY_ENV=1; fi
if [[ -n "${WITHDRAW_CONCURRENCY+x}" ]]; then WITHDRAW_CONCURRENCY_SET_BY_ENV=1; fi
if [[ -n "${CLAIM_CONCURRENCY+x}" ]]; then CLAIM_CONCURRENCY_SET_BY_ENV=1; fi
if [[ -n "${CLAIM_REWARDS_CONCURRENCY+x}" ]]; then CLAIM_REWARDS_CONCURRENCY_SET_BY_ENV=1; fi
if [[ -n "${BATCH_DELAY_MS+x}" ]]; then BATCH_DELAY_MS_SET_BY_ENV=1; fi
USER_COUNT="${USER_COUNT:-5}"
DEPOSIT_AMOUNT_WEI="${DEPOSIT_AMOUNT_WEI:-100000000000000000000}"
DEV_ACCOUNTS_FILE="${DEV_ACCOUNTS_FILE:-$BASEDIR/dev_accounts.txt}"
AUTO_PROVISION_DEV_ACCOUNTS="${AUTO_PROVISION_DEV_ACCOUNTS:-1}"
AUTO_PROVISION_FUND_WEI="${AUTO_PROVISION_FUND_WEI:-1000000000000000000000}"
SKIP_DEPOSITS="${SKIP_DEPOSITS:-0}"
FAIL_FAST="${FAIL_FAST:-1}"
DEPOSIT_INTERVAL_SECS="${DEPOSIT_INTERVAL_SECS:-2}"
USER_FLOW_MODE="${USER_FLOW_MODE:-serial}"
DEPOSIT_CONCURRENCY="${DEPOSIT_CONCURRENCY:-1}"
WITHDRAW_CONCURRENCY="${WITHDRAW_CONCURRENCY:-1}"
CLAIM_CONCURRENCY="${CLAIM_CONCURRENCY:-1}"
CLAIM_REWARDS_CONCURRENCY="${CLAIM_REWARDS_CONCURRENCY:-1}"
BATCH_DELAY_MS="${BATCH_DELAY_MS:-0}"

# --- Withdraw / claim ---
WITHDRAW_USERS="${WITHDRAW_USERS:-3}"
WITHDRAW_FRACTION_BP="${WITHDRAW_FRACTION_BP:-1000}"
UNBONDING_WAIT_BUFFER_SECS="${UNBONDING_WAIT_BUFFER_SECS:-10}"
CLAIM_POLL_INTERVAL_SECS="${CLAIM_POLL_INTERVAL_SECS:-2}"
CLAIM_POLL_MAX_ATTEMPTS="${CLAIM_POLL_MAX_ATTEMPTS:-100}"
WITHDRAW_SUBMIT_RETRIES="${WITHDRAW_SUBMIT_RETRIES:-25}"
WITHDRAW_RETRY_SLEEP_SECS="${WITHDRAW_RETRY_SLEEP_SECS:-2}"
WITHDRAW_CLAIM_GAS_LIMIT="${WITHDRAW_CLAIM_GAS_LIMIT:-9500000}"

# --- Optional reward paths (defaults favor withdraw-internal reward handling) ---
POOL_OWNER_PK="${POOL_OWNER_PK:-}"
PRE_WITHDRAW_HARVEST="${PRE_WITHDRAW_HARVEST:-0}"
PRE_WITHDRAW_CLAIM_REWARDS="${PRE_WITHDRAW_CLAIM_REWARDS:-0}"
REWARD_SYNC_WAIT_SECS="${REWARD_SYNC_WAIT_SECS:-0}"
POST_CLAIMWITHDRAW_CLAIM_REWARDS="${POST_CLAIMWITHDRAW_CLAIM_REWARDS:-1}"
POST_CLAIMWITHDRAW_WAIT_SECS="${POST_CLAIMWITHDRAW_WAIT_SECS:-20}"
POST_CLAIMWITHDRAW_USERS="${POST_CLAIMWITHDRAW_USERS:-}"

POOL_EVM_ADDR=""
DEPOSIT_OK=0
DEPOSIT_FAIL=0
RUN_START_TS=0
RUN_END_TS=0
WITHDRAW_SUBMIT_ATTEMPTED=0
WITHDRAW_SUBMIT_SUCCESS=0
WITHDRAW_SUBMIT_FAILED=0
WITHDRAW_SUBMIT_RETRIES_TOTAL=0
CLAIM_ATTEMPTED=0
CLAIM_SUCCESS=0
CLAIM_FAILED=0
CLAIM_RETRIES_TOTAL=0
POST_CLAIM_REWARDS_ATTEMPTED=0
POST_CLAIM_REWARDS_SUCCESS=0
POST_CLAIM_REWARDS_FAILED=0
WITHDRAW_REQUEST_WINDOW_START=""
WITHDRAW_REQUEST_WINDOW_END=""
WITHDRAW_REQUESTS_MAPPED=0
WITHDRAW_REQUESTS_CLAIMED_VERIFIED=0

usage() {
  cat <<EOF
Usage: $0 [options]

What this script tests (CommunityPool, not x/poolrebalancer scheduling):
  - Multi-account flows: several dev keys each approve bond and deposit into the pool contract.
  - Optional exit path: fraction of LP units via withdraw(), wait for unbonding + maturity time, claimWithdraw().
  - Optional reward path: after claimWithdraws, standalone claimRewards() to exercise accrual payout vs
    rewards settled inside withdraw().

What you see in the log:
  - Pool-level views (totalUnits, totalStaked, …) and per-user snapshots: native wei, bond token balance,
    pool units around each transaction; maturity polling; balance deltas on claimRewards when enabled.

Defaults: USER_COUNT=5, WITHDRAW_USERS=3, POST_CLAIMWITHDRAW_CLAIM_REWARDS=1 (see env vars below to disable).

Environment (common):
  BASEDIR              Default: \$HOME/.og-evm-devnet
  NODE_RPC             Default: tcp://127.0.0.1:26657
  CHAIN_ID             Default: 10740
  EVM_RPC              Default: http://127.0.0.1:8545
  CHAIN_HOME           Default: \$BASEDIR/val0
  BOND_PRECOMPILE      Bond ERC20 precompile address
  POOL_CONTRACT_ADDR   CommunityPool 0x address (or resolve from poolrebalancer params)
  DEV_ACCOUNTS_FILE    Default: \$BASEDIR/dev_accounts.txt
  USER_FLOW_STRESS_PROFILE  Optional profile: 100users|stress100 (opt-in scale/retry defaults)
                            Profile defaults (only if env not explicitly set):
                              USER_COUNT=100, WITHDRAW_USERS=30, FAIL_FAST=0,
                              DEPOSIT_INTERVAL_SECS=0, WITHDRAW_SUBMIT_RETRIES=40,
                              CLAIM_POLL_MAX_ATTEMPTS=180
  USER_FLOW_MODE            serial|parallel (default: serial)
  DEPOSIT_CONCURRENCY       Parallel workers for deposit phase (default: 1)
  WITHDRAW_CONCURRENCY      Parallel workers for withdraw submit phase (default: 1)
  CLAIM_CONCURRENCY         Parallel workers for claimWithdraw phase (default: 1)
  CLAIM_REWARDS_CONCURRENCY Parallel workers for post claimRewards phase (default: 1)
  BATCH_DELAY_MS            Delay between worker batches in parallel mode (default: 0)

Deposit phase:
  USER_COUNT             Dev accounts to use (default: 5)
  DEPOSIT_AMOUNT_WEI     Per-user deposit (default: 1e20 wei)
  SKIP_DEPOSITS          If 1, skip deposits (only withdraw/claim)
  FAIL_FAST              If 1, exit on first deposit failure (default: 1)
  DEPOSIT_INTERVAL_SECS  Seconds after each deposit (default: 2)

Withdraw phase (optional):
  WITHDRAW_USERS         First N users run withdraw+claim (default: 3; set 0 or use --deposits-only for deposits only)
  WITHDRAW_FRACTION_BP   Basis points of user units to withdraw (default: 1000 = 10%)
  POOL_OWNER_PK          Private key for optional manual harvest() (default: dev0)
  PRE_WITHDRAW_HARVEST   If 1, owner harvest() before withdraw (default: 0)
  PRE_WITHDRAW_CLAIM_REWARDS  If 1, claimRewards() before withdraw (default: 0)
  POST_CLAIMWITHDRAW_CLAIM_REWARDS  If 1, after last claimWithdraw wait then claimRewards() per user (default: 1)
  POST_CLAIMWITHDRAW_WAIT_SECS      Wall seconds to wait after claimWithdraws (more blocks) before claimRewards (default: 20)
  POST_CLAIMWITHDRAW_USERS          How many dev users dev0..dev(N-1) run claimRewards (default: USER_COUNT)
  REWARD_SYNC_WAIT_SECS  Sleep after harvest / before optional pre-claim (default: 0)
  WITHDRAW_RETRY_SLEEP_SECS  Sleep between withdraw() retries (default: 2)
  UNBONDING_WAIT_BUFFER_SECS  Extra seconds after unbonding_time (default: 10)
  CLAIM_POLL_INTERVAL_SECS    Between claimWithdraw attempts (default: 2)
  CLAIM_POLL_MAX_ATTEMPTS     Max claim attempts per request (default: 100)

Examples:
  POOL_CONTRACT_ADDR=0xabc... USER_COUNT=10 bash $0
  WITHDRAW_USERS=2 WITHDRAW_FRACTION_BP=10000 bash $0
  WITHDRAW_USERS=0 bash $0
  USER_FLOW_STRESS_PROFILE=100users bash $0
  bash $0 --deposits-only

Notes:
  - Default: no pre-withdraw claimRewards(); CommunityPool.withdraw() settles pending rewards internally.
  - Withdraw phase logs liquid_native_wei (cast balance), bond_token_wei (ERC20 balanceOf), pool_units at:
    before_withdraw, after_withdraw, before_claimWithdraw, after_claimWithdraw.
  - POST_CLAIMWITHDRAW_CLAIM_REWARDS=1 adds before_claimRewards_postwithdraw / after_claimRewards_postwithdraw and liquid_native_wei_delta per user.
  - Stress profile is throughput-oriented and intentionally non-default (slower/flakier on busy devnets).
  - In stress profile, failures are aggregated for summary output instead of always exiting at first failure.
EOF
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing dependency: $1" >&2
    exit 1
  }
}

# Visual phase break + title; extra args are bullet hints.
log_flow_section() {
  echo ""
  echo "--------------------------------------------------------------------"
  printf "==> %s\n" "$1"
  shift || true
  while [[ $# -gt 0 ]]; do
    printf "    * %s\n" "$1"
    shift
  done
  echo "--------------------------------------------------------------------"
}

is_stress_mode_active() {
  [[ -n "${USER_FLOW_STRESS_PROFILE:-}" ]]
}

safe_ratio_percent_2dp() {
  local num="${1:-0}" den="${2:-0}"
  python3 - "$num" "$den" <<'PY'
import sys
n = int(sys.argv[1])
d = int(sys.argv[2])
if d <= 0:
    print("0.00")
else:
    print(f"{(n * 100.0) / d:.2f}")
PY
}

safe_avg_2dp() {
  local num="${1:-0}" den="${2:-0}"
  python3 - "$num" "$den" <<'PY'
import sys
n = int(sys.argv[1])
d = int(sys.argv[2])
if d <= 0:
    print("0.00")
else:
    print(f"{n / d:.2f}")
PY
}

is_parallel_mode_active() {
  [[ "${USER_FLOW_MODE:-serial}" == "parallel" ]]
}

sleep_ms() {
  local ms="${1:-0}"
  [[ "$ms" =~ ^[0-9]+$ ]] || ms=0
  (( ms <= 0 )) && return 0
  python3 - "$ms" <<'PY'
import sys
import time
time.sleep(int(sys.argv[1]) / 1000.0)
PY
}

count_dev_accounts_in_file() {
  local f="$1"
  awk '
    /^dev[0-9]+:/ { c++ }
    END { print c + 0 }
  ' "$f" 2>/dev/null || echo 0
}

append_dev_account_to_file() {
  local f="$1" name="$2" bech32="$3" priv="$4" mnemonic="$5"
  {
    echo ""
    echo "${name}:"
    echo "  address: ${bech32}"
    echo "  private_key: ${priv}"
    echo "  mnemonic: ${mnemonic}"
  } >>"$f"
}

create_dev_account_record() {
  local idx="$1"
  local name="dev${idx}"
  local dev_home="$BASEDIR/.dev_keys_tmp"
  local full_output mnemonic bech32 priv

  rm -rf "$dev_home"
  mkdir -p "$dev_home"
  full_output="$(evmd keys add "$name" --keyring-backend test --algo eth_secp256k1 --home "$dev_home" 2>&1)"
  mnemonic="$(printf '%s\n' "$full_output" | sed -n '$p')"
  bech32="$(evmd keys show "$name" -a --keyring-backend test --home "$dev_home" 2>/dev/null || true)"
  priv="$(evmd keys unsafe-export-eth-key "$name" --keyring-backend test --home "$dev_home" 2>/dev/null || true)"
  rm -rf "$dev_home"

  if [[ -z "$bech32" || -z "$priv" ]]; then
    echo "error: failed to create account metadata for $name" >&2
    return 1
  fi
  if [[ "$priv" != 0x* ]]; then
    priv="0x$priv"
  fi
  printf '%s\n%s\n%s\n%s\n' "$name" "$bech32" "$priv" "$mnemonic"
}

fund_dev_account_from_dev0() {
  local to_addr_hex="$1"
  local funder_pk
  local errf
  funder_pk="$(dev_account_private_key_from_file "dev0" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$funder_pk" ]]; then
    echo "error: cannot auto-provision accounts: missing dev0 private key in $DEV_ACCOUNTS_FILE" >&2
    return 1
  fi
  wait_evm_nonce_settled_for_pk "$funder_pk" "$EVM_RPC" 45
  errf="$(mktemp -t cast_fund_dev.XXXXXX)"
  cast send --json --rpc-url "$EVM_RPC" --private-key "$funder_pk" --value "$AUTO_PROVISION_FUND_WEI" "$to_addr_hex" >/dev/null 2>"$errf" || {
    cat "$errf" >&2
    rm -f "$errf"
    return 1
  }
  rm -f "$errf"
}

ensure_dev_accounts_available() {
  local target_count="$1"
  local available idx record name bech32 priv mnemonic evm_addr

  available="$(count_dev_accounts_in_file "$DEV_ACCOUNTS_FILE")"
  [[ "$available" =~ ^[0-9]+$ ]] || available=0
  (( available >= target_count )) && return 0

  if [[ "$AUTO_PROVISION_DEV_ACCOUNTS" != "1" ]]; then
    return 1
  fi

  log_flow_section "Auto-provision dev accounts" \
    "Requested USER_COUNT=$target_count but only $available account(s) in $DEV_ACCOUNTS_FILE." \
    "Creating and funding dev${available}..dev$((target_count - 1)) with AUTO_PROVISION_FUND_WEI=$AUTO_PROVISION_FUND_WEI."

  for idx in $(seq "$available" $((target_count - 1))); do
    echo "  -- provisioning dev${idx} ($((idx - available + 1))/$((target_count - available)))"
    record="$(create_dev_account_record "$idx")" || return 1
    name="$(printf '%s\n' "$record" | sed -n '1p')"
    bech32="$(printf '%s\n' "$record" | sed -n '2p')"
    priv="$(printf '%s\n' "$record" | sed -n '3p')"
    mnemonic="$(printf '%s\n' "$record" | sed -n '4p')"
    evm_addr="$(resolve_evm_hex_from_bech32 "$bech32")"
    if [[ -z "$evm_addr" || "$evm_addr" == "0x" ]]; then
      echo "error: could not derive EVM address for auto-provisioned $name ($bech32)" >&2
      return 1
    fi
    fund_dev_account_from_dev0 "$evm_addr" || return 1
    append_dev_account_to_file "$DEV_ACCOUNTS_FILE" "$name" "$bech32" "$priv" "$mnemonic"
    echo "  -- provisioned $name: $bech32 ($evm_addr)"
  done
}

normalize_user_counts_for_available_accounts() {
  local available
  local explicit_user_count_requested=0
  (( USER_COUNT_SET_BY_ENV == 1 )) && explicit_user_count_requested=1
  available="$(count_dev_accounts_in_file "$DEV_ACCOUNTS_FILE")"
  [[ "$available" =~ ^[0-9]+$ ]] || available=0
  if (( available < 1 )); then
    echo "error: no dev accounts found in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi

  if (( USER_COUNT > available )); then
    if is_stress_mode_active || (( explicit_user_count_requested == 1 )); then
      if ensure_dev_accounts_available "$USER_COUNT"; then
        available="$(count_dev_accounts_in_file "$DEV_ACCOUNTS_FILE")"
      else
        echo "error: requested USER_COUNT=$USER_COUNT but only $available dev accounts are available and auto-provisioning failed" >&2
        echo "hint: ensure EVM RPC is reachable and dev0 has funds; or lower --user-count / USER_COUNT" >&2
        exit 1
      fi
    else
      echo "error: USER_COUNT=$USER_COUNT exceeds available dev accounts=$available in $DEV_ACCOUNTS_FILE" >&2
      exit 1
    fi
  fi

  if (( WITHDRAW_USERS > USER_COUNT )); then
    echo "warning: WITHDRAW_USERS=$WITHDRAW_USERS exceeds USER_COUNT=$USER_COUNT; capping WITHDRAW_USERS to $USER_COUNT" >&2
    WITHDRAW_USERS="$USER_COUNT"
  fi
}

# POOL_CONTRACT_ADDR wins; else query poolrebalancer params and map bech32 → 0x.
resolve_pool_evm_addr() {
  if [[ -n "$POOL_CONTRACT_ADDR" ]]; then
    POOL_EVM_ADDR="$POOL_CONTRACT_ADDR"
    log_flow_section "Pool contract (from env)" \
      "Using POOL_CONTRACT_ADDR from environment (skipping chain query for pool_delegator_address)."
    echo "    POOL_CONTRACT_ADDR=$POOL_EVM_ADDR"
    return 0
  fi
  local del params
  log_flow_section "Resolve CommunityPool from chain" \
    "Reading x/poolrebalancer params for pool_delegator_address, then mapping bech32 to EVM 0x for cast calls."
  params="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json 2>/dev/null || true)"
  del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
  if [[ -z "$del" ]]; then
    echo "error: set POOL_CONTRACT_ADDR or configure poolrebalancer.params.pool_delegator_address" >&2
    exit 1
  fi
  POOL_EVM_ADDR="$(resolve_evm_hex_from_bech32 "$del")"
  if [[ -z "$POOL_EVM_ADDR" || "$POOL_EVM_ADDR" == "0x" ]]; then
    echo "error: could not resolve EVM address for pool delegator $del" >&2
    exit 1
  fi
  echo "    pool_delegator_bech32  $del"
  echo "    pool_evm               $POOL_EVM_ADDR"
}

# withdraw() requires non-zero pool delegation; poll totalStaked until set.
wait_for_total_staked() {
  local timeout="${1:-180}"
  local start total
  start="$(date +%s)"
  log_flow_section "Wait for pool stake (totalStaked > 0)" \
    "withdraw() sizing needs the pool to have delegated stake; polling CommunityPool.totalStaked()."
  while true; do
    total="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    if [[ "$total" =~ ^[0-9]+$ ]] && [[ "$total" != "0" ]]; then
      echo "    ok totalStaked=$total"
      return 0
    fi
    if (( $(date +%s) - start > timeout )); then
      echo "error: totalStaked still zero after ${timeout}s" >&2
      exit 1
    fi
    sleep 2
  done
}

# Aggregate CommunityPool getters after deposits.
log_pool_snapshot() {
  local tu pa ts spl
  tu="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  pa="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "principalAssets()(uint256)")"
  ts="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  spl="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  log_flow_section "On-chain pool snapshot (read-only)" \
    "CommunityPool aggregate state after deposits (and any prior activity)."
  echo "    totalUnits                 $tu   (sum of LP units)"
  echo "    principalAssets            $pa   (principal backing)"
  echo "    totalStaked                $ts   (bond delegated via poolrebalancer)"
  echo "    stakeablePrincipalLedger   $spl   (principal available to stake)"
}

print_contract_correctness_checks() {
  local phase="$1"
  local tu pa ts spl rr
  tu="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  pa="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "principalAssets()(uint256)")"
  ts="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  spl="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  rr="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"

  local principal_minus_staked="n/a"
  local unit_price_ppm="n/a"
  if [[ "$pa" =~ ^[0-9]+$ && "$ts" =~ ^[0-9]+$ ]]; then
    principal_minus_staked=$((pa - ts))
  fi
  if [[ "$tu" =~ ^[0-9]+$ && "$tu" != "0" && "$pa" =~ ^[0-9]+$ ]]; then
    unit_price_ppm="$(python3 -c "print((int('$pa') * 1000000) // int('$tu'))")"
  fi

  log_flow_section "Contract correctness checks ($phase)" \
    "pool=$POOL_EVM_ADDR" \
    "totals: totalUnits=$tu principalAssets=$pa totalStaked=$ts stakeablePrincipalLedger=$spl rewardReserve=$rr" \
    "derived: principal_minus_staked=$principal_minus_staked unit_price_ppm=$unit_price_ppm (assets per unit * 1e6)" \
    "flow: deposits_ok=$DEPOSIT_OK/$USER_COUNT withdraw_submitted=$WITHDRAW_SUBMIT_SUCCESS claim_ok=$CLAIM_SUCCESS post_claimRewards_ok=$POST_CLAIM_REWARDS_SUCCESS"

  if [[ -n "$WITHDRAW_REQUEST_WINDOW_START" && -n "$WITHDRAW_REQUEST_WINDOW_END" ]]; then
    echo "    withdraw_request_window: [$WITHDRAW_REQUEST_WINDOW_START, $WITHDRAW_REQUEST_WINDOW_END)"
    echo "    mapped_requests=$WITHDRAW_REQUESTS_MAPPED claimed_verified=$WITHDRAW_REQUESTS_CLAIMED_VERIFIED"
  fi
}

log_contract_snapshot_for_batch() {
  local phase="$1"
  local batch_label="$2"
  local tu pa ts spl rr
  tu="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  pa="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "principalAssets()(uint256)")"
  ts="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  spl="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  rr="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
  echo "     snapshot[$phase][$batch_label]: totalUnits=$tu principalAssets=$pa totalStaked=$ts stakeablePrincipalLedger=$spl rewardReserve=$rr"
}

withdraw_request_claimed_flag() {
  local rid="$1"
  local raw
  raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" \
    "withdrawRequests(uint256)(address,uint256,uint64,bool,bool)" "$rid" 2>/dev/null || true)"
  printf '%s\n' "$raw" | awk 'NF{c++} c==4 {print $1; exit}'
}

# Approve + deposit for dev0..dev(N-1).
run_deposits() {
  local i pk name
  log_flow_section "Deposits ($USER_COUNT accounts)" \
    "Per account: approve bond ERC20 on the bond precompile, then CommunityPool.deposit(amount). Amount wei: $DEPOSIT_AMOUNT_WEI."
  if is_parallel_mode_active && (( DEPOSIT_CONCURRENCY > 1 )); then
    local batch_start batch_end conc tmpdir
    conc="$DEPOSIT_CONCURRENCY"
    tmpdir="$(mktemp -d -t user_flow_dep.XXXXXX)"
    for batch_start in $(seq 0 "$conc" $((USER_COUNT - 1))); do
      batch_end=$((batch_start + conc - 1))
      (( batch_end >= USER_COUNT )) && batch_end=$((USER_COUNT - 1))
      local pids=() files=()
      for i in $(seq "$batch_start" "$batch_end"); do
        local f="$tmpdir/dep_${i}.out"
        files+=("$f")
        (
          name="dev${i}"
          pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
          if [[ -z "$pk" ]]; then
            echo "FAIL|$name|missing private key"
            exit 0
          fi
          echo "  -- $name: approve bond + deposit into pool"
          if approve_and_deposit "$pk" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$DEPOSIT_AMOUNT_WEI" "$EVM_RPC"; then
            echo "OK|$name|"
          else
            echo "FAIL|$name|deposit tx failed"
          fi
        ) >"$f" 2>&1 &
        pids+=("$!")
      done
      for p in "${pids[@]}"; do wait "$p"; done
      for f in "${files[@]}"; do
        local r line status uname msg last_log
        line="$(awk '/^(OK|FAIL)\|/{print; exit}' "$f" 2>/dev/null || true)"
        status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
        uname="$(printf '%s' "$line" | awk -F'|' '{print $2}')"
        msg="$(printf '%s' "$line" | awk -F'|' '{print $3}')"
        if [[ "$status" == "OK" ]]; then
          DEPOSIT_OK=$((DEPOSIT_OK + 1))
          echo "       ok"
        else
          DEPOSIT_FAIL=$((DEPOSIT_FAIL + 1))
          last_log="$(awk 'NF{last=$0} END{print last}' "$f" 2>/dev/null || true)"
          if [[ -z "$uname" ]]; then
            uname="$(awk '/-- dev[0-9]+:/{for(i=1;i<=NF;i++){if($i ~ /^dev[0-9]+:$/){gsub(":","",$i); print $i; exit}}}' "$f" 2>/dev/null || true)"
          fi
          [[ -z "$uname" ]] && uname="unknown"
          if [[ -z "$msg" && -n "$last_log" ]]; then
            msg="$last_log"
          fi
          echo "warning: deposit failed for $uname${msg:+ ($msg)}" >&2
        fi
      done
      sleep_ms "$BATCH_DELAY_MS"
    done
    rm -rf "$tmpdir"
    if (( DEPOSIT_FAIL > 0 )) && [[ "$FAIL_FAST" == "1" ]]; then
      echo "error: one or more deposits failed and FAIL_FAST=1" >&2
      exit 1
    fi
    echo "    summary: deposits_ok=$DEPOSIT_OK deposits_failed=$DEPOSIT_FAIL"
    return 0
  fi
  for i in $(seq 0 $((USER_COUNT - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "error: missing $name in $DEV_ACCOUNTS_FILE (need USER_COUNT <= generated dev accounts)" >&2
      exit 1
    fi
    echo "  -- $name: approve bond + deposit into pool"
    if approve_and_deposit "$pk" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$DEPOSIT_AMOUNT_WEI" "$EVM_RPC"; then
      DEPOSIT_OK=$((DEPOSIT_OK + 1))
      echo "       ok"
    else
      DEPOSIT_FAIL=$((DEPOSIT_FAIL + 1))
      echo "warning: deposit failed for $name" >&2
      if [[ "$FAIL_FAST" == "1" ]]; then
        exit 1
      fi
    fi
    if [[ "$DEPOSIT_INTERVAL_SECS" =~ ^[0-9]+$ ]] && (( DEPOSIT_INTERVAL_SECS > 0 )) && (( i < USER_COUNT - 1 )); then
      sleep "$DEPOSIT_INTERVAL_SECS"
    fi
  done
  echo "    summary: deposits_ok=$DEPOSIT_OK deposits_failed=$DEPOSIT_FAIL"
}

resolve_pool_owner_pk() {
  if [[ -n "${POOL_OWNER_PK:-}" ]]; then
    return 0
  fi
  POOL_OWNER_PK="$(dev_account_private_key_from_file "dev0" "$DEV_ACCOUNTS_FILE" || true)"
}

# Latest EVM block header time as unix seconds (for maturity vs wall clock).
block_timestamp_unix() {
  cast block latest --rpc-url "$EVM_RPC" --json 2>/dev/null | jq -r '.timestamp // empty' | python3 -c "
import sys
s=sys.stdin.read().strip()
if not s:
    print(0)
elif s.startswith('0x'):
    print(int(s,16))
else:
    print(int(s))
"
}

# withdrawRequests(requestId) maturity field as unix seconds (parsed from cast output).
withdraw_request_maturity_unix() {
  local rid="$1"
  cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" \
    "withdrawRequests(uint256)(address,uint256,uint64,bool,bool)" "$rid" 2>/dev/null \
    | python3 -c "
import sys
lines=[l.strip() for l in sys.stdin if l.strip()]
if len(lines) < 3:
    print(0)
    sys.exit(0)
m = lines[2].split()[0]
if m.startswith('0x'):
    print(int(m, 16))
else:
    print(int(m.split('[')[0].strip()))
"
}

# Optional harvest / sleep / pre-claim before withdraw loop.
run_pre_withdraw_reward_sync() {
  resolve_pool_owner_pk
  log_flow_section "Pre-withdraw (optional reward sync)" \
    "By default we do not call claimRewards() here: withdraw() pulls pending rewards via _claimPendingRewards. Enable PRE_WITHDRAW_CLAIM_REWARDS=1 to force claimRewards() before withdraw."
  if [[ "${PRE_WITHDRAW_HARVEST:-0}" == "1" && -n "${POOL_OWNER_PK:-}" ]]; then
    echo "  -- manual harvest() (debug; module EndBlock also harvests)"
    if ! cast_send_expect_success "$EVM_RPC" "$POOL_OWNER_PK" "$POOL_EVM_ADDR" "harvest()"; then
      echo "warning: manual harvest() reverted; continuing." >&2
    fi
  elif [[ "${PRE_WITHDRAW_HARVEST:-0}" == "1" ]]; then
    echo "warning: PRE_WITHDRAW_HARVEST=1 but no POOL_OWNER_PK — skipping harvest" >&2
  fi
  if [[ "${REWARD_SYNC_WAIT_SECS:-0}" =~ ^[0-9]+$ ]] && (( REWARD_SYNC_WAIT_SECS > 0 )); then
    echo "  -- sleeping ${REWARD_SYNC_WAIT_SECS}s (REWARD_SYNC_WAIT_SECS)"
    sleep "$REWARD_SYNC_WAIT_SECS"
  fi
  if [[ "${PRE_WITHDRAW_CLAIM_REWARDS:-0}" != "1" ]]; then
    echo "    skipping standalone claimRewards(); withdraw() will settle pending pool rewards."
    return 0
  fi
  local i name pk
  for i in $(seq 0 $((WITHDRAW_USERS - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    [[ -z "$pk" ]] && continue
    echo "  -- PRE_WITHDRAW_CLAIM_REWARDS: claimRewards() for $name"
    cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimRewards()" || {
      echo "error: claimRewards failed for $name" >&2
      exit 1
    }
  done
}

# Spin until block time >= maturity or wall-clock timeout.
wait_until_mature_or_timeout() {
  local rid="$1"
  local max_sec="${2:-240}"
  local start mt bt
  start="$(date +%s)"
  mt="$(withdraw_request_maturity_unix "$rid")"
  if [[ "$mt" == "0" ]]; then
    sleep 3
    mt="$(withdraw_request_maturity_unix "$rid")"
  fi
  if [[ "$mt" == "0" ]]; then
    echo "error: withdrawRequests($rid) has maturity 0 (withdraw tx may have reverted)" >&2
    return 1
  fi
  echo "    requestId=$rid  maturityUnix=$mt  (claimWithdraw allowed when latest block time >= this)"
  echo "    polling until block time catches up (max ${max_sec}s wall clock)..."
  while true; do
    bt="$(block_timestamp_unix)"
    if [[ "$bt" =~ ^[0-9]+$ ]] && [[ "$mt" =~ ^[0-9]+$ ]] && (( mt > 0 && bt >= mt )); then
      echo "    maturity reached: latest blockTime=$bt >= maturityTime=$mt"
      return 0
    fi
    if (( $(date +%s) - start > max_sec )); then
      echo "error: timeout waiting for maturity (requestId=$rid blockTime=$bt maturity=$mt)" >&2
      return 1
    fi
    sleep 2
  done
}

# min(units, max(1, units * bp / 10000)) — basis points fraction of LP units to exit.
compute_withdraw_units() {
  local units="$1"
  local bp="$2"
  local out
  if ! [[ "$units" =~ ^[0-9]+$ ]] || [[ "$units" == "0" ]]; then
    echo "0"
    return 0
  fi
  if command -v python3 >/dev/null 2>&1; then
    out="$(python3 -c "u=int('$units'); b=int('$bp'); print(min(u, max(1, u * b // 10000)))" 2>/dev/null || echo "0")"
  else
    out=$(( units * bp / 10000 ))
    if (( out < 1 && units > 0 )); then
      out=1
    fi
    if (( out > units )); then
      out=$units
    fi
  fi
  printf '%s' "$out"
}

run_one_withdraw_submit() {
  local i="$1"
  local name pk addr units wunits wsubmit=0 withdraw_retries_used=0
  name="dev${i}"
  pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "FAIL|$name||0|missing private key"
    return 0
  fi
  addr="$(cast wallet address --private-key "$pk")"
  units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$addr")"
  if [[ ! "$units" =~ ^[0-9]+$ ]] || [[ "$units" == "n/a" ]]; then
    echo "FAIL|$name||0|could not read unitsOf"
    return 0
  fi
  wunits="$(compute_withdraw_units "$units" "$WITHDRAW_FRACTION_BP")"
  if [[ "$wunits" == "0" ]]; then
    echo "SKIP|$name||0|withdraw units computed to 0"
    return 0
  fi
  echo "  -- $name: units=$units  withdrawUnits=$wunits"
  log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_withdraw"
  for _try in $(seq 1 "$WITHDRAW_SUBMIT_RETRIES"); do
    if CAST_SEND_GAS_LIMIT="${WITHDRAW_CLAIM_GAS_LIMIT}" cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "withdraw(uint256)" "$wunits"; then
      wsubmit=1
      withdraw_retries_used=$((_try - 1))
      break
    fi
    echo "       withdraw attempt $_try/$WITHDRAW_SUBMIT_RETRIES reverted; sleep ${WITHDRAW_RETRY_SLEEP_SECS}s then retry"
    sleep "${WITHDRAW_RETRY_SLEEP_SECS}"
  done
  if [[ "$wsubmit" == "1" ]]; then
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_withdraw"
    echo "OK|$name||$withdraw_retries_used|"
  else
    echo "FAIL|$name||$withdraw_retries_used|withdraw failed after retries"
  fi
}

run_one_claim_withdraw() {
  local rid="$1" name="$2"
  local pk addr attempt=0 claim_ok=0
  pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "FAIL|$name|$rid|0|missing private key"
    return 0
  fi
  log_flow_section "claimWithdraw for requestId=$rid ($name)" \
    "After maturity, claimWithdraw returns principal + bond to the user (may retry if pool liquidity is still settling)."
  if ! wait_until_mature_or_timeout "$rid" 300; then
    echo "FAIL|$name|$rid|0|maturity wait failed"
    return 0
  fi
  addr="$(cast wallet address --private-key "$pk")"
  log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_claimWithdraw"
  echo "  -- sending claimWithdraw(uint256) requestId=$rid"
  while (( attempt < CLAIM_POLL_MAX_ATTEMPTS )); do
    if CAST_SEND_GAS_LIMIT="${WITHDRAW_CLAIM_GAS_LIMIT}" cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimWithdraw(uint256)" "$rid"; then
      echo "       claim ok requestId=$rid"
      log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_claimWithdraw"
      claim_ok=1
      break
    fi
    attempt=$((attempt + 1))
    echo "       claim retry $attempt/$CLAIM_POLL_MAX_ATTEMPTS (insufficient liquid or still settling)..."
    sleep "$CLAIM_POLL_INTERVAL_SECS"
  done
  if (( claim_ok == 1 )); then
    echo "OK|$name|$rid|$attempt|"
  else
    echo "FAIL|$name|$rid|$attempt|claim failed after retries"
  fi
}

run_one_post_claim_rewards() {
  local i="$1"
  local name pk addr lb la delta
  name="dev${i}"
  pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "SKIP|$name||missing private key"
    return 0
  fi
  addr="$(cast wallet address --private-key "$pk")"
  lb="$(normalize_cast_balance_wei "$(cast balance --rpc-url "$EVM_RPC" "$addr" 2>/dev/null || true)")"
  echo "  -- $name: claimRewards()  (liquid_native before=$lb wei)"
  log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_claimRewards_postwithdraw"
  if ! cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimRewards()"; then
    echo "FAIL|$name||claimRewards failed"
    return 0
  fi
  la="$(normalize_cast_balance_wei "$(cast balance --rpc-url "$EVM_RPC" "$addr" 2>/dev/null || true)")"
  log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_claimRewards_postwithdraw"
  if [[ "$lb" =~ ^[0-9]+$ && "$la" =~ ^[0-9]+$ ]]; then
    delta="$(python3 -c "print(int('$la') - int('$lb'))")"
    echo "       liquid_native delta (after - before) = $delta wei"
  else
    echo "       liquid_native delta = n/a (could not parse cast balance)"
  fi
  echo "OK|$name||"
}

# Withdraw queue → sleep unbonding → claimWithdraw each captured requestId in order.
run_withdraw_and_claim() {
  if [[ "$WITHDRAW_USERS" == "0" ]]; then
    log_flow_section "Withdraw / claim (skipped)" "WITHDRAW_USERS=0 — deposits only."
    return 0
  fi
  wait_for_total_staked 240

  run_pre_withdraw_reward_sync

  local i pk name addr units wunits rid ub_sec wait_sec
  local rid_window_start rid_window_end
  local stress_mode
  stress_mode=0
  if is_stress_mode_active; then
    stress_mode=1
  fi
  ub_sec="$(parse_unbonding_seconds "$(evmd query staking params --node "$NODE_RPC" -o json | jq -r '.params.unbonding_time // "30s"')")"
  log_flow_section "Submit withdraw requests (first $WITHDRAW_USERS users)" \
    "For each user: read unitsOf, compute withdrawUnits = units * WITHDRAW_FRACTION_BP / 10000, call withdraw(withdrawUnits). Snapshots show before/after each tx." \
    "WITHDRAW_FRACTION_BP=$WITHDRAW_FRACTION_BP (basis points; 1000 = 10%). Staking unbonding_time ~${ub_sec}s; we then sleep unbonding+${UNBONDING_WAIT_BUFFER_SECS}s before maturity wait."

  declare -a RIDS=()
  declare -a RID_OWNERS=()
  declare -a WITHDRAW_USER_NAMES=()
  declare -a WITHDRAW_USER_ADDRS=()

  for i in $(seq 0 $((WITHDRAW_USERS - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "error: missing $name for withdraw" >&2
      exit 1
    fi
    WITHDRAW_USER_NAMES+=("$name")
    WITHDRAW_USER_ADDRS+=("$(cast wallet address --private-key "$pk")")
  done
  rid_window_start="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "nextWithdrawRequestId()(uint256)")"
  WITHDRAW_REQUEST_WINDOW_START="$rid_window_start"

  if is_parallel_mode_active && (( WITHDRAW_CONCURRENCY > 1 )); then
    local batch_start batch_end conc tmpdir
    local total_withdraw_batches
    conc="$WITHDRAW_CONCURRENCY"
    tmpdir="$(mktemp -d -t user_flow_wsub.XXXXXX)"
    total_withdraw_batches=$(( (WITHDRAW_USERS + conc - 1) / conc ))
    for batch_start in $(seq 0 "$conc" $((WITHDRAW_USERS - 1))); do
      batch_end=$((batch_start + conc - 1))
      (( batch_end >= WITHDRAW_USERS )) && batch_end=$((WITHDRAW_USERS - 1))
      local batch_num=$((batch_start / conc + 1))
      echo "  -- withdraw submit batch ${batch_num}/${total_withdraw_batches}: users dev${batch_start}..dev${batch_end}"
      local pids=() files=()
      for i in $(seq "$batch_start" "$batch_end"); do
        local f="$tmpdir/wsub_${i}.out"
        files+=("$f")
        ( run_one_withdraw_submit "$i" ) >"$f" 2>&1 &
        pids+=("$!")
      done
      for p in "${pids[@]}"; do wait "$p"; done
      for f in "${files[@]}"; do
        local line status uname rid_out retries msg
        line="$(awk '/^(OK|FAIL|SKIP)\|/{print; exit}' "$f" 2>/dev/null || true)"
        status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
        uname="$(printf '%s' "$line" | awk -F'|' '{print $2}')"
        rid_out="$(printf '%s' "$line" | awk -F'|' '{print $3}')"
        retries="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
        msg="$(printf '%s' "$line" | awk -F'|' '{print $5}')"
        [[ "$status" == "SKIP" ]] && continue
        WITHDRAW_SUBMIT_ATTEMPTED=$((WITHDRAW_SUBMIT_ATTEMPTED + 1))
        [[ "$retries" =~ ^[0-9]+$ ]] || retries=0
        WITHDRAW_SUBMIT_RETRIES_TOTAL=$((WITHDRAW_SUBMIT_RETRIES_TOTAL + retries))
        if [[ "$status" == "OK" ]]; then
          WITHDRAW_SUBMIT_SUCCESS=$((WITHDRAW_SUBMIT_SUCCESS + 1))
          :
        else
          WITHDRAW_SUBMIT_FAILED=$((WITHDRAW_SUBMIT_FAILED + 1))
          echo "error: withdraw failed for $uname${msg:+ ($msg)}" >&2
          if (( stress_mode != 1 )); then
            rm -rf "$tmpdir"
            exit 1
          fi
        fi
      done
      echo "  -- withdraw submit batch ${batch_num}/${total_withdraw_batches} complete: attempted=$WITHDRAW_SUBMIT_ATTEMPTED success=$WITHDRAW_SUBMIT_SUCCESS failed=$WITHDRAW_SUBMIT_FAILED"
      log_contract_snapshot_for_batch "withdraw_submit" "${batch_num}/${total_withdraw_batches}"
      sleep_ms "$BATCH_DELAY_MS"
    done
    rm -rf "$tmpdir"
  else
    for i in $(seq 0 $((WITHDRAW_USERS - 1))); do
      line="$(run_one_withdraw_submit "$i" | awk '/^(OK|FAIL|SKIP)\|/{x=$0} END{print x}')"
      status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
      name="$(printf '%s' "$line" | awk -F'|' '{print $2}')"
      rid="$(printf '%s' "$line" | awk -F'|' '{print $3}')"
      retries="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
      msg="$(printf '%s' "$line" | awk -F'|' '{print $5}')"
      [[ "$status" == "SKIP" ]] && continue
      WITHDRAW_SUBMIT_ATTEMPTED=$((WITHDRAW_SUBMIT_ATTEMPTED + 1))
      [[ "$retries" =~ ^[0-9]+$ ]] || retries=0
      WITHDRAW_SUBMIT_RETRIES_TOTAL=$((WITHDRAW_SUBMIT_RETRIES_TOTAL + retries))
      if [[ "$status" != "OK" ]]; then
        WITHDRAW_SUBMIT_FAILED=$((WITHDRAW_SUBMIT_FAILED + 1))
        echo "error: withdraw failed for $name${msg:+ ($msg)}" >&2
        if (( stress_mode == 1 )); then
          echo "warning: stress mode enabled, continuing after withdraw failure for $name" >&2
          continue
        fi
        exit 1
      fi
      WITHDRAW_SUBMIT_SUCCESS=$((WITHDRAW_SUBMIT_SUCCESS + 1))
      :
    done
  fi

  rid_window_end="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "nextWithdrawRequestId()(uint256)")"
  WITHDRAW_REQUEST_WINDOW_END="$rid_window_end"
  if [[ "$rid_window_start" =~ ^[0-9]+$ && "$rid_window_end" =~ ^[0-9]+$ ]] && (( rid_window_end > rid_window_start )); then
    for rid in $(seq "$rid_window_start" $((rid_window_end - 1))); do
      local owner_line owner_addr owner_name
      owner_line="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "withdrawRequests(uint256)(address,uint256,uint64,bool,bool)" "$rid" 2>/dev/null | awk 'NF{print; exit}' || true)"
      owner_addr="$(printf '%s' "$owner_line" | awk '{print $1}')"
      owner_name=""
      local owner_addr_lc candidate_addr_lc
      owner_addr_lc="$(printf '%s' "$owner_addr" | tr '[:upper:]' '[:lower:]')"
      for idx in "${!WITHDRAW_USER_ADDRS[@]}"; do
        candidate_addr_lc="$(printf '%s' "${WITHDRAW_USER_ADDRS[$idx]}" | tr '[:upper:]' '[:lower:]')"
        if [[ "$candidate_addr_lc" == "$owner_addr_lc" ]]; then
          owner_name="${WITHDRAW_USER_NAMES[$idx]}"
          break
        fi
      done
      if [[ -n "$owner_name" ]]; then
        RIDS+=("$rid")
        RID_OWNERS+=("$owner_name")
      fi
    done
  fi

  WITHDRAW_REQUESTS_MAPPED="${#RIDS[@]}"
  echo "    mapped withdraw requests for claims: ${#RIDS[@]} (submitted_success=$WITHDRAW_SUBMIT_SUCCESS)"
  if (( ${#RIDS[@]} == 0 )); then
    echo "warning: no withdraw txs executed" >&2
    return 0
  fi

  wait_sec=$(( ub_sec + UNBONDING_WAIT_BUFFER_SECS ))
  log_flow_section "Wait for unbonding (wall clock)" \
    "Sleeping ${wait_sec}s = staking unbonding_time (${ub_sec}s) + UNBONDING_WAIT_BUFFER_SECS (${UNBONDING_WAIT_BUFFER_SECS}s). Then we wait for each withdraw request’s on-chain maturity time."
  sleep "$wait_sec"

  if is_parallel_mode_active && (( CLAIM_CONCURRENCY > 1 )); then
    local cconc ctmpdir cstart cend
    local total_claim_batches
    cconc="$CLAIM_CONCURRENCY"
    ctmpdir="$(mktemp -d -t user_flow_claim.XXXXXX)"
    local total_claims="${#RIDS[@]}"
    total_claim_batches=$(( (total_claims + cconc - 1) / cconc ))
    for cstart in $(seq 0 "$cconc" $((total_claims - 1))); do
      cend=$((cstart + cconc - 1))
      (( cend >= total_claims )) && cend=$((total_claims - 1))
      local cbatch_num=$((cstart / cconc + 1))
      echo "  -- claimWithdraw batch ${cbatch_num}/${total_claim_batches}: requests index ${cstart}..${cend}"
      local pids=() files=()
      for idx in $(seq "$cstart" "$cend"); do
        rid="${RIDS[$idx]}"
        name="${RID_OWNERS[$idx]}"
        local f="$ctmpdir/claim_${idx}.out"
        files+=("$f")
        ( run_one_claim_withdraw "$rid" "$name" ) >"$f" 2>&1 &
        pids+=("$!")
      done
      for p in "${pids[@]}"; do wait "$p"; done
      for f in "${files[@]}"; do
        local line status retries msg rid_out
        line="$(awk '/^(OK|FAIL)\|/{print; exit}' "$f" 2>/dev/null || true)"
        status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
        rid_out="$(printf '%s' "$line" | awk -F'|' '{print $3}')"
        retries="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
        msg="$(printf '%s' "$line" | awk -F'|' '{print $5}')"
        CLAIM_ATTEMPTED=$((CLAIM_ATTEMPTED + 1))
        [[ "$retries" =~ ^[0-9]+$ ]] || retries=0
        CLAIM_RETRIES_TOTAL=$((CLAIM_RETRIES_TOTAL + retries))
        if [[ "$status" == "OK" ]]; then
          CLAIM_SUCCESS=$((CLAIM_SUCCESS + 1))
        else
          CLAIM_FAILED=$((CLAIM_FAILED + 1))
          echo "error: claim failed for requestId=$rid_out${msg:+ ($msg)}" >&2
          if (( stress_mode != 1 )); then
            rm -rf "$ctmpdir"
            exit 1
          fi
        fi
      done
      echo "  -- claimWithdraw batch ${cbatch_num}/${total_claim_batches} complete: attempted=$CLAIM_ATTEMPTED success=$CLAIM_SUCCESS failed=$CLAIM_FAILED"
      log_contract_snapshot_for_batch "claimWithdraw" "${cbatch_num}/${total_claim_batches}"
      sleep_ms "$BATCH_DELAY_MS"
    done
    rm -rf "$ctmpdir"
  else
    for idx in "${!RIDS[@]}"; do
      rid="${RIDS[$idx]}"
      name="${RID_OWNERS[$idx]}"
      line="$(run_one_claim_withdraw "$rid" "$name" | awk '/^(OK|FAIL)\|/{x=$0} END{print x}')"
      status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
      retries="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
      msg="$(printf '%s' "$line" | awk -F'|' '{print $5}')"
      CLAIM_ATTEMPTED=$((CLAIM_ATTEMPTED + 1))
      [[ "$retries" =~ ^[0-9]+$ ]] || retries=0
      CLAIM_RETRIES_TOTAL=$((CLAIM_RETRIES_TOTAL + retries))
      if [[ "$status" != "OK" ]]; then
        CLAIM_FAILED=$((CLAIM_FAILED + 1))
        echo "error: claim failed for requestId=$rid${msg:+ ($msg)}" >&2
        if (( stress_mode == 1 )); then
          echo "warning: stress mode enabled, continuing after claim failure for requestId=$rid" >&2
          continue
        fi
        exit 1
      fi
      CLAIM_SUCCESS=$((CLAIM_SUCCESS + 1))
    done
  fi

  local verified_claimed=0
  for rid in "${RIDS[@]}"; do
    local claimed_flag
    claimed_flag="$(withdraw_request_claimed_flag "$rid")"
    if [[ "$claimed_flag" == "true" ]]; then
      verified_claimed=$((verified_claimed + 1))
    fi
  done
  WITHDRAW_REQUESTS_CLAIMED_VERIFIED="$verified_claimed"
  echo "    verified claim flags on-chain: claimed=$verified_claimed/${#RIDS[@]}"
}

# After claimWithdraws: optional extra claimRewards per user (reward index path vs withdraw-embedded).
run_post_claimwithdraw_claim_rewards() {
  if [[ "${POST_CLAIMWITHDRAW_CLAIM_REWARDS:-0}" != "1" ]]; then
    return 0
  fi
  if [[ "$WITHDRAW_USERS" == "0" ]]; then
    echo "POST_CLAIMWITHDRAW_CLAIM_REWARDS=1 but WITHDRAW_USERS=0 — skipping (no claimWithdraw phase)" >&2
    return 0
  fi

  local n="${POST_CLAIMWITHDRAW_USERS:-}"
  if [[ -z "$n" ]]; then
    n="$USER_COUNT"
  fi
  if ! [[ "$n" =~ ^[0-9]+$ ]] || (( n < 1 )); then
    echo "error: POST_CLAIMWITHDRAW_USERS must be a positive integer" >&2
    exit 1
  fi

  local h0 h1
  h0="$(cast block-number --rpc-url "$EVM_RPC" 2>/dev/null || echo "?")"
  log_flow_section "Standalone claimRewards() (post claimWithdraw)" \
    "Optional path: after all claimWithdraws, wait POST_CLAIMWITHDRAW_WAIT_SECS=(${POST_CLAIMWITHDRAW_WAIT_SECS}s) so more blocks accrue rewards, then claimRewards() per user." \
    "This exercises reward accounting vs rewards already folded into withdraw(). First block height ≈ $h0."
  sleep "${POST_CLAIMWITHDRAW_WAIT_SECS}"
  h1="$(cast block-number --rpc-url "$EVM_RPC" 2>/dev/null || echo "?")"
  echo "    after wait: block≈$h1 — claiming for $n user(s) (dev0..dev$((n - 1)))"

  local i
  if is_parallel_mode_active && (( CLAIM_REWARDS_CONCURRENCY > 1 )); then
    local conc tmpdir batch_start batch_end
    local total_rewards_batches
    conc="$CLAIM_REWARDS_CONCURRENCY"
    tmpdir="$(mktemp -d -t user_flow_creward.XXXXXX)"
    total_rewards_batches=$(( (n + conc - 1) / conc ))
    for batch_start in $(seq 0 "$conc" $((n - 1))); do
      batch_end=$((batch_start + conc - 1))
      (( batch_end >= n )) && batch_end=$((n - 1))
      local rbatch_num=$((batch_start / conc + 1))
      echo "  -- claimRewards batch ${rbatch_num}/${total_rewards_batches}: users dev${batch_start}..dev${batch_end}"
      local pids=() files=()
      for i in $(seq "$batch_start" "$batch_end"); do
        local f="$tmpdir/creward_${i}.out"
        files+=("$f")
        ( run_one_post_claim_rewards "$i" ) >"$f" 2>&1 &
        pids+=("$!")
      done
      for p in "${pids[@]}"; do wait "$p"; done
      for f in "${files[@]}"; do
        local line status uname msg
        line="$(awk '/^(OK|FAIL|SKIP)\|/{print; exit}' "$f" 2>/dev/null || true)"
        status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
        uname="$(printf '%s' "$line" | awk -F'|' '{print $2}')"
        msg="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
        [[ "$status" == "SKIP" ]] && continue
        POST_CLAIM_REWARDS_ATTEMPTED=$((POST_CLAIM_REWARDS_ATTEMPTED + 1))
        if [[ "$status" == "OK" ]]; then
          POST_CLAIM_REWARDS_SUCCESS=$((POST_CLAIM_REWARDS_SUCCESS + 1))
        else
          POST_CLAIM_REWARDS_FAILED=$((POST_CLAIM_REWARDS_FAILED + 1))
          echo "error: claimRewards failed for $uname${msg:+ ($msg)}" >&2
          if ! is_stress_mode_active; then
            rm -rf "$tmpdir"
            exit 1
          fi
        fi
      done
      echo "  -- claimRewards batch ${rbatch_num}/${total_rewards_batches} complete: attempted=$POST_CLAIM_REWARDS_ATTEMPTED success=$POST_CLAIM_REWARDS_SUCCESS failed=$POST_CLAIM_REWARDS_FAILED"
      log_contract_snapshot_for_batch "claimRewards" "${rbatch_num}/${total_rewards_batches}"
      sleep_ms "$BATCH_DELAY_MS"
    done
    rm -rf "$tmpdir"
  else
    for i in $(seq 0 $((n - 1))); do
      local line status name msg
      line="$(run_one_post_claim_rewards "$i" | awk '/^(OK|FAIL|SKIP)\|/{x=$0} END{print x}')"
      status="$(printf '%s' "$line" | awk -F'|' '{print $1}')"
      name="$(printf '%s' "$line" | awk -F'|' '{print $2}')"
      msg="$(printf '%s' "$line" | awk -F'|' '{print $4}')"
      [[ "$status" == "SKIP" ]] && continue
      POST_CLAIM_REWARDS_ATTEMPTED=$((POST_CLAIM_REWARDS_ATTEMPTED + 1))
      if [[ "$status" != "OK" ]]; then
        echo "error: claimRewards failed for $name${msg:+ ($msg)}" >&2
        POST_CLAIM_REWARDS_FAILED=$((POST_CLAIM_REWARDS_FAILED + 1))
        if is_stress_mode_active; then
          echo "warning: stress mode enabled, continuing after claimRewards failure for $name" >&2
          continue
        fi
        exit 1
      fi
      POST_CLAIM_REWARDS_SUCCESS=$((POST_CLAIM_REWARDS_SUCCESS + 1))
    done
  fi
}

print_stress_summary() {
  local runtime_sec withdraw_avg_retries claim_avg_retries claim_completion_rate
  runtime_sec=$((RUN_END_TS - RUN_START_TS))
  withdraw_avg_retries="$(safe_avg_2dp "$WITHDRAW_SUBMIT_RETRIES_TOTAL" "$WITHDRAW_SUBMIT_ATTEMPTED")"
  claim_avg_retries="$(safe_avg_2dp "$CLAIM_RETRIES_TOTAL" "$CLAIM_ATTEMPTED")"
  claim_completion_rate="$(safe_ratio_percent_2dp "$CLAIM_SUCCESS" "$WITHDRAW_SUBMIT_SUCCESS")"

  log_flow_section "Stress profile summary" \
    "profile=$USER_FLOW_STRESS_PROFILE mode=$USER_FLOW_MODE runtime_sec=$runtime_sec" \
    "deposits_ok=$DEPOSIT_OK deposits_failed=$DEPOSIT_FAIL" \
    "withdraw_submit_attempted=$WITHDRAW_SUBMIT_ATTEMPTED succeeded=$WITHDRAW_SUBMIT_SUCCESS failed=$WITHDRAW_SUBMIT_FAILED avg_retries=$withdraw_avg_retries" \
    "claim_attempted=$CLAIM_ATTEMPTED succeeded=$CLAIM_SUCCESS failed=$CLAIM_FAILED avg_retries=$claim_avg_retries completion_rate=${claim_completion_rate}% (claims_succeeded/withdraws_submitted)" \
    "post_claimRewards_attempted=$POST_CLAIM_REWARDS_ATTEMPTED succeeded=$POST_CLAIM_REWARDS_SUCCESS failed=$POST_CLAIM_REWARDS_FAILED"
}

parse_cli() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --deposits-only) WITHDRAW_USERS=0; shift ;;
      *) echo "unknown arg: $1" >&2; usage; exit 1 ;;
    esac
  done
}

apply_stress_profile_overrides() {
  case "${USER_FLOW_STRESS_PROFILE:-}" in
    "" )
      return 0
      ;;
    100users|stress100)
      # Profile defaults apply only when users did not explicitly set env values.
      if (( USER_COUNT_SET_BY_ENV == 0 )); then USER_COUNT=100; fi
      if (( WITHDRAW_USERS_SET_BY_ENV == 0 )); then WITHDRAW_USERS=30; fi
      if (( FAIL_FAST_SET_BY_ENV == 0 )); then FAIL_FAST=0; fi
      if (( DEPOSIT_INTERVAL_SECS_SET_BY_ENV == 0 )); then DEPOSIT_INTERVAL_SECS=0; fi
      if (( WITHDRAW_SUBMIT_RETRIES_SET_BY_ENV == 0 )); then WITHDRAW_SUBMIT_RETRIES=40; fi
      if (( CLAIM_POLL_MAX_ATTEMPTS_SET_BY_ENV == 0 )); then CLAIM_POLL_MAX_ATTEMPTS=180; fi
      if (( USER_FLOW_MODE_SET_BY_ENV == 0 )); then USER_FLOW_MODE=parallel; fi
      if (( DEPOSIT_CONCURRENCY_SET_BY_ENV == 0 )); then DEPOSIT_CONCURRENCY=10; fi
      if (( WITHDRAW_CONCURRENCY_SET_BY_ENV == 0 )); then WITHDRAW_CONCURRENCY=8; fi
      if (( CLAIM_CONCURRENCY_SET_BY_ENV == 0 )); then CLAIM_CONCURRENCY=5; fi
      if (( CLAIM_REWARDS_CONCURRENCY_SET_BY_ENV == 0 )); then CLAIM_REWARDS_CONCURRENCY=12; fi
      if (( BATCH_DELAY_MS_SET_BY_ENV == 0 )); then BATCH_DELAY_MS=100; fi
      # Reduce RPC mempool/nonce contention for high-concurrency stress runs.
      if [[ -z "${CAST_SEND_RESILIENT_MODE+x}" ]]; then CAST_SEND_RESILIENT_MODE=true; fi
      if [[ -z "${DEPOSIT_GAS_ESCALATION+x}" ]]; then DEPOSIT_GAS_ESCALATION=true; fi
      ;;
    *)
      echo "error: unknown USER_FLOW_STRESS_PROFILE=$USER_FLOW_STRESS_PROFILE (expected: 100users|stress100)" >&2
      exit 1
      ;;
  esac
}

validate_flow_knobs() {
  if [[ "$USER_FLOW_MODE" != "serial" && "$USER_FLOW_MODE" != "parallel" ]]; then
    echo "error: USER_FLOW_MODE must be serial or parallel (got: $USER_FLOW_MODE)" >&2
    exit 1
  fi
  for v in DEPOSIT_CONCURRENCY WITHDRAW_CONCURRENCY CLAIM_CONCURRENCY CLAIM_REWARDS_CONCURRENCY; do
    if ! [[ "${!v}" =~ ^[0-9]+$ ]] || (( ${!v} < 1 )); then
      echo "error: $v must be a positive integer (got: ${!v})" >&2
      exit 1
    fi
  done
  if ! [[ "$BATCH_DELAY_MS" =~ ^[0-9]+$ ]]; then
    echo "error: BATCH_DELAY_MS must be a non-negative integer (got: $BATCH_DELAY_MS)" >&2
    exit 1
  fi
}

main() {
  RUN_START_TS="$(date +%s)"
  parse_cli "$@"
  apply_stress_profile_overrides
  validate_flow_knobs
  require_bin jq
  require_bin curl
  require_bin evmd
  require_bin cast

  if [[ ! -f "$DEV_ACCOUNTS_FILE" ]]; then
    echo "error: DEV_ACCOUNTS_FILE not found: $DEV_ACCOUNTS_FILE" >&2
    echo "hint: generate dev accounts via multi_node_startup / rebalance_scenario_runner" >&2
    exit 1
  fi
  log_flow_section "Preflight" \
    "USER_COUNT=$USER_COUNT WITHDRAW_USERS=$WITHDRAW_USERS DEV_ACCOUNTS_FILE=$DEV_ACCOUNTS_FILE" \
    "Existing dev accounts in file: $(count_dev_accounts_in_file "$DEV_ACCOUNTS_FILE")"
  # Needed before auto-provisioning new accounts, which sends funding txs.
  ensure_evm_rpc_ready
  normalize_user_counts_for_available_accounts

  resolve_pool_evm_addr

  log_flow_section "Run summary" \
    "EVM_RPC=$EVM_RPC  NODE_RPC=$NODE_RPC  USER_COUNT=$USER_COUNT  WITHDRAW_USERS=$WITHDRAW_USERS  POST_CLAIMWITHDRAW_CLAIM_REWARDS=${POST_CLAIMWITHDRAW_CLAIM_REWARDS:-0}" \
    "USER_FLOW_STRESS_PROFILE=${USER_FLOW_STRESS_PROFILE:-none} USER_FLOW_MODE=$USER_FLOW_MODE FAIL_FAST=$FAIL_FAST DEPOSIT_INTERVAL_SECS=$DEPOSIT_INTERVAL_SECS WITHDRAW_SUBMIT_RETRIES=$WITHDRAW_SUBMIT_RETRIES CLAIM_POLL_MAX_ATTEMPTS=$CLAIM_POLL_MAX_ATTEMPTS" \
    "DEPOSIT_CONCURRENCY=$DEPOSIT_CONCURRENCY WITHDRAW_CONCURRENCY=$WITHDRAW_CONCURRENCY CLAIM_CONCURRENCY=$CLAIM_CONCURRENCY CLAIM_REWARDS_CONCURRENCY=$CLAIM_REWARDS_CONCURRENCY BATCH_DELAY_MS=$BATCH_DELAY_MS"

  if [[ "$SKIP_DEPOSITS" == "1" ]]; then
    echo "SKIP_DEPOSITS=1 — skipping deposit loop"
  else
    run_deposits
  fi

  log_pool_snapshot
  print_contract_correctness_checks "post-deposits"

  if [[ "$WITHDRAW_USERS" != "0" ]]; then
    run_withdraw_and_claim
    print_contract_correctness_checks "post-claimWithdraw"
  fi

  run_post_claimwithdraw_claim_rewards
  print_contract_correctness_checks "final"

  RUN_END_TS="$(date +%s)"
  if is_stress_mode_active; then
    print_stress_summary
  fi

  log_flow_section "Done" "All requested phases finished. Repo: $ROOT_DIR"
}

main "$@"
