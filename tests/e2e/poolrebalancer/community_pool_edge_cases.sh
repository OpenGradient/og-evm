#!/usr/bin/env bash
# CommunityPool edge-case E2E (incremental).
# Step 1: auth — non-owner reverts on privileged calls.
# Step 2: drift — owner syncTotalStaked(skew); poll until totalStaked matches staking bonded (reconcile recovery).
# Step 3: withdraw_sizing — optional poll for pendingRebalanceUnbondReserve>0; one withdraw(); assert amountOut
#         formula and that pendingRebalanceUnbondReserve is unchanged (user withdraw does not debit it).
# Step 4: liquidity — create one withdraw request, assert claimWithdraw(requestId) reverts before maturity;
#         optional stress loop retries matured claim to probe liquidity/settling behavior without making the
#         deterministic phase flaky by default.
# Step 5: dust — tiny deposit/withdraw rounding reverts plus owner setConfig boundary + stake no-op checks
#         with config restoration.
# Step 6: rewards — multi-harvest + claimRewards sanity and liquid reserve invariants.
# Requires: running devnet, pool wired (poolrebalancer.params.pool_delegator_address or POOL_CONTRACT_ADDR),
#           BASEDIR/dev_accounts.txt; withdraw_sizing needs LP units on WITHDRAW_SIZING_ACCOUNT (default dev2).
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/lib/pool_e2e_common.sh"

BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-tcp://127.0.0.1:26657}"
CHAIN_ID="${CHAIN_ID:-10740}"
EVM_RPC="${EVM_RPC:-http://127.0.0.1:8545}"
BOND_PRECOMPILE="${BOND_PRECOMPILE:-0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE}"
CHAIN_HOME="${CHAIN_HOME:-$BASEDIR/val0}"
POOL_CONTRACT_ADDR="${POOL_CONTRACT_ADDR:-}"
DEV_ACCOUNTS_FILE="${DEV_ACCOUNTS_FILE:-$BASEDIR/dev_accounts.txt}"

# Phases are set by: (1) first argv to this script (auth|drift|withdraw_sizing|liquidity|all|comma list),
# (2) env COMMUNITY_POOL_EDGE_PHASES, (3) default auth only.
# Private key source: dev account that is not pool owner and not automationCaller (default dev1).
AUTH_NON_OWNER_ACCOUNT="${AUTH_NON_OWNER_ACCOUNT:-dev1}"
# Pool owner key for syncTotalStaked (default: dev0 from DEV_ACCOUNTS_FILE if unset).
POOL_OWNER_PK="${POOL_OWNER_PK:-}"
# Added to on-chain totalStaked to simulate bookkeeping drift (wei).
DRIFT_SKEW_WEI="${DRIFT_SKEW_WEI:-1000000000000000000}"
# Wall-clock timeout waiting for reconcile to restore totalStaked vs staking delegations sum.
DRIFT_RECOVER_MAX_WAIT_SECS="${DRIFT_RECOVER_MAX_WAIT_SECS:-180}"
# Optional: pool delegator bech32 override if params cannot be queried.
POOL_DEL_BECH32="${POOL_DEL_BECH32:-}"

# Step 3: account used for withdraw sizing (script can auto-deposit if units are missing).
WITHDRAW_SIZING_ACCOUNT="${WITHDRAW_SIZING_ACCOUNT:-dev2}"
WITHDRAW_SIZING_FRACTION_BP="${WITHDRAW_SIZING_FRACTION_BP:-1000}"
# Poll for pendingRebalanceUnbondReserve > 0 (optional; 0 = skip poll).
WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS="${WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS:-60}"
WITHDRAW_SIZING_GAS_LIMIT="${WITHDRAW_SIZING_GAS_LIMIT:-9500000}"
# Ordered fallback BPs tried when withdraw() reverts at the primary fraction.
WITHDRAW_SIZING_CANDIDATE_BP_LIST="${WITHDRAW_SIZING_CANDIDATE_BP_LIST:-1000,500,200,100,50,20,10,5,1}"
# Auto-deposit fallback for withdraw_sizing when target account has no units / pool totals are zero.
WITHDRAW_SIZING_AUTO_DEPOSIT="${WITHDRAW_SIZING_AUTO_DEPOSIT:-1}"
WITHDRAW_SIZING_AUTO_DEPOSIT_USERS="${WITHDRAW_SIZING_AUTO_DEPOSIT_USERS:-3}"
WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI="${WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI:-100000000000000000000}"
WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS="${WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS:-1}"
WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS="${WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS:-120}"
# Step 4: claimWithdraw maturity revert always; optional best-effort retry at maturity.
LIQUIDITY_ACCOUNT="${LIQUIDITY_ACCOUNT:-$WITHDRAW_SIZING_ACCOUNT}"
LIQUIDITY_FRACTION_BP="${LIQUIDITY_FRACTION_BP:-$WITHDRAW_SIZING_FRACTION_BP}"
LIQUIDITY_CANDIDATE_BP_LIST="${LIQUIDITY_CANDIDATE_BP_LIST:-$WITHDRAW_SIZING_CANDIDATE_BP_LIST}"
LIQUIDITY_GAS_LIMIT="${LIQUIDITY_GAS_LIMIT:-$WITHDRAW_SIZING_GAS_LIMIT}"
LIQUIDITY_MATURITY_MAX_WAIT_SECS="${LIQUIDITY_MATURITY_MAX_WAIT_SECS:-300}"
CLAIM_STRESS_INSUFFICIENT_LIQUID="${CLAIM_STRESS_INSUFFICIENT_LIQUID:-0}"
CLAIM_STRESS_MAX_ATTEMPTS="${CLAIM_STRESS_MAX_ATTEMPTS:-20}"
CLAIM_STRESS_POLL_INTERVAL_SECS="${CLAIM_STRESS_POLL_INTERVAL_SECS:-2}"
# Step 5: dust / config.
DUST_ACCOUNT="${DUST_ACCOUNT:-dev1}"
DUST_SECONDARY_ACCOUNT="${DUST_SECONDARY_ACCOUNT:-dev2}"
DUST_SEED_DEPOSIT_AMOUNT_WEI="${DUST_SEED_DEPOSIT_AMOUNT_WEI:-1000000000000000000}"
DUST_BOUNDARY_MAX_VALIDATORS="${DUST_BOUNDARY_MAX_VALIDATORS:-1}"
DUST_HIGH_MIN_STAKE_AMOUNT_WEI="${DUST_HIGH_MIN_STAKE_AMOUNT_WEI:-115792089237316195423570985008687907853269984665640564039457584007913129639935}"
# Step 6: rewards / invariants.
REWARDS_ACCOUNT="${REWARDS_ACCOUNT:-dev1}"
REWARDS_HARVEST_COUNT="${REWARDS_HARVEST_COUNT:-3}"
REWARDS_HARVEST_INTERVAL_SECS="${REWARDS_HARVEST_INTERVAL_SECS:-1}"
SKIP_EMPTY_POOL_HARVEST="${SKIP_EMPTY_POOL_HARVEST:-1}"

REQUEST_SETUP_PK=""
REQUEST_SETUP_USER_ADDR=""
REQUEST_SETUP_REQUEST_ID=""
REQUEST_SETUP_WITHDRAW_UNITS=""
REQUEST_SETUP_EXPECTED_OUT=""

uint256_add() {
  local a="${1:-0}" b="${2:-0}"
  python3 -c "print(int('$a') + int('$b'))" 2>/dev/null
}

uint256_sub_nonnegative() {
  local a="${1:-0}" b="${2:-0}"
  python3 -c "print(max(0, int('$a') - int('$b')))" 2>/dev/null
}

uint256_pending_rewards_from_index() {
  local units="${1:-0}" acc="${2:-0}" debt="${3:-0}"
  python3 -c "print(max(0, (int('$units') * int('$acc')) // 10**18 - int('$debt')))" 2>/dev/null
}

uint256_sub_strict() {
  local a="${1:-0}" b="${2:-0}"
  python3 - "$a" "$b" <<'PY' 2>/dev/null
import sys
a = int(sys.argv[1])
b = int(sys.argv[2])
if a < b:
    sys.exit(1)
print(a - b)
PY
}

receipt_json_effective_fee_wei() {
  local json="${1:-}"
  python3 - <<'PY' "$json" 2>/dev/null
import json
import sys

raw = sys.argv[1]
obj = json.loads(raw)

def parse_uint(v):
    if v is None:
        return None
    if isinstance(v, int):
        return v
    s = str(v).strip()
    if not s:
        return None
    if s.startswith(("0x", "0X")):
        return int(s, 16)
    return int(s)

gas_used = parse_uint(obj.get("gasUsed"))
gas_price = parse_uint(obj.get("effectiveGasPrice"))
if gas_price is None:
    gas_price = parse_uint(obj.get("gasPrice"))
if gas_used is None or gas_price is None:
    sys.exit(1)
print(gas_used * gas_price)
PY
}

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

