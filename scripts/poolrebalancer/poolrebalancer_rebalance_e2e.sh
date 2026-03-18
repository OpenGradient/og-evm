#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-"tcp://127.0.0.1:26657"}"
CHAIN_ID="${CHAIN_ID:-10740}"
KEYRING="${KEYRING:-test}"
HOME0="$BASEDIR/val0"

# ---- Test knobs (override via env) ----
POOLREBALANCER_MAX_TARGET_VALIDATORS="${POOLREBALANCER_MAX_TARGET_VALIDATORS:-3}"
POOLREBALANCER_THRESHOLD_BP="${POOLREBALANCER_THRESHOLD_BP:-1}"
POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-1}"
POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-1000000000000000000}" # 1e18
POOLREBALANCER_USE_UNDELEGATE_FALLBACK="${POOLREBALANCER_USE_UNDELEGATE_FALLBACK:-true}"

# staking params for making undelegation fallback observable
STAKING_UNBONDING_TIME="${STAKING_UNBONDING_TIME:-30s}"
STAKING_MAX_ENTRIES="${STAKING_MAX_ENTRIES:-100}"

TX_FEES="${TX_FEES:-200000000000000ogwei}" # denom will be rewritten after chain start

# Delegation imbalance amounts (safe under default funding of 1e24 ogwei).
IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION:-200000000000000000000000ogwei}" # denom rewritten after chain start
IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION:-100ogwei}"

POLL_SAMPLES="${POLL_SAMPLES:-25}"
POLL_SLEEP_SECS="${POLL_SLEEP_SECS:-2}"

