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
USER_COUNT="${USER_COUNT:-5}"
DEPOSIT_AMOUNT_WEI="${DEPOSIT_AMOUNT_WEI:-100000000000000000000}"
DEV_ACCOUNTS_FILE="${DEV_ACCOUNTS_FILE:-$BASEDIR/dev_accounts.txt}"
SKIP_DEPOSITS="${SKIP_DEPOSITS:-0}"
FAIL_FAST="${FAIL_FAST:-1}"
DEPOSIT_INTERVAL_SECS="${DEPOSIT_INTERVAL_SECS:-2}"

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
  bash $0 --deposits-only

Notes:
  - Default: no pre-withdraw claimRewards(); CommunityPool.withdraw() settles pending rewards internally.
  - Withdraw phase logs liquid_native_wei (cast balance), bond_token_wei (ERC20 balanceOf), pool_units at:
    before_withdraw, after_withdraw, before_claimWithdraw, after_claimWithdraw.
  - POST_CLAIMWITHDRAW_CLAIM_REWARDS=1 adds before_claimRewards_postwithdraw / after_claimRewards_postwithdraw and liquid_native_wei_delta per user.
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

# Approve + deposit for dev0..dev(N-1).
run_deposits() {
  local i pk name
  log_flow_section "Deposits ($USER_COUNT accounts)" \
    "Per account: approve bond ERC20 on the bond precompile, then CommunityPool.deposit(amount). Amount wei: $DEPOSIT_AMOUNT_WEI."
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

# Withdraw queue → sleep unbonding → claimWithdraw each captured requestId in order.
run_withdraw_and_claim() {
  if [[ "$WITHDRAW_USERS" == "0" ]]; then
    log_flow_section "Withdraw / claim (skipped)" "WITHDRAW_USERS=0 — deposits only."
    return 0
  fi
  wait_for_total_staked 240

  run_pre_withdraw_reward_sync

  local i pk name addr units wunits rid ub_sec wait_sec
  ub_sec="$(parse_unbonding_seconds "$(evmd query staking params --node "$NODE_RPC" -o json | jq -r '.params.unbonding_time // "30s"')")"
  log_flow_section "Submit withdraw requests (first $WITHDRAW_USERS users)" \
    "For each user: read unitsOf, compute withdrawUnits = units * WITHDRAW_FRACTION_BP / 10000, call withdraw(withdrawUnits). Snapshots show before/after each tx." \
    "WITHDRAW_FRACTION_BP=$WITHDRAW_FRACTION_BP (basis points; 1000 = 10%). Staking unbonding_time ~${ub_sec}s; we then sleep unbonding+${UNBONDING_WAIT_BUFFER_SECS}s before maturity wait."

  declare -a RIDS=()

  for i in $(seq 0 $((WITHDRAW_USERS - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "error: missing $name for withdraw" >&2
      exit 1
    fi
    addr="$(cast wallet address --private-key "$pk")"
    units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$addr")"
    if [[ ! "$units" =~ ^[0-9]+$ ]] || [[ "$units" == "n/a" ]]; then
      echo "error: could not read unitsOf for $name" >&2
      exit 1
    fi
    wunits="$(compute_withdraw_units "$units" "$WITHDRAW_FRACTION_BP")"
    if [[ "$wunits" == "0" ]]; then
      echo "skip $name: withdraw units computed to 0 (units=$units)"
      continue
    fi
    rid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "nextWithdrawRequestId()(uint256)")"
    echo "  -- $name: units=$units  withdrawUnits=$wunits  nextWithdrawRequestId (expected)=$rid"
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_withdraw"
    wsubmit=0
    for _try in $(seq 1 "$WITHDRAW_SUBMIT_RETRIES"); do
      if CAST_SEND_GAS_LIMIT="${WITHDRAW_CLAIM_GAS_LIMIT}" cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "withdraw(uint256)" "$wunits"; then
        wsubmit=1
        break
      fi
      echo "       withdraw attempt $_try/$WITHDRAW_SUBMIT_RETRIES reverted; sleep ${WITHDRAW_RETRY_SLEEP_SECS}s then retry"
      sleep "${WITHDRAW_RETRY_SLEEP_SECS}"
    done
    if [[ "$wsubmit" != "1" ]]; then
      echo "error: withdraw failed for $name after $WITHDRAW_SUBMIT_RETRIES attempts" >&2
      exit 1
    fi
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_withdraw"
    RIDS+=("$rid")
  done

  if (( ${#RIDS[@]} == 0 )); then
    echo "warning: no withdraw txs executed" >&2
    return 0
  fi

  wait_sec=$(( ub_sec + UNBONDING_WAIT_BUFFER_SECS ))
  log_flow_section "Wait for unbonding (wall clock)" \
    "Sleeping ${wait_sec}s = staking unbonding_time (${ub_sec}s) + UNBONDING_WAIT_BUFFER_SECS (${UNBONDING_WAIT_BUFFER_SECS}s). Then we wait for each withdraw request’s on-chain maturity time."
  sleep "$wait_sec"

  local idx=0
  for rid in "${RIDS[@]}"; do
    name="dev${idx}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE")"
    log_flow_section "claimWithdraw for requestId=$rid ($name)" \
      "After maturity, claimWithdraw returns principal + bond to the user (may retry if pool liquidity is still settling)."
    wait_until_mature_or_timeout "$rid" 300 || exit 1
    addr="$(cast wallet address --private-key "$pk")"
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_claimWithdraw"
    echo "  -- sending claimWithdraw(uint256) requestId=$rid"
    attempt=0
    while (( attempt < CLAIM_POLL_MAX_ATTEMPTS )); do
      if CAST_SEND_GAS_LIMIT="${WITHDRAW_CLAIM_GAS_LIMIT}" cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimWithdraw(uint256)" "$rid"; then
        echo "       claim ok requestId=$rid"
        log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_claimWithdraw"
        break
      fi
      attempt=$((attempt + 1))
      echo "       claim retry $attempt/$CLAIM_POLL_MAX_ATTEMPTS (insufficient liquid or still settling)..."
      sleep "$CLAIM_POLL_INTERVAL_SECS"
    done
    if (( attempt >= CLAIM_POLL_MAX_ATTEMPTS )); then
      echo "error: claim failed for requestId=$rid after $CLAIM_POLL_MAX_ATTEMPTS attempts" >&2
      exit 1
    fi
    idx=$((idx + 1))
  done
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

  local i name pk addr lb la delta
  for i in $(seq 0 $((n - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "warning: skip post claimRewards for missing $name" >&2
      continue
    fi
    addr="$(cast wallet address --private-key "$pk")"
    lb="$(normalize_cast_balance_wei "$(cast balance --rpc-url "$EVM_RPC" "$addr" 2>/dev/null || true)")"
    echo "  -- $name: claimRewards()  (liquid_native before=$lb wei)"
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "before_claimRewards_postwithdraw"
    if ! cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimRewards()"; then
      echo "error: claimRewards failed for $name" >&2
      exit 1
    fi
    la="$(normalize_cast_balance_wei "$(cast balance --rpc-url "$EVM_RPC" "$addr" 2>/dev/null || true)")"
    log_user_withdraw_snapshot "$EVM_RPC" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$addr" "$name" "after_claimRewards_postwithdraw"
    if [[ "$lb" =~ ^[0-9]+$ && "$la" =~ ^[0-9]+$ ]]; then
      delta="$(python3 -c "print(int('$la') - int('$lb'))")"
      echo "       liquid_native delta (after - before) = $delta wei"
    else
      echo "       liquid_native delta = n/a (could not parse cast balance)"
    fi
  done
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

main() {
  parse_cli "$@"
  require_bin jq
  require_bin curl
  require_bin evmd
  require_bin cast

  if [[ ! -f "$DEV_ACCOUNTS_FILE" ]]; then
    echo "error: DEV_ACCOUNTS_FILE not found: $DEV_ACCOUNTS_FILE" >&2
    echo "hint: generate dev accounts via multi_node_startup / rebalance_scenario_runner" >&2
    exit 1
  fi

  ensure_evm_rpc_ready
  resolve_pool_evm_addr

  log_flow_section "Run summary" \
    "EVM_RPC=$EVM_RPC  NODE_RPC=$NODE_RPC  USER_COUNT=$USER_COUNT  WITHDRAW_USERS=$WITHDRAW_USERS  POST_CLAIMWITHDRAW_CLAIM_REWARDS=${POST_CLAIMWITHDRAW_CLAIM_REWARDS:-0}"

  if [[ "$SKIP_DEPOSITS" == "1" ]]; then
    echo "SKIP_DEPOSITS=1 — skipping deposit loop"
  else
    run_deposits
  fi

  log_pool_snapshot

  if [[ "$WITHDRAW_USERS" != "0" ]]; then
    run_withdraw_and_claim
  fi

  run_post_claimwithdraw_claim_rewards

  log_flow_section "Done" "All requested phases finished. Repo: $ROOT_DIR"
}

main "$@"