# POOL_CONTRACT_ADDR wins for EVM 0x; pool delegator bech32 always from params (or POOL_DEL_BECH32 override).
resolve_pool_evm_addr() {
  local params del_from_params
  params="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json 2>/dev/null || true)"
  del_from_params="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
  if [[ -z "$POOL_DEL_BECH32" && -n "$del_from_params" ]]; then
    POOL_DEL_BECH32="$del_from_params"
  fi

  if [[ -n "$POOL_CONTRACT_ADDR" ]]; then
    POOL_EVM_ADDR="$POOL_CONTRACT_ADDR"
    log_flow_section "Pool contract (from env)" \
      "Using POOL_CONTRACT_ADDR from environment; pool delegator bech32 from params (or POOL_DEL_BECH32)."
    echo "    POOL_CONTRACT_ADDR=$POOL_EVM_ADDR"
    if [[ -z "$POOL_DEL_BECH32" ]]; then
      echo "error: pool_delegator_address empty and POOL_DEL_BECH32 unset; cannot query staking for drift" >&2
      exit 1
    fi
    echo "    pool_delegator_bech32  $POOL_DEL_BECH32"
    return 0
  fi

  log_flow_section "Resolve CommunityPool from chain" \
    "Reading x/poolrebalancer params for pool_delegator_address, then mapping bech32 to EVM 0x for cast calls."
  if [[ -z "$POOL_DEL_BECH32" ]]; then
    echo "error: set POOL_CONTRACT_ADDR or configure poolrebalancer.params.pool_delegator_address" >&2
    exit 1
  fi
  POOL_EVM_ADDR="$(resolve_evm_hex_from_bech32 "$POOL_DEL_BECH32")"
  if [[ -z "$POOL_EVM_ADDR" || "$POOL_EVM_ADDR" == "0x" ]]; then
    echo "error: could not resolve EVM address for pool delegator $POOL_DEL_BECH32" >&2
    exit 1
  fi
  echo "    pool_delegator_bech32  $POOL_DEL_BECH32"
  echo "    pool_evm               $POOL_EVM_ADDR"
}

phase_enabled() {
  local want="$1"
  local IFS=','
  local p
  for p in $COMMUNITY_POOL_EDGE_PHASES; do
    [[ "$p" == "$want" ]] && return 0
  done
  return 1
}

run_phase_auth() {
  local pk
  log_flow_section "Phase auth" \
    "Non-owner EOA must revert: reconcileStakedBuckets (automationCaller-only), creditStakeableFromRebalance (owner|automation), syncTotalStaked (owner-only)." \
    "Signer: AUTH_NON_OWNER_ACCOUNT=$AUTH_NON_OWNER_ACCOUNT"
  pk="$(dev_account_private_key_from_file "$AUTH_NON_OWNER_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "error: missing $AUTH_NON_OWNER_ACCOUNT in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi

  echo "  -- reconcileStakedBuckets(0,0) expect revert"
  cast_send_expect_revert "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" \
    "reconcileStakedBuckets(uint256,uint256)" 0 0 || exit 1

  echo "  -- creditStakeableFromRebalance(1) expect revert"
  cast_send_expect_revert "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" \
    "creditStakeableFromRebalance(uint256)" 1 || exit 1

  echo "  -- syncTotalStaked(1) expect revert"
  cast_send_expect_revert "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" \
    "syncTotalStaked(uint256)" 1 || exit 1

  echo "    auth phase: ok"
}

resolve_pool_owner_pk() {
  if [[ -n "${POOL_OWNER_PK:-}" ]]; then
    return 0
  fi
  POOL_OWNER_PK="$(dev_account_private_key_from_file "dev0" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$POOL_OWNER_PK" ]]; then
    echo "error: set POOL_OWNER_PK or add dev0 to $DEV_ACCOUNTS_FILE (required for drift phase)" >&2
    exit 1
  fi
}

# Optional: log principalAssets == stakeablePrincipalLedger + totalStaked + pendingRebalanceUnbondReserve
log_principal_assets_invariant() {
  local pa spl ts pnd sum
  pa="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "principalAssets()(uint256)")"
  spl="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  ts="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  pnd="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingRebalanceUnbondReserve()(uint256)")"
  if ! command -v python3 >/dev/null 2>&1; then
    echo "    principal_assets_invariant: skip (no python3)"
    return 0
  fi
  sum="$(python3 -c "print(int('$spl')+int('$ts')+int('$pnd'))" 2>/dev/null || echo "")"
  if [[ -n "$sum" ]] && uint256_eq "$pa" "$sum"; then
    echo "    principal_assets_invariant: ok (principalAssets == stakeable + totalStaked + pendingRebalance)"
  else
    echo "warning: principal_assets_invariant mismatch principalAssets=$pa sum_stakeable_staked_pending=$sum" >&2
  fi
}

log_contract_debug_state() {
  local label="$1"
  local max_retrieve max_validators min_stake total_units total_staked stakeable principal_assets
  local pending_rebalance pending_withdraw matured_withdraw price_per_unit
  max_retrieve="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxRetrieve()(uint32)")"
  max_validators="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxValidators()(uint32)")"
  min_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "minStakeAmount()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  stakeable="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  principal_assets="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "principalAssets()(uint256)")"
  pending_rebalance="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingRebalanceUnbondReserve()(uint256)")"
  pending_withdraw="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingWithdrawReserve()(uint256)")"
  matured_withdraw="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maturedWithdrawReserve()(uint256)")"
  price_per_unit="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pricePerUnit()(uint256)")"
  echo "  -- state[$label]"
  echo "     maxRetrieve=$max_retrieve maxValidators=$max_validators minStakeAmount=$min_stake"
  echo "     totalUnits=$total_units totalStaked=$total_staked stakeablePrincipalLedger=$stakeable"
  echo "     principalAssets=$principal_assets pricePerUnit=$price_per_unit"
  echo "     pendingRebalanceUnbondReserve=$pending_rebalance pendingWithdrawReserve=$pending_withdraw maturedWithdrawReserve=$matured_withdraw"
}

assert_liquid_reserve_invariants() {
  local label="$1"
  local liquid reward_reserve matured_withdraw stakeable reserved accounted_liquid
  liquid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "liquidBalance()(uint256)")"
  reward_reserve="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
  matured_withdraw="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maturedWithdrawReserve()(uint256)")"
  stakeable="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  reserved="$(uint256_add "$reward_reserve" "$matured_withdraw")"
  accounted_liquid="$(uint256_add "$stakeable" "$reserved")"

  if uint256_gt "$reward_reserve" "$liquid"; then
    echo "error: reward invariant failed [$label]: rewardReserve=$reward_reserve > liquidBalance=$liquid" >&2
    return 1
  fi
  if uint256_gt "$reserved" "$liquid"; then
    echo "error: liquid reserve invariant failed [$label]: rewardReserve+maturedWithdrawReserve=$reserved > liquidBalance=$liquid" >&2
    return 1
  fi
  if uint256_gt "$accounted_liquid" "$liquid"; then
    echo "error: accounted liquid invariant failed [$label]: stakeable+rewardReserve+maturedWithdrawReserve=$accounted_liquid > liquidBalance=$liquid" >&2
    return 1
  fi

  echo "  -- invariants[$label] ok: rewardReserve=$reward_reserve liquidBalance=$liquid reserved=$reserved accountedLiquid=$accounted_liquid"
}