usage() {
  cat <<EOF
Usage: $0

Runs a local E2E sanity test for x/poolrebalancer:
- Generates a 3-validator localnet using multi_node_startup.sh
- Patches genesis to enable poolrebalancer for dev0
- Starts val0/val1/val2
- Delegates an imbalanced stake distribution from dev0
- Verifies poolrebalancer schedules pending redelegations capped by max_move_per_op

Environment variables:
  BASEDIR                           Localnet base dir (default: $HOME/.og-evm-devnet)
  NODE_RPC                          RPC endpoint (default: tcp://127.0.0.1:26657)
  CHAIN_ID                          Chain ID (default: 10740)
  TX_FEES                           Fees for txs (default: $TX_FEES)

  VAL0_MNEMONIC / VAL1_MNEMONIC / VAL2_MNEMONIC
                                   Required for genesis generation. Provide 24-word BIP39 mnemonics.

  POOLREBALANCER_MAX_TARGET_VALIDATORS
  POOLREBALANCER_THRESHOLD_BP
  POOLREBALANCER_MAX_OPS_PER_BLOCK
  POOLREBALANCER_MAX_MOVE_PER_OP
  POOLREBALANCER_USE_UNDELEGATE_FALLBACK

  STAKING_UNBONDING_TIME            Reduce so pending queues mature quickly (default: 30s)
  STAKING_MAX_ENTRIES             Raise to allow more in-flight undelegations (default: 100)

  IMBALANCE_MAIN_DELEGATION         Large delegation to validator[0]
  IMBALANCE_MINOR_DELEGATION        Small delegations to validator[1], validator[2]

EOF
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}

stop_nodes() {
  # Be aggressive: localnet nodes are started via `evmd start ...` by multi_node_startup.sh.
  pkill -f "evmd start" >/dev/null 2>&1 || true
  pkill -f "multi_node_startup.sh" >/dev/null 2>&1 || true
  # give the OS a moment to release ports
  sleep 1
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
  # Prefer CometBFT RPC /tx?hash=0x... which becomes available on commit.
  local rpc_http="http://127.0.0.1:26657"
  for _ in $(seq 1 40); do
    local resp
    resp="$(curl -sS --max-time 1 "${rpc_http}/tx?hash=0x${txhash}" 2>/dev/null || true)"
    # When not found yet, CometBFT returns an error JSON (still non-empty). Only treat it as committed
    # once it has a .result.tx_result object.
    if echo "$resp" | jq -e '.result.tx_result' >/dev/null 2>&1; then
      # Ensure the tx succeeded (code=0). A failed tx can still be committed and show up in /tx.
      local code
      code="$(echo "$resp" | jq -r '.result.tx_result.code' 2>/dev/null || echo 1)"
      if [[ "$code" == "0" ]]; then
        return 0
      fi
      echo "tx committed but failed (code=$code): $txhash" >&2
      echo "$resp" | jq -r '.result.tx_result.log' >&2 || true
      return 1
    fi
    # Fallback to the app query path (may require indexing to be enabled).
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
  # Some CLI versions return txhash even when CheckTx fails. Always check `.code`.
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

      # Common transient: sequence mismatch if previous tx hasn't fully propagated.
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

  local badAmt
  badAmt="$(echo "$json" | jq -r --argjson cap "$cap" '[.redelegations[] | (.amount.amount|tonumber) > $cap] | any')"
  if [[ "$badAmt" != "false" ]]; then
    echo "FAIL: found pending amount > max_move_per_op" >&2
    return 1
  fi

  # Transitive safety: no src is also a dst among in-flight entries.
  local badTrans
  badTrans="$(echo "$json" | jq -r '([.redelegations[].src_validator_address] | unique) as $srcs | ([.redelegations[].dst_validator_address] | unique) as $dsts | ([ $srcs[] | . as $s | (($dsts | index($s)) != null) ] | any)')"
  if [[ "$badTrans" != "false" ]]; then
    echo "FAIL: transitive safety violated (src appears in dst set)" >&2
    return 1
  fi
}

main() {
  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage; exit 0
  fi

  require_bin jq
  require_bin curl
  require_bin evmd

  if [[ -z "${VAL0_MNEMONIC:-}" || -z "${VAL1_MNEMONIC:-}" || -z "${VAL2_MNEMONIC:-}" ]]; then
    echo "VAL0_MNEMONIC/VAL1_MNEMONIC/VAL2_MNEMONIC must be set" >&2
    exit 1
  fi

  echo "==> Stopping any existing localnet"
  stop_nodes

  echo "==> Generating genesis (3 validators) at $BASEDIR"
  # multi_node_startup.sh prints a lot of init output; silence both stdout/stderr
  (cd "$ROOT_DIR" && GENERATE_GENESIS=true ./multi_node_startup.sh -y >/dev/null 2>&1)

  local del_addr
  del_addr="$(dev0_address_from_file)"
  local del_mnemonic
  del_mnemonic="$(dev0_mnemonic_from_file)"

  echo "==> Pool delegator (dev0) = $del_addr"
  echo "==> Patching genesis staking params (unbonding_time + max_entries)"
  patch_genesis_staking_params
  echo "==> Patching genesis poolrebalancer params"
  patch_genesis_poolrebalancer_params "$del_addr"

  echo "==> Starting validators"
  mkdir -p "$BASEDIR/logs"
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=0 ./multi_node_startup.sh >"$BASEDIR/logs/val0.log" 2>&1 &)
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=1 ./multi_node_startup.sh >"$BASEDIR/logs/val1.log" 2>&1 &)
  (cd "$ROOT_DIR" && START_VALIDATOR=true NODE_NUMBER=2 ./multi_node_startup.sh >"$BASEDIR/logs/val2.log" 2>&1 &)

  echo "==> Waiting for block production"
  local h
  h="$(wait_for_height 60)"
  echo "height=$h"

  # Discover bond denom for this chain and rewrite denom-bearing knobs to match.
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
      assert_pending_invariants "$j" "$POOLREBALANCER_MAX_MOVE_PER_OP"
      local pendingUndel
      pendingUndel="$(evmd query poolrebalancer pending-undelegations --node "$NODE_RPC" -o json | jq -r '.undelegations | length')"
      echo "pending_undelegations=$pendingUndel"
      echo "PASS: pending redelegations observed and invariants hold"
      exit 0
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