run_phase_drift() {
  command -v python3 >/dev/null 2>&1 || {
    echo "error: drift phase requires python3 (for uint256 skew math)" >&2
    exit 1
  }
  log_flow_section "Phase drift" \
    "Owner syncTotalStaked(current + DRIFT_SKEW_WEI) simulates bookkeeping drift; reconcile should restore totalStaked to the staking bonded delegation sum." \
    "Works when bonded sum is 0: skew 0→nonzero, then expect reconcile back to 0. POOL_DEL_BECH32=$POOL_DEL_BECH32 DRIFT_SKEW_WEI=$DRIFT_SKEW_WEI DRIFT_RECOVER_MAX_WAIT_SECS=$DRIFT_RECOVER_MAX_WAIT_SECS"

  resolve_pool_owner_pk

  local expected cur wrong t0 now
  expected="$(staking_delegations_bond_total_wei "$NODE_RPC" "$POOL_DEL_BECH32")"
  if [[ ! "$expected" =~ ^[0-9]+$ ]]; then
    expected="0"
  fi

  cur="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  if [[ ! "$cur" =~ ^[0-9]+$ ]]; then
    echo "error: could not read CommunityPool totalStaked()" >&2
    exit 1
  fi

  wrong="$(python3 -c "print(int('$cur') + int('$DRIFT_SKEW_WEI'))")"
  echo "  -- staking bonded sum (bond denom) = $expected"
  echo "  -- contract totalStaked before skew = $cur"
  echo "    (this script does not deposit; totalStaked is whatever is already on-chain for this pool — e.g. leftover"
  echo "     from an earlier rebalance_scenario_runner seed / user_flow deposits / stake. It is not DRIFT_SKEW_WEI.)"
  echo "  -- drift skew: add DRIFT_SKEW_WEI=$DRIFT_SKEW_WEI to that value → syncTotalStaked($wrong)"
  if [[ "$expected" == "0" ]] && [[ "$cur" == "0" ]]; then
    echo "  -- note: bonded sum and totalStaked both 0 — skewing up from zero; reconcile should return totalStaked to 0"
  fi
  if [[ "$expected" == "0" ]] && [[ "$cur" != "0" ]]; then
    echo "warning: staking delegation sum is 0 but contract totalStaked is non-zero — possible empty delegations query," \
      "fully unbonded state not yet reconciled, or stale devnet; drift target is still staking_sum=$expected" >&2
  fi

  cast_send_expect_success "$EVM_RPC" "$POOL_OWNER_PK" "$POOL_EVM_ADDR" "syncTotalStaked(uint256)" "$wrong" || exit 1

  t0="$(date +%s)"
  while true; do
    cur="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    expected="$(staking_delegations_bond_total_wei "$NODE_RPC" "$POOL_DEL_BECH32")"
    if [[ "$cur" =~ ^[0-9]+$ ]] && [[ "$expected" =~ ^[0-9]+$ ]] && uint256_eq "$cur" "$expected"; then
      echo "  -- drift recovered: totalStaked=$cur matches staking bonded sum=$expected"
      log_principal_assets_invariant
      echo "    drift phase: ok"
      return 0
    fi
    now="$(date +%s)"
    if (( now - t0 > DRIFT_RECOVER_MAX_WAIT_SECS )); then
      echo "error: drift phase timed out after ${DRIFT_RECOVER_MAX_WAIT_SECS}s (totalStaked=$cur staking_sum=$expected)" >&2
      exit 1
    fi
    sleep 2
  done
}

# Optional: wait for module rebalance unbond reserve to show non-zero.
poll_optional_pending_rebalance_reserve() {
  local deadline now p
  [[ "${WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS:-0}" =~ ^[0-9]+$ ]] || return 0
  (( WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS == 0 )) && {
    echo "  -- pendingRebalanceUnbondReserve poll skipped (WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS=0)"
    return 0
  }
  echo "  -- polling up to ${WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS}s for pendingRebalanceUnbondReserve > 0 (optional)"
  deadline=$(( $(date +%s) + WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS ))
  while (( $(date +%s) < deadline )); do
    p="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingRebalanceUnbondReserve()(uint256)")"
    if [[ "$p" =~ ^[0-9]+$ ]] && (( p > 0 )); then
      echo "  -- observed pendingRebalanceUnbondReserve=$p > 0"
      return 0
    fi
    sleep 2
  done
  echo "warning: SKIP (optional): pendingRebalanceUnbondReserve still 0 after ${WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS}s — continuing withdraw_sizing assertions anyway" >&2
}

pool_evm_withdraw_request_amount_out() {
  local pool="$1"
  local rpc="$2"
  local rid="$3"
  local raw amount_raw amount

  [[ -z "$pool" ]] && {
    printf ''
    return 0
  }

  raw="$(cast call --rpc-url "$rpc" "$pool" \
    "withdrawRequests(uint256)(address,uint256,uint64,bool,bool)" "$rid" 2>/dev/null || true)"
  amount_raw="$(printf '%s\n' "$raw" | awk 'NF{c++} c==2 {print $1; exit}')"
  if amount="$(normalize_cast_uint256_output "$amount_raw" 2>/dev/null)"; then
    printf '%s' "$amount"
  else
    printf ''
  fi
}

maybe_auto_deposit_for_withdraw_sizing() {
  local user_units="$1"
  local total_units="$2"
  local total_staked="$3"
  local need_seed=false
  local i name pk deposited_target=false

  [[ "$WITHDRAW_SIZING_AUTO_DEPOSIT" == "1" ]] || return 0

  if [[ ! "$user_units" =~ ^[0-9]+$ ]] || (( user_units == 0 )); then
    need_seed=true
  fi
  if [[ ! "$total_units" =~ ^[0-9]+$ ]] || (( total_units == 0 )); then
    need_seed=true
  fi
  if [[ ! "$total_staked" =~ ^[0-9]+$ ]] || (( total_staked == 0 )); then
    need_seed=true
  fi
  [[ "$need_seed" == "true" ]] || return 0

  log_flow_section "withdraw_sizing auto-deposit" \
    "Detected missing preconditions (units/stake). Auto-depositing from dev accounts, similar to user_flow_multikey." \
    "AUTO_DEPOSIT_USERS=$WITHDRAW_SIZING_AUTO_DEPOSIT_USERS AMOUNT_WEI=$WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI"

  if [[ ! "$WITHDRAW_SIZING_AUTO_DEPOSIT_USERS" =~ ^[0-9]+$ ]] || (( WITHDRAW_SIZING_AUTO_DEPOSIT_USERS < 1 )); then
    echo "error: WITHDRAW_SIZING_AUTO_DEPOSIT_USERS must be a positive integer" >&2
    exit 1
  fi

  for i in $(seq 0 $((WITHDRAW_SIZING_AUTO_DEPOSIT_USERS - 1))); do
    name="dev${i}"
    pk="$(dev_account_private_key_from_file "$name" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "warning: skipping auto-deposit for missing account $name" >&2
      continue
    fi
    [[ "$name" == "$WITHDRAW_SIZING_ACCOUNT" ]] && deposited_target=true
    echo "  -- auto-deposit from $name amount=$WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI"
    approve_and_deposit "$pk" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI" "$EVM_RPC" || {
      echo "error: auto-deposit failed for $name" >&2
      exit 1
    }
    if [[ "$WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS" =~ ^[0-9]+$ ]] && (( WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS > 0 )); then
      sleep "$WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS"
    fi
  done

  # Ensure the withdraw account itself has units even when it wasn't in the first N dev accounts.
  if [[ "$deposited_target" != "true" ]]; then
    pk="$(dev_account_private_key_from_file "$WITHDRAW_SIZING_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
    if [[ -z "$pk" ]]; then
      echo "error: missing $WITHDRAW_SIZING_ACCOUNT in $DEV_ACCOUNTS_FILE for auto-deposit" >&2
      exit 1
    fi
    echo "  -- auto-deposit target account $WITHDRAW_SIZING_ACCOUNT amount=$WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI"
    approve_and_deposit "$pk" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI" "$EVM_RPC" || {
      echo "error: auto-deposit failed for target $WITHDRAW_SIZING_ACCOUNT" >&2
      exit 1
    }
  fi

  # Wait for pool automation to stake at least some principal if needed.
  if [[ "$WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS" =~ ^[0-9]+$ ]] && (( WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS > 0 )); then
    local deadline ts_now
    deadline=$(( $(date +%s) + WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS ))
    while (( $(date +%s) < deadline )); do
      ts_now="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
      if [[ "$ts_now" =~ ^[0-9]+$ ]] && (( ts_now > 0 )); then
        echo "  -- totalStaked is now $ts_now after auto-deposit"
        return 0
      fi
      sleep 2
    done
    echo "warning: totalStaked remained 0 after auto-deposit wait (${WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS}s)" >&2
  fi
}

run_phase_withdraw_sizing() {
  command -v python3 >/dev/null 2>&1 || {
    echo "error: withdraw_sizing phase requires python3" >&2
    exit 1
  }
  log_flow_section "Phase withdraw_sizing" \
    "Assert withdraw() amountOut == floor(withdrawUnits * totalStaked / totalUnits) and pendingRebalanceUnbondReserve unchanged (CommunityPool does not reduce it on user withdraw)." \
    "WITHDRAW_SIZING_ACCOUNT=$WITHDRAW_SIZING_ACCOUNT WITHDRAW_SIZING_FRACTION_BP=$WITHDRAW_SIZING_FRACTION_BP"

  local pk user_addr user_units total_units total_staked pr_before pr_after next_rid expected_out actual_out withdraw_units
  local candidate_bps bp attempt_ok=false
  pk="$(dev_account_private_key_from_file "$WITHDRAW_SIZING_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "error: missing $WITHDRAW_SIZING_ACCOUNT in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi
  user_addr="$(cast wallet address --private-key "$pk" 2>/dev/null || true)"
  if [[ -z "$user_addr" ]]; then
    echo "error: could not derive address for $WITHDRAW_SIZING_ACCOUNT" >&2
    exit 1
  fi

  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"
  maybe_auto_deposit_for_withdraw_sizing "$user_units" "$total_units" "$total_staked"

  # Re-read state after optional auto-deposit.
  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"

  if [[ ! "$total_staked" =~ ^[0-9]+$ ]] || [[ ! "$total_units" =~ ^[0-9]+$ ]] || (( total_staked == 0 )) || (( total_units == 0 )); then
    echo "warning: skipping withdraw_sizing — need totalStaked>0 and totalUnits>0 (set WITHDRAW_SIZING_AUTO_DEPOSIT=1 or run user_flow first)" >&2
    return 0
  fi
  if [[ ! "$user_units" =~ ^[0-9]+$ ]] || (( user_units == 0 )); then
    echo "warning: skipping withdraw_sizing — $WITHDRAW_SIZING_ACCOUNT still has no pool units (auto-deposit disabled/failed; run user_flow or set WITHDRAW_SIZING_ACCOUNT)" >&2
    return 0
  fi

  poll_optional_pending_rebalance_reserve

  pr_before="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingRebalanceUnbondReserve()(uint256)")"
  [[ "$pr_before" =~ ^[0-9]+$ ]] || pr_before="0"

  candidate_bps="$WITHDRAW_SIZING_CANDIDATE_BP_LIST"
  # Ensure the requested fraction is attempted first when it's not already first in the list.
  if [[ ",$candidate_bps," != *",$WITHDRAW_SIZING_FRACTION_BP,"* ]]; then
    candidate_bps="${WITHDRAW_SIZING_FRACTION_BP},${candidate_bps}"
  fi

  for bp in $(printf '%s' "$candidate_bps" | tr ',' ' '); do
    [[ "$bp" =~ ^[0-9]+$ ]] || continue
    (( bp < 1 || bp > 10000 )) && continue

    # Re-read changing pool values for each attempt.
    total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
    user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"
    if [[ ! "$total_staked" =~ ^[0-9]+$ ]] || [[ ! "$total_units" =~ ^[0-9]+$ ]] || [[ ! "$user_units" =~ ^[0-9]+$ ]] || (( total_staked == 0 )) || (( total_units == 0 )) || (( user_units == 0 )); then
      continue
    fi

    local _py
    _py="$(python3 -c "
import sys
uu, tu, ts, bp = map(int, sys.argv[1:5])
if tu == 0 or ts == 0:
    sys.exit(2)
wu = uu * bp // 10000
if wu == 0:
    wu = uu
exp = (wu * ts) // tu
if exp == 0:
    wu = uu
    exp = (wu * ts) // tu
if exp == 0:
    sys.exit(3)
print(wu)
print(exp)
" "$user_units" "$total_units" "$total_staked" "$bp")" || continue
    withdraw_units="$(printf '%s\n' "$_py" | sed -n '1p')"
    expected_out="$(printf '%s\n' "$_py" | sed -n '2p')"
    [[ -n "$withdraw_units" && -n "$expected_out" ]] || continue

    next_rid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "nextWithdrawRequestId()(uint256)")"
    if [[ ! "$next_rid" =~ ^[0-9]+$ ]]; then
      echo "error: could not read nextWithdrawRequestId" >&2
      exit 1
    fi

    echo "  -- attempt bp=$bp user=$WITHDRAW_SIZING_ACCOUNT unitsOf=$user_units totalUnits=$total_units totalStaked=$total_staked"
    echo "     withdrawUnits=$withdraw_units expectedAmountOut=$expected_out pendingRebalanceUnbondReserve(before)=$pr_before"
    if (
      export CAST_SEND_GAS_LIMIT="$WITHDRAW_SIZING_GAS_LIMIT"
      cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "withdraw(uint256)" "$withdraw_units"
    ); then
      attempt_ok=true
      echo "  -- withdraw succeeded with bp=$bp"
      break
    fi
    echo "  -- withdraw reverted at bp=$bp; trying smaller candidate (if any)"
    sleep 1
  done

  if [[ "$attempt_ok" != "true" ]]; then
    echo "error: withdraw_sizing could not find a successful withdraw amount; candidates=$candidate_bps" >&2
    echo "hint: try lower WITHDRAW_SIZING_CANDIDATE_BP_LIST or inspect precompile undelegate constraints" >&2
    exit 1
  fi

  actual_out="$(pool_evm_withdraw_request_amount_out "$POOL_EVM_ADDR" "$EVM_RPC" "$next_rid")"
  if [[ -z "$actual_out" ]]; then
    echo "error: could not read withdrawRequests($next_rid).amountOut" >&2
    exit 1
  fi

  if ! uint256_eq "$expected_out" "$actual_out"; then
    echo "error: amountOut mismatch: expected $expected_out (floor(withdrawUnits*totalStaked/totalUnits)) got $actual_out" >&2
    exit 1
  fi
  echo "  -- withdrawRequests($next_rid).amountOut=$actual_out (matches formula)"

  pr_after="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "pendingRebalanceUnbondReserve()(uint256)")"
  [[ "$pr_after" =~ ^[0-9]+$ ]] || pr_after="0"
  if ! uint256_eq "$pr_before" "$pr_after"; then
    echo "error: pendingRebalanceUnbondReserve changed after user withdraw (before=$pr_before after=$pr_after); expected unchanged" >&2
    exit 1
  fi
  echo "  -- pendingRebalanceUnbondReserve unchanged: $pr_after"

  echo "    withdraw_sizing phase: ok"
}

block_timestamp_unix() {
  cast block latest --rpc-url "$EVM_RPC" --json 2>/dev/null | jq -r '.timestamp // empty' | python3 -c "
import sys
s = sys.stdin.read().strip()
if not s:
    print(0)
elif s.startswith('0x') or s.startswith('0X'):
    print(int(s, 16))
else:
    print(int(s))
"
}

withdraw_request_maturity_unix() {
  local rid="$1"
  cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" \
    "withdrawRequests(uint256)(address,uint256,uint64,bool,bool)" "$rid" 2>/dev/null \
    | python3 -c "
import sys
parts = sys.stdin.read().split()
if len(parts) < 3:
    print(0)
    sys.exit(0)
m = parts[2]
if m.startswith('0x') or m.startswith('0X'):
    print(int(m, 16))
else:
    print(int(m.split('[')[0].strip()))
"
}

wait_until_withdraw_request_mature_or_timeout() {
  local rid="$1"
  local max_sec="${2:-300}"
  local start mt bt
  start="$(date +%s)"
  mt="$(withdraw_request_maturity_unix "$rid")"
  if [[ "$mt" == "0" ]]; then
    sleep 3
    mt="$(withdraw_request_maturity_unix "$rid")"
  fi
  if [[ "$mt" == "0" ]]; then
    echo "warning: requestId=$rid has maturity 0; skipping optional liquidity stress" >&2
    return 1
  fi
  echo "  -- requestId=$rid maturityUnix=$mt; waiting for latest block time to reach maturity (max ${max_sec}s)"
  while true; do
    bt="$(block_timestamp_unix)"
    if [[ "$bt" =~ ^[0-9]+$ ]] && (( bt >= mt )); then
      echo "  -- maturity reached: latest blockTime=$bt >= maturityTime=$mt"
      return 0
    fi
    if (( $(date +%s) - start > max_sec )); then
      echo "warning: timed out waiting for requestId=$rid maturity (blockTime=$bt maturity=$mt)" >&2
      return 1
    fi
    sleep 2
  done
}

submit_withdraw_request_for_account() {
  local account_name="$1"
  local gas_limit="$2"
  local fraction_bp="$3"
  local candidate_bps="$4"
  local pk user_addr user_units total_units total_staked bp next_rid withdraw_units expected_out
  local attempt_ok=false

  pk="$(dev_account_private_key_from_file "$account_name" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$pk" ]]; then
    echo "error: missing $account_name in $DEV_ACCOUNTS_FILE" >&2
    return 1
  fi
  user_addr="$(cast wallet address --private-key "$pk" 2>/dev/null || true)"
  if [[ -z "$user_addr" ]]; then
    echo "error: could not derive address for $account_name" >&2
    return 1
  fi

  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"
  maybe_auto_deposit_for_withdraw_sizing "$user_units" "$total_units" "$total_staked"

  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"

  if [[ ! "$total_staked" =~ ^[0-9]+$ ]] || [[ ! "$total_units" =~ ^[0-9]+$ ]] || (( total_staked == 0 )) || (( total_units == 0 )); then
    echo "warning: skipping request setup for $account_name — need totalStaked>0 and totalUnits>0" >&2
    return 2
  fi
  if [[ ! "$user_units" =~ ^[0-9]+$ ]] || (( user_units == 0 )); then
    echo "warning: skipping request setup for $account_name — account has no pool units" >&2
    return 2
  fi

  if [[ ",$candidate_bps," != *",$fraction_bp,"* ]]; then
    candidate_bps="${fraction_bp},${candidate_bps}"
  fi

  for bp in $(printf '%s' "$candidate_bps" | tr ',' ' '); do
    [[ "$bp" =~ ^[0-9]+$ ]] || continue
    (( bp < 1 || bp > 10000 )) && continue

    total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
    user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$user_addr")"
    if [[ ! "$total_staked" =~ ^[0-9]+$ ]] || [[ ! "$total_units" =~ ^[0-9]+$ ]] || [[ ! "$user_units" =~ ^[0-9]+$ ]] || (( total_staked == 0 )) || (( total_units == 0 )) || (( user_units == 0 )); then
      continue
    fi

    local _py
    _py="$(python3 -c "
import sys
uu, tu, ts, bp = map(int, sys.argv[1:5])
if tu == 0 or ts == 0:
    sys.exit(2)
wu = uu * bp // 10000
if wu == 0:
    wu = uu
exp = (wu * ts) // tu
if exp == 0:
    wu = uu
    exp = (wu * ts) // tu
if exp == 0:
    sys.exit(3)
print(wu)
print(exp)
" "$user_units" "$total_units" "$total_staked" "$bp")" || continue
    withdraw_units="$(printf '%s\n' "$_py" | sed -n '1p')"
    expected_out="$(printf '%s\n' "$_py" | sed -n '2p')"
    [[ -n "$withdraw_units" && -n "$expected_out" ]] || continue

    next_rid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "nextWithdrawRequestId()(uint256)")"
    if [[ ! "$next_rid" =~ ^[0-9]+$ ]]; then
      echo "error: could not read nextWithdrawRequestId" >&2
      return 1
    fi

    echo "  -- attempt bp=$bp user=$account_name unitsOf=$user_units totalUnits=$total_units totalStaked=$total_staked"
    echo "     withdrawUnits=$withdraw_units expectedAmountOut=$expected_out nextWithdrawRequestId=$next_rid"
    if (
      export CAST_SEND_GAS_LIMIT="$gas_limit"
      cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "withdraw(uint256)" "$withdraw_units"
    ); then
      attempt_ok=true
      REQUEST_SETUP_PK="$pk"
      REQUEST_SETUP_USER_ADDR="$user_addr"
      REQUEST_SETUP_REQUEST_ID="$next_rid"
      REQUEST_SETUP_WITHDRAW_UNITS="$withdraw_units"
      REQUEST_SETUP_EXPECTED_OUT="$expected_out"
      break
    fi
    echo "  -- withdraw reverted at bp=$bp; trying smaller candidate (if any)"
    sleep 1
  done

  if [[ "$attempt_ok" != "true" ]]; then
    echo "error: could not find a successful withdraw amount for $account_name; candidates=$candidate_bps" >&2
    return 1
  fi
}

run_phase_liquidity() {
  command -v python3 >/dev/null 2>&1 || {
    echo "error: liquidity phase requires python3" >&2
    exit 1
  }
  log_flow_section "Phase liquidity" \
    "Create one withdraw request, then assert claimWithdraw(requestId) reverts before maturity." \
    "Optional CLAIM_STRESS_INSUFFICIENT_LIQUID=1 waits for maturity and retries claimWithdraw best-effort to probe liquidity settling." \
    "LIQUIDITY_ACCOUNT=$LIQUIDITY_ACCOUNT LIQUIDITY_FRACTION_BP=$LIQUIDITY_FRACTION_BP"

  REQUEST_SETUP_PK=""
  REQUEST_SETUP_USER_ADDR=""
  REQUEST_SETUP_REQUEST_ID=""
  REQUEST_SETUP_WITHDRAW_UNITS=""
  REQUEST_SETUP_EXPECTED_OUT=""

  local pk user_addr rid withdraw_units expected_out
  submit_withdraw_request_for_account "$LIQUIDITY_ACCOUNT" "$LIQUIDITY_GAS_LIMIT" "$LIQUIDITY_FRACTION_BP" "$LIQUIDITY_CANDIDATE_BP_LIST" || {
    case "$?" in
      2) return 0 ;;
      *) exit 1 ;;
    esac
  }

  pk="$REQUEST_SETUP_PK"
  user_addr="$REQUEST_SETUP_USER_ADDR"
  rid="$REQUEST_SETUP_REQUEST_ID"
  withdraw_units="$REQUEST_SETUP_WITHDRAW_UNITS"
  expected_out="$REQUEST_SETUP_EXPECTED_OUT"

  echo "  -- submitted withdraw requestId=$rid withdrawUnits=$withdraw_units expectedAmountOut=$expected_out"
  echo "  -- claimWithdraw($rid) before maturity should revert"
  (
    export CAST_SEND_GAS_LIMIT="$LIQUIDITY_GAS_LIMIT"
    cast_send_expect_revert "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimWithdraw(uint256)" "$rid"
  ) || {
    echo "error: expected pre-maturity claimWithdraw($rid) to revert" >&2
    exit 1
  }
  echo "  -- pre-maturity claimWithdraw($rid) reverted as expected"

  if [[ "$CLAIM_STRESS_INSUFFICIENT_LIQUID" == "1" ]]; then
    local attempt=0
    log_flow_section "Optional liquidity stress" \
      "Best-effort only: wait for request maturity, then retry claimWithdraw to observe whether liquid principal is available by then." \
      "This does not fail the deterministic phase if liquidity remains insufficient or the pool is still settling."
    if wait_until_withdraw_request_mature_or_timeout "$rid" "$LIQUIDITY_MATURITY_MAX_WAIT_SECS"; then
      while (( attempt < CLAIM_STRESS_MAX_ATTEMPTS )); do
        if (
          export CAST_SEND_GAS_LIMIT="$LIQUIDITY_GAS_LIMIT"
          cast_send_expect_success "$EVM_RPC" "$pk" "$POOL_EVM_ADDR" "claimWithdraw(uint256)" "$rid"
        ); then
          echo "  -- optional stress claim succeeded for requestId=$rid owner=$LIQUIDITY_ACCOUNT addr=$user_addr"
          break
        fi
        attempt=$((attempt + 1))
        echo "  -- optional stress retry $attempt/$CLAIM_STRESS_MAX_ATTEMPTS (likely insufficient liquid or pool still settling)"
        sleep "$CLAIM_STRESS_POLL_INTERVAL_SECS"
      done
      if (( attempt >= CLAIM_STRESS_MAX_ATTEMPTS )); then
        echo "warning: optional liquidity stress did not claim requestId=$rid after $CLAIM_STRESS_MAX_ATTEMPTS attempts" >&2
      fi
    fi
  fi

  echo "    liquidity phase: ok"
}

run_phase_dust() {
  command -v python3 >/dev/null 2>&1 || {
    echo "error: dust phase requires python3" >&2
    exit 1
  }

  log_flow_section "Phase dust" \
    "Assert deposit(1) reverts when price/unit rounds minted units to 0; assert withdraw(1) reverts when amountOut rounds to 0." \
    "Then set owner config boundaries: maxValidators=0 must revert; maxValidators=$DUST_BOUNDARY_MAX_VALIDATORS with minStake=$DUST_HIGH_MIN_STAKE_AMOUNT_WEI must make stake() a no-op; restore config at the end." \
    "DUST_ACCOUNT=$DUST_ACCOUNT DUST_SECONDARY_ACCOUNT=$DUST_SECONDARY_ACCOUNT DUST_SEED_DEPOSIT_AMOUNT_WEI=$DUST_SEED_DEPOSIT_AMOUNT_WEI"

  resolve_pool_owner_pk

  local status=0
  local owner_pk primary_pk secondary_pk primary_addr secondary_addr
  local old_max_retrieve old_max_validators old_min_stake old_total_staked
  local read_max_retrieve read_max_validators read_min_stake restored_total_staked
  local stakeable_before_stake total_staked_before_stake stakeable_after_stake total_staked_after_stake
  local total_units_for_dust target_total_staked_for_zero_mint
  local stakeable_before_restore final_stakeable expected_post_restore_total_staked

  owner_pk="$POOL_OWNER_PK"
  primary_pk="$(dev_account_private_key_from_file "$DUST_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
  secondary_pk="$(dev_account_private_key_from_file "$DUST_SECONDARY_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$primary_pk" ]]; then
    echo "error: missing $DUST_ACCOUNT in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi
  if [[ -z "$secondary_pk" ]]; then
    echo "error: missing $DUST_SECONDARY_ACCOUNT in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi
  primary_addr="$(cast wallet address --private-key "$primary_pk" 2>/dev/null || true)"
  secondary_addr="$(cast wallet address --private-key "$secondary_pk" 2>/dev/null || true)"
  if [[ -z "$primary_addr" || -z "$secondary_addr" ]]; then
    echo "error: could not derive dust account addresses" >&2
    exit 1
  fi

  old_max_retrieve="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxRetrieve()(uint32)")"
  old_max_validators="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxValidators()(uint32)")"
  old_min_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "minStakeAmount()(uint256)")"
  old_total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  if [[ ! "$old_max_retrieve" =~ ^[0-9]+$ ]] || [[ ! "$old_max_validators" =~ ^[0-9]+$ ]] || [[ ! "$old_min_stake" =~ ^[0-9]+$ ]] || [[ ! "$old_total_staked" =~ ^[0-9]+$ ]]; then
    echo "error: could not read original CommunityPool config/state before dust phase" >&2
    exit 1
  fi
  log_contract_debug_state "dust:start"

  echo "  -- setConfig(maxValidators=0) should revert"
  if ! cast_send_expect_revert "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" \
    "setConfig(uint32,uint32,uint256)" "$old_max_retrieve" 0 "$old_min_stake"; then
    echo "error: expected setConfig(..., maxValidators=0, ...) to revert" >&2
    status=1
  fi

  if (( status == 0 )); then
    echo "  -- setConfig boundary: maxValidators=$DUST_BOUNDARY_MAX_VALIDATORS minStakeAmount=$DUST_HIGH_MIN_STAKE_AMOUNT_WEI"
    if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" \
      "setConfig(uint32,uint32,uint256)" "$old_max_retrieve" "$DUST_BOUNDARY_MAX_VALIDATORS" "$DUST_HIGH_MIN_STAKE_AMOUNT_WEI"; then
      echo "error: setConfig boundary update failed" >&2
      status=1
    fi
  fi

  if (( status == 0 )); then
    read_max_retrieve="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxRetrieve()(uint32)")"
    read_max_validators="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxValidators()(uint32)")"
    read_min_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "minStakeAmount()(uint256)")"
    if ! uint256_eq "$read_max_retrieve" "$old_max_retrieve" || ! uint256_eq "$read_max_validators" "$DUST_BOUNDARY_MAX_VALIDATORS" || ! uint256_eq "$read_min_stake" "$DUST_HIGH_MIN_STAKE_AMOUNT_WEI"; then
      echo "error: boundary config readback mismatch maxRetrieve=$read_max_retrieve maxValidators=$read_max_validators minStakeAmount=$read_min_stake" >&2
      status=1
    else
      echo "  -- boundary config applied: maxRetrieve=$read_max_retrieve maxValidators=$read_max_validators minStakeAmount=$read_min_stake"
    fi
    log_contract_debug_state "dust:after-boundary-config"
  fi

  if (( status == 0 )); then
    echo "  -- seed deposit from $DUST_ACCOUNT amount=$DUST_SEED_DEPOSIT_AMOUNT_WEI under high minStake"
    if ! approve_and_deposit "$primary_pk" "$POOL_EVM_ADDR" "$BOND_PRECOMPILE" "$DUST_SEED_DEPOSIT_AMOUNT_WEI" "$EVM_RPC"; then
      echo "error: dust seed approve+deposit failed for $DUST_ACCOUNT" >&2
      status=1
    fi
    log_contract_debug_state "dust:after-seed-deposit"
  fi

  if (( status == 0 )); then
    stakeable_before_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
    total_staked_before_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    echo "  -- stake() under elevated minStake should no-op"
    if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" "stake()"; then
      echo "error: stake() tx failed under elevated minStake" >&2
      status=1
    fi
  fi

  if (( status == 0 )); then
    stakeable_after_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
    total_staked_after_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    if ! uint256_eq "$stakeable_before_stake" "$stakeable_after_stake" || ! uint256_eq "$total_staked_before_stake" "$total_staked_after_stake"; then
      echo "error: expected stake() no-op under elevated minStake; before stakeable=$stakeable_before_stake totalStaked=$total_staked_before_stake after stakeable=$stakeable_after_stake totalStaked=$total_staked_after_stake" >&2
      status=1
    else
      echo "  -- stake() no-op confirmed: stakeablePrincipalLedger=$stakeable_after_stake totalStaked=$total_staked_after_stake"
    fi
  fi

  if (( status == 0 )); then
    total_units_for_dust="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
    if [[ ! "$total_units_for_dust" =~ ^[0-9]+$ ]] || ! uint256_gt "$total_units_for_dust" 1; then
      echo "error: dust phase needs totalUnits > 1 after seed deposit (got $total_units_for_dust)" >&2
      log_contract_debug_state "dust:bad-total-units"
      status=1
    fi
  fi

  if (( status == 0 )); then
    target_total_staked_for_zero_mint="$(python3 -c "print(int('$total_units_for_dust') + 1)")"
    echo "  -- syncTotalStaked($target_total_staked_for_zero_mint) so deposit(1) rounds minted units to 0"
    if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" "syncTotalStaked(uint256)" "$target_total_staked_for_zero_mint"; then
      echo "error: failed to inflate totalStaked for dust deposit test" >&2
      status=1
    fi
  fi

  if (( status == 0 )); then
    echo "  -- approve + deposit(1) from $DUST_SECONDARY_ACCOUNT should revert with zero minted units"
    if ! cast_send_expect_success "$EVM_RPC" "$secondary_pk" "$BOND_PRECOMPILE" \
      "approve(address,uint256)" "$POOL_EVM_ADDR" 1; then
      echo "error: approve(1) failed for $DUST_SECONDARY_ACCOUNT" >&2
      status=1
    elif ! cast_send_expect_revert "$EVM_RPC" "$secondary_pk" "$POOL_EVM_ADDR" "deposit(uint256)" 1; then
      echo "error: expected deposit(1) dust path to revert" >&2
      log_contract_debug_state "dust:deposit1-unexpected-success"
      status=1
    else
      echo "  -- deposit(1) reverted as expected under zero-minted-units conditions"
    fi
  fi

  if (( status == 0 )); then
    echo "  -- syncTotalStaked(1) so withdraw(1) rounds amountOut to 0"
    if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" "syncTotalStaked(uint256)" 1; then
      echo "error: failed to set totalStaked=1 for dust withdraw test" >&2
      status=1
    elif ! cast_send_expect_revert "$EVM_RPC" "$primary_pk" "$POOL_EVM_ADDR" "withdraw(uint256)" 1; then
      echo "error: expected withdraw(1) dust path to revert" >&2
      log_contract_debug_state "dust:withdraw1-unexpected-success"
      status=1
    else
      echo "  -- withdraw(1) reverted as expected when amountOut rounds to 0"
    fi
  fi

  log_contract_debug_state "dust:before-restore"
  stakeable_before_restore="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  [[ "$stakeable_before_restore" =~ ^[0-9]+$ ]] || stakeable_before_restore="0"

  echo "  -- restoring original totalStaked while elevated minStake still prevents auto-stake"
  if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" "syncTotalStaked(uint256)" "$old_total_staked"; then
    echo "error: failed to restore original totalStaked after dust phase" >&2
    status=1
  fi
  restored_total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  if ! uint256_eq "$restored_total_staked" "$old_total_staked"; then
    echo "error: totalStaked restore failed before config restore got $restored_total_staked expected $old_total_staked" >&2
    log_contract_debug_state "dust:restore-totalstaked-mismatch"
    status=1
  else
    echo "  -- original totalStaked restored under elevated minStake: $restored_total_staked"
  fi

  echo "  -- restoring original config"
  if ! cast_send_expect_success "$EVM_RPC" "$owner_pk" "$POOL_EVM_ADDR" \
    "setConfig(uint32,uint32,uint256)" "$old_max_retrieve" "$old_max_validators" "$old_min_stake"; then
    echo "error: failed to restore original config after dust phase" >&2
    status=1
  fi

  read_max_retrieve="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxRetrieve()(uint32)")"
  read_max_validators="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "maxValidators()(uint32)")"
  read_min_stake="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "minStakeAmount()(uint256)")"
  restored_total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  final_stakeable="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "stakeablePrincipalLedger()(uint256)")"
  if ! uint256_eq "$read_max_retrieve" "$old_max_retrieve" || ! uint256_eq "$read_max_validators" "$old_max_validators" || ! uint256_eq "$read_min_stake" "$old_min_stake"; then
    echo "error: restored config mismatch maxRetrieve=$read_max_retrieve expected=$old_max_retrieve maxValidators=$read_max_validators expected=$old_max_validators minStakeAmount=$read_min_stake expected=$old_min_stake" >&2
    log_contract_debug_state "dust:restore-config-mismatch"
    status=1
  else
    echo "  -- original config restored"
  fi

  expected_post_restore_total_staked="$(uint256_add "$old_total_staked" "$stakeable_before_restore")"
  if uint256_eq "$restored_total_staked" "$old_total_staked"; then
    echo "  -- post-restore totalStaked unchanged at original value: $restored_total_staked"
  elif uint256_eq "$restored_total_staked" "$expected_post_restore_total_staked" && uint256_eq "$final_stakeable" 0; then
    echo "  -- post-restore totalStaked advanced by prior liquid stakeable=$stakeable_before_restore after minStake restoration"
    echo "     this is expected live-chain automation, not a contract/module bug"
  else
    echo "error: unexpected post-restore state totalStaked=$restored_total_staked old_total_staked=$old_total_staked stakeable_before_restore=$stakeable_before_restore final_stakeable=$final_stakeable" >&2
    log_contract_debug_state "dust:unexpected-post-restore"
    status=1
  fi
  log_contract_debug_state "dust:after-restore"

  if (( status != 0 )); then
    exit 1
  fi

  echo "    dust phase: ok"
}

run_phase_rewards() {
  command -v python3 >/dev/null 2>&1 || {
    echo "error: rewards phase requires python3" >&2
    exit 1
  }

  log_flow_section "Phase rewards" \
    "Run owner harvest() ${REWARDS_HARVEST_COUNT}x, then claimRewards() for one dev account and assert sane deltas plus liquid reserve invariants." \
    "REWARDS_ACCOUNT=$REWARDS_ACCOUNT SKIP_EMPTY_POOL_HARVEST=$SKIP_EMPTY_POOL_HARVEST REWARDS_HARVEST_INTERVAL_SECS=$REWARDS_HARVEST_INTERVAL_SECS"

  resolve_pool_owner_pk

  local rewards_pk rewards_addr user_units total_units total_staked
  local before_staked before_liquid before_reward before_acc after_staked after_liquid after_reward after_acc
  local reward_user_balance_before reward_user_balance_after reserve_before_claim reserve_after_claim
  local units_before_claim units_after_claim acc_before_claim acc_after_claim debt_before_claim debt_after_claim
  local liquid_before_claim liquid_after_claim pending_before_claim pending_after_claim expected_debt_after_claim
  local simulated_claim_before_tx raw_simulated_claim_before_tx expected_reserve_after_no_concurrency
  local reward_reserve_credit_during_claim_block claim_receipt_json claim_tx_fee_wei claim_balance_delta
  local claim_gross_from_wallet_delta errf raw i

  rewards_pk="$(dev_account_private_key_from_file "$REWARDS_ACCOUNT" "$DEV_ACCOUNTS_FILE" || true)"
  if [[ -z "$rewards_pk" ]]; then
    echo "error: missing $REWARDS_ACCOUNT in $DEV_ACCOUNTS_FILE" >&2
    exit 1
  fi
  rewards_addr="$(cast wallet address --private-key "$rewards_pk" 2>/dev/null || true)"
  if [[ -z "$rewards_addr" ]]; then
    echo "error: could not derive address for $REWARDS_ACCOUNT" >&2
    exit 1
  fi

  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$rewards_addr")"

  if [[ "${WITHDRAW_SIZING_AUTO_DEPOSIT:-1}" == "1" ]]; then
    local saved_withdraw_account="$WITHDRAW_SIZING_ACCOUNT"
    WITHDRAW_SIZING_ACCOUNT="$REWARDS_ACCOUNT"
    maybe_auto_deposit_for_withdraw_sizing "$user_units" "$total_units" "$total_staked"
    WITHDRAW_SIZING_ACCOUNT="$saved_withdraw_account"
  fi

  total_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
  total_units="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalUnits()(uint256)")"
  user_units="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$rewards_addr")"
  log_contract_debug_state "rewards:start"

  if [[ ! "$total_units" =~ ^[0-9]+$ ]] || [[ "$total_units" == "0" ]]; then
    if [[ "$SKIP_EMPTY_POOL_HARVEST" == "1" ]]; then
      echo "warning: SKIP rewards phase: totalUnits is 0 (empty pool). Deep empty-pool harvest cases belong in Forge; see contracts/solidity/pool/README.md." >&2
      assert_liquid_reserve_invariants "rewards:empty-skip" || exit 1
      return 0
    fi
    echo "error: rewards phase requires totalUnits>0 unless SKIP_EMPTY_POOL_HARVEST=1" >&2
    exit 1
  fi
  if [[ ! "$user_units" =~ ^[0-9]+$ ]] || [[ "$user_units" == "0" ]]; then
    echo "warning: SKIP rewards phase: $REWARDS_ACCOUNT has no pool units even after optional auto-deposit" >&2
    assert_liquid_reserve_invariants "rewards:no-user-units" || exit 1
    return 0
  fi

  for i in $(seq 1 "$REWARDS_HARVEST_COUNT"); do
    before_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    before_liquid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "liquidBalance()(uint256)")"
    before_reward="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
    before_acc="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "accRewardPerUnit()(uint256)")"
    echo "  -- harvest attempt $i/$REWARDS_HARVEST_COUNT"
    if ! cast_send_expect_success "$EVM_RPC" "$POOL_OWNER_PK" "$POOL_EVM_ADDR" "harvest()"; then
      echo "warning: harvest failed at attempt $i — likely no validator rewards available yet" >&2
      echo "note: this only means no fresh rewards were claimable from the distribution precompile at this instant" >&2
      echo "note: existing rewardReserve / user pending rewards may still be non-zero from earlier harvests or EndBlock automation" >&2
      if [[ "${REWARDS_REQUIRE_HARVEST_SUCCESS:-0}" == "1" ]]; then
        echo "error: REWARDS_REQUIRE_HARVEST_SUCCESS=1 but harvest failed" >&2
        exit 1
      else
        echo "  -- skipping remaining harvest attempts and proceeding with claimRewards test" >&2
        break
      fi
    fi
    after_staked="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "totalStaked()(uint256)")"
    after_liquid="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "liquidBalance()(uint256)")"
    after_reward="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
    after_acc="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "accRewardPerUnit()(uint256)")"
    if ! uint256_eq "$before_staked" "$after_staked"; then
      echo "error: harvest changed totalStaked on attempt $i (before=$before_staked after=$after_staked)" >&2
      exit 1
    fi
    if uint256_gt "$before_liquid" "$after_liquid"; then
      echo "error: harvest decreased liquidBalance on attempt $i (before=$before_liquid after=$after_liquid)" >&2
      exit 1
    fi
    if uint256_gt "$before_reward" "$after_reward"; then
      echo "error: harvest decreased rewardReserve on attempt $i (before=$before_reward after=$after_reward)" >&2
      exit 1
    fi
    if uint256_gt "$before_acc" "$after_acc"; then
      echo "error: harvest decreased accRewardPerUnit on attempt $i (before=$before_acc after=$after_acc)" >&2
      exit 1
    fi
    echo "     liquid delta=$(uint256_sub_nonnegative "$after_liquid" "$before_liquid") rewardReserve delta=$(uint256_sub_nonnegative "$after_reward" "$before_reward") accRewardPerUnit delta=$(uint256_sub_nonnegative "$after_acc" "$before_acc")"
    assert_liquid_reserve_invariants "rewards:post-harvest-$i" || exit 1
    if [[ "$REWARDS_HARVEST_INTERVAL_SECS" =~ ^[0-9]+$ ]] && (( REWARDS_HARVEST_INTERVAL_SECS > 0 )) && (( i < REWARDS_HARVEST_COUNT )); then
      sleep "$REWARDS_HARVEST_INTERVAL_SECS"
    fi
  done

  reserve_before_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
  units_before_claim="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$rewards_addr")"
  acc_before_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "accRewardPerUnit()(uint256)")"
  debt_before_claim="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "rewardDebt(address)(uint256)" "$rewards_addr")"
  liquid_before_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "liquidBalance()(uint256)")"
  reward_user_balance_before="$(pool_evm_call_uint256_args "$BOND_PRECOMPILE" "$EVM_RPC" "balanceOf(address)(uint256)" "$rewards_addr")"
  pending_before_claim="$(uint256_pending_rewards_from_index "$units_before_claim" "$acc_before_claim" "$debt_before_claim")"
  expected_debt_after_claim="$(python3 -c "print((int('$units_before_claim') * int('$acc_before_claim')) // 10**18)" 2>/dev/null || echo "")"
  raw_simulated_claim_before_tx="$(cast call --rpc-url "$EVM_RPC" --from "$rewards_addr" "$POOL_EVM_ADDR" "claimRewards()(uint256)" 2>/dev/null || true)"
  if simulated_claim_before_tx="$(normalize_cast_uint256_output "$raw_simulated_claim_before_tx")"; then
    :
  else
    echo "error: could not simulate claimRewards() with eth_call before sending tx" >&2
    exit 1
  fi
  expected_reserve_after_no_concurrency="$(uint256_sub_strict "$reserve_before_claim" "$simulated_claim_before_tx" || true)"
  if [[ -z "$expected_reserve_after_no_concurrency" ]]; then
    echo "error: simulated claimRewards() amount $simulated_claim_before_tx exceeds rewardReserve before claim $reserve_before_claim" >&2
    exit 1
  fi

  echo "  -- pre-claim reward accounting snapshot"
  echo "     units=$units_before_claim accRewardPerUnit=$acc_before_claim rewardDebt=$debt_before_claim pendingFromIndex=$pending_before_claim"
  echo "     rewardReserve=$reserve_before_claim liquidBalance=$liquid_before_claim user bond balance=$reward_user_balance_before"
  echo "     claimRewards() eth_call simulation=$simulated_claim_before_tx"
  echo "     expected rewardReserve after claim if no later credit lands in the same block=$expected_reserve_after_no_concurrency"
  echo "     interpretation: eth_call is the authoritative pre-tx expectation; pendingFromIndex is a readable cross-check from reward accounting state"
  echo "  -- claimRewards() for $REWARDS_ACCOUNT"
  wait_evm_nonce_settled_for_pk "$rewards_pk" "$EVM_RPC" 45
  errf="$(mktemp -t cast_claim_rewards.XXXXXX)"
  raw="$(cast send --json --rpc-url "$EVM_RPC" --private-key "$rewards_pk" "$POOL_EVM_ADDR" "claimRewards()" 2>"$errf")" || {
    cat "$errf" >&2
    rm -f "$errf"
    echo "error: claimRewards failed for $REWARDS_ACCOUNT" >&2
    exit 1
  }
  rm -f "$errf"
  if ! claim_receipt_json="$(cast_stdout_to_receipt_json "$raw")"; then
    echo "error: claimRewards did not return parseable JSON receipt for $REWARDS_ACCOUNT" >&2
    echo "$raw" >&2
    exit 1
  fi
  if ! echo "$claim_receipt_json" | jq -e '.status' >/dev/null 2>&1; then
    echo "error: claimRewards receipt JSON missing .status for $REWARDS_ACCOUNT" >&2
    echo "$claim_receipt_json" >&2
    exit 1
  fi
  if ! cast_receipt_success "$claim_receipt_json"; then
    echo "error: claimRewards reverted for $REWARDS_ACCOUNT (status=$(echo "$claim_receipt_json" | jq -r '.status'))" >&2
    exit 1
  fi
  claim_tx_fee_wei="$(receipt_json_effective_fee_wei "$claim_receipt_json" || true)"
  if [[ -z "$claim_tx_fee_wei" ]]; then
    echo "error: could not derive claimRewards tx fee from receipt JSON" >&2
    echo "$claim_receipt_json" >&2
    exit 1
  fi
  reserve_after_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "rewardReserve()(uint256)")"
  units_after_claim="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "unitsOf(address)(uint256)" "$rewards_addr")"
  acc_after_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "accRewardPerUnit()(uint256)")"
  debt_after_claim="$(pool_evm_call_uint256_args "$POOL_EVM_ADDR" "$EVM_RPC" "rewardDebt(address)(uint256)" "$rewards_addr")"
  liquid_after_claim="$(pool_evm_call_uint256 "$POOL_EVM_ADDR" "$EVM_RPC" "liquidBalance()(uint256)")"
  reward_user_balance_after="$(pool_evm_call_uint256_args "$BOND_PRECOMPILE" "$EVM_RPC" "balanceOf(address)(uint256)" "$rewards_addr")"
  pending_after_claim="$(uint256_pending_rewards_from_index "$units_after_claim" "$acc_after_claim" "$debt_after_claim")"
  reward_reserve_credit_during_claim_block="$(python3 -c "print(int('$reserve_after_claim') - int('$expected_reserve_after_no_concurrency'))" 2>/dev/null || echo "")"
  claim_balance_delta="$(uint256_sub_nonnegative "$reward_user_balance_after" "$reward_user_balance_before")"
  claim_gross_from_wallet_delta="$(uint256_add "$claim_balance_delta" "$claim_tx_fee_wei")"

  if ! uint256_eq "$simulated_claim_before_tx" "$pending_before_claim"; then
    echo "warning: pre-claim pendingFromIndex ($pending_before_claim) differed from claimRewards() eth_call simulation ($simulated_claim_before_tx)" >&2
    echo "note: this can happen on a live devnet if state changes between separate reads; the simulation is treated as authoritative" >&2
  fi
  if ! uint256_eq "$claim_gross_from_wallet_delta" "$simulated_claim_before_tx"; then
    echo "error: claimRewards gross wallet accounting mismatch for $REWARDS_ACCOUNT (expected eth_call=$simulated_claim_before_tx net_wallet_delta=$claim_balance_delta tx_fee=$claim_tx_fee_wei net_plus_fee=$claim_gross_from_wallet_delta)" >&2
    exit 1
  fi
  if ! uint256_eq "$debt_after_claim" "$expected_debt_after_claim"; then
    echo "error: claimRewards rewardDebt mismatch for $REWARDS_ACCOUNT (expected=$expected_debt_after_claim after=$debt_after_claim)" >&2
    exit 1
  fi
  if ! uint256_eq "$units_before_claim" "$units_after_claim"; then
    echo "error: claimRewards changed pool units for $REWARDS_ACCOUNT (before=$units_before_claim after=$units_after_claim)" >&2
    exit 1
  fi
  if [[ -z "$reward_reserve_credit_during_claim_block" ]] || [[ "$reward_reserve_credit_during_claim_block" =~ ^- ]]; then
    echo "error: claimRewards reserve accounting mismatch for $REWARDS_ACCOUNT (before=$reserve_before_claim pending=$pending_before_claim after=$reserve_after_claim impliedConcurrentCredit=$reward_reserve_credit_during_claim_block)" >&2
    exit 1
  fi

  echo "  -- post-claim reward accounting snapshot"
  echo "     units=$units_after_claim accRewardPerUnit=$acc_after_claim rewardDebt=$debt_after_claim pendingFromIndex=$pending_after_claim"
  echo "     rewardReserve=$reserve_after_claim liquidBalance=$liquid_after_claim user bond balance=$reward_user_balance_after"
  echo "     user bond balance delta (net of gas)=$claim_balance_delta"
  echo "     claimRewards tx fee (same bond/native denom)=$claim_tx_fee_wei"
  echo "     user wallet gross reward delta (net + fee)=$claim_gross_from_wallet_delta"
  echo "     expected claim from pre-claim index state=$pending_before_claim"
  echo "     expected claim from pre-tx eth_call simulation=$simulated_claim_before_tx"
  echo "     reserve movement explained as: rewardReserve_after = rewardReserve_before - claimed + concurrentRewardCredit"
  echo "     concurrentRewardCreditDuringClaimBlock=$reward_reserve_credit_during_claim_block"
  if uint256_gt "$acc_after_claim" "$acc_before_claim"; then
    echo "     note: accRewardPerUnit increased during/after the claim block; EndBlock auto-harvest likely credited fresh rewards after the user claim"
    echo "     note: pendingFromIndex(after)=$pending_after_claim means the user already has new unclaimed rewards from that later credit"
  else
    echo "     note: accRewardPerUnit unchanged across the claim block; no same-block post-claim harvest was observed"
  fi
  assert_liquid_reserve_invariants "rewards:post-claim" || exit 1
  log_contract_debug_state "rewards:after-claim"

  echo "    rewards phase: ok"
}

require_bins() {
  command -v jq >/dev/null 2>&1 || {
    echo "missing dependency: jq" >&2
    exit 1
  }
  command -v curl >/dev/null 2>&1 || {
    echo "missing dependency: curl" >&2
    exit 1
  }
  command -v cast >/dev/null 2>&1 || {
    echo "missing dependency: cast" >&2
    exit 1
  }
  command -v evmd >/dev/null 2>&1 || {
    echo "missing dependency: evmd" >&2
    exit 1
  }
}

main() {
  require_bins
  # Optional: bash community_pool_edge_cases.sh auth|drift|withdraw_sizing|liquidity|dust|rewards|all|auth,drift
  if [[ -n "${1:-}" ]]; then
    case "$1" in
      auth|drift|withdraw_sizing|liquidity|dust|rewards)
        COMMUNITY_POOL_EDGE_PHASES="$1"
        shift
        ;;
      all)
        COMMUNITY_POOL_EDGE_PHASES="auth,drift,withdraw_sizing,liquidity,dust,rewards"
        shift
        ;;
      *)
        if [[ "$1" == *","* ]]; then
          COMMUNITY_POOL_EDGE_PHASES="$1"
          shift
        else
          echo "error: unknown phase '$1' (expected auth|drift|withdraw_sizing|liquidity|dust|rewards|all|comma-separated list)" >&2
          exit 1
        fi
        ;;
    esac
  fi
  COMMUNITY_POOL_EDGE_PHASES="${COMMUNITY_POOL_EDGE_PHASES:-auth}"

  if [[ ! -f "$DEV_ACCOUNTS_FILE" ]]; then
    echo "error: missing $DEV_ACCOUNTS_FILE" >&2
    echo "hint: start a devnet with rebalance_scenario_runner or multi_node_startup so dev accounts exist" >&2
    exit 1
  fi

  echo "==> community_pool_edge_cases"
  echo "    COMMUNITY_POOL_EDGE_PHASES=${COMMUNITY_POOL_EDGE_PHASES}"
  echo "    NODE_RPC=$NODE_RPC EVM_RPC=$EVM_RPC (ensure_evm_rpc_ready may override EVM_RPC)"

  resolve_pool_evm_addr
  ensure_evm_rpc_ready || exit 1
  echo "    EVM_RPC=$EVM_RPC"

  local ran_any=false
  if phase_enabled auth; then
    run_phase_auth
    ran_any=true
  fi
  if phase_enabled drift; then
    run_phase_drift
    ran_any=true
  fi
  if phase_enabled withdraw_sizing; then
    run_phase_withdraw_sizing
    ran_any=true
  fi
  if phase_enabled liquidity; then
    run_phase_liquidity
    ran_any=true
  fi
  if phase_enabled dust; then
    run_phase_dust
    ran_any=true
  fi
  if phase_enabled rewards; then
    run_phase_rewards
    ran_any=true
  fi
  if [[ "$ran_any" == "false" ]]; then
    echo "    (no phases enabled matching COMMUNITY_POOL_EDGE_PHASES; nothing to do)"
  fi

  log_flow_section "Done" "community_pool_edge_cases finished successfully."
}

main "$@"
