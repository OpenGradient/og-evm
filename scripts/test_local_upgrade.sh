#!/usr/bin/env bash
#
# Test the full software-upgrade flow locally with two binaries.
#
# Usage:
#   scripts/test_local_upgrade.sh --old ./build/evmd-old --new ./build/evmd-new
#   scripts/test_local_upgrade.sh --old ./build/evmd-old --new ./build/evmd-new --upgrade-name v0.6.0-enable-missing-preinstalls
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Colours
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info()  { echo -e "${YELLOW}[INFO]${NC}  $*"; }
pass()  { echo -e "${GREEN}[PASS]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; }

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
OLD_BINARY=""
NEW_BINARY=""
UPGRADE_NAME="v0.6.0-enable-missing-preinstalls"
CHAIN_ID="upgrade-test-1"
DENOM="ogwei"
KEYRING="test"
KEYALGO="eth_secp256k1"
MONIKER="upgrade-test"
VALKEY="val0"
BASE_FEE=10000000
HOME_DIR=""
NODE_PID=""
KEEP_DATA=false

# Preinstall addresses to verify after upgrade
PREINSTALL_ADDRS=(
  "0x4e59b44847b379578588920ca78fbf26c0b4956c"
  "0xcA11bde05977b3631167028862bE2a173976CA11"
  "0x000000000022D473030F116dDEE9F6B43aC78BA3"
  "0x914d7Fec6aaC8cd542e72Bca78B30650d45643d7"
  "0x0000F90827F1C53a10cb7A02335B175320002935"
)

TEE_PRECOMPILE="0x0000000000000000000000000000000000000900"

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
usage() {
  cat <<'EOF'
Test the full software-upgrade flow locally with two binaries.

Usage:
  test_local_upgrade.sh [options]

Required:
  --old PATH           Path to the OLD evmd binary (pre-upgrade)
  --new PATH           Path to the NEW evmd binary (with upgrade handler)

Optional:
  --upgrade-name NAME  Upgrade plan name (default: v0.6.0-enable-missing-preinstalls)
  --chain-id ID        Chain ID (default: upgrade-test-1)
  --keep               Keep the test data directory after completion
  -h, --help           Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --old)           OLD_BINARY="$(realpath "$2")"; shift 2 ;;
    --new)           NEW_BINARY="$(realpath "$2")"; shift 2 ;;
    --upgrade-name)  UPGRADE_NAME="$2"; shift 2 ;;
    --chain-id)      CHAIN_ID="$2"; shift 2 ;;
    --keep)          KEEP_DATA=true; shift ;;
    -h|--help)       usage; exit 0 ;;
    *)               echo "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$OLD_BINARY" || -z "$NEW_BINARY" ]]; then
  echo "Error: --old and --new are required."
  usage
  exit 1
fi

for bin in "$OLD_BINARY" "$NEW_BINARY"; do
  if [[ ! -x "$bin" ]]; then
    echo "Error: binary not found or not executable: $bin"
    exit 1
  fi
done

command -v jq >/dev/null 2>&1 || { echo "Error: jq is required."; exit 1; }

# ---------------------------------------------------------------------------
# Setup temp directory and cleanup trap
# ---------------------------------------------------------------------------
HOME_DIR="$(mktemp -d "${TMPDIR:-/tmp}/evmd-upgrade-test-XXXXXX")"
LOG_DIR="$(pwd)"
LOG_OLD="$LOG_DIR/node-old.log"
LOG_NEW="$LOG_DIR/node-new.log"

cleanup() {
  info "Cleaning up..."
  if [[ -n "$NODE_PID" ]] && kill -0 "$NODE_PID" 2>/dev/null; then
    kill "$NODE_PID" 2>/dev/null || true
    wait "$NODE_PID" 2>/dev/null || true
  fi
  if [[ "$KEEP_DATA" == "false" ]]; then
    rm -rf "$HOME_DIR"
    info "Removed $HOME_DIR"
  else
    info "Data kept at $HOME_DIR"
  fi
}
trap cleanup EXIT INT TERM

info "Test directory: $HOME_DIR"
info "Old binary:     $OLD_BINARY"
info "New binary:     $NEW_BINARY"
info "Upgrade name:   $UPGRADE_NAME"
info "Chain ID:       $CHAIN_ID"
echo ""

# ---------------------------------------------------------------------------
# Helper: wait for node to produce blocks
# ---------------------------------------------------------------------------
wait_for_block() {
  local binary="$1"
  local target="${2:-1}"
  local timeout="${3:-120}"
  local start=$SECONDS

  info "Waiting for block height >= $target (timeout: ${timeout}s)..."
  while true; do
    local height
    height=$("$binary" status --home "$HOME_DIR" --node tcp://127.0.0.1:26657 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")
    if [[ "$height" -ge "$target" ]]; then
      info "Reached block height $height"
      return 0
    fi
    if (( SECONDS - start > timeout )); then
      fail "Timed out waiting for block $target (current: $height)"
      return 1
    fi
    sleep 1
  done
}

get_height() {
  local binary="$1"
  "$binary" status --home "$HOME_DIR" --node tcp://127.0.0.1:26657 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0"
}

# ---------------------------------------------------------------------------
# Step 1: Init chain with old binary
# ---------------------------------------------------------------------------
info "=== Step 1: Initializing chain with old binary ==="

"$OLD_BINARY" config set client chain-id "$CHAIN_ID" --home "$HOME_DIR"
"$OLD_BINARY" config set client keyring-backend "$KEYRING" --home "$HOME_DIR"
"$OLD_BINARY" keys add "$VALKEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR" 2>/dev/null
"$OLD_BINARY" init "$MONIKER" -o --chain-id "$CHAIN_ID" --home "$HOME_DIR" >/dev/null 2>&1

VAL_ADDR=$("$OLD_BINARY" keys show "$VALKEY" -a --keyring-backend "$KEYRING" --home "$HOME_DIR")
info "Validator address: $VAL_ADDR"

# ---------------------------------------------------------------------------
# Step 2: Customize genesis
# ---------------------------------------------------------------------------
info "=== Step 2: Customizing genesis ==="

GENESIS="$HOME_DIR/config/genesis.json"
TMP_GENESIS="${GENESIS}.tmp"

# Denom
jq ".app_state.staking.params.bond_denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq ".app_state.gov.deposit_params.min_deposit[0].denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq ".app_state.gov.params.min_deposit[0].denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq ".app_state.gov.params.expedited_min_deposit[0].denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq ".app_state.evm.params.evm_denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq ".app_state.mint.params.mint_denom=\"$DENOM\"" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

# Bank denom metadata
jq ".app_state.bank.denom_metadata=[{\"description\":\"Test token\",\"denom_units\":[{\"denom\":\"$DENOM\",\"exponent\":0,\"aliases\":[]},{\"denom\":\"OGETH\",\"exponent\":18,\"aliases\":[]}],\"base\":\"$DENOM\",\"display\":\"OGETH\",\"name\":\"ETH Token\",\"symbol\":\"OGETH\",\"uri\":\"\",\"uri_hash\":\"\"}]" "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

# Active static precompiles (10, WITHOUT TEE 0x0900)
jq '.app_state.evm.params.active_static_precompiles=["0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805","0x0000000000000000000000000000000000000806","0x0000000000000000000000000000000000000807"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

# ERC20
jq '.app_state.erc20.native_precompiles=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
jq '.app_state.erc20.token_pairs=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"ogwei",enabled:true}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

# Block gas limit
jq '.consensus.params.block.max_gas="10000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

# Short governance periods for testing
sed -i.bak 's/"max_deposit_period": "172800s"/"max_deposit_period": "30s"/g' "$GENESIS"
sed -i.bak 's/"voting_period": "172800s"/"voting_period": "30s"/g' "$GENESIS"
sed -i.bak 's/"expedited_voting_period": "86400s"/"expedited_voting_period": "15s"/g' "$GENESIS"
rm -f "${GENESIS}.bak"

info "Genesis customized"

# ---------------------------------------------------------------------------
# Step 3: Fund validator and create gentx
# ---------------------------------------------------------------------------
info "=== Step 3: Funding validator and creating gentx ==="

"$OLD_BINARY" genesis add-genesis-account "$VAL_ADDR" "100000000000000000000000000${DENOM}" --home "$HOME_DIR"
"$OLD_BINARY" genesis gentx "$VALKEY" "10000000000000000000000${DENOM}" \
  --gas-prices "${BASE_FEE}${DENOM}" \
  --keyring-backend "$KEYRING" \
  --chain-id "$CHAIN_ID" \
  --home "$HOME_DIR" \
  --ip "127.0.0.1"
"$OLD_BINARY" genesis collect-gentxs --home "$HOME_DIR" >/dev/null 2>&1
"$OLD_BINARY" genesis validate-genesis --home "$HOME_DIR" >/dev/null 2>&1
info "Genesis validated"

# ---------------------------------------------------------------------------
# Step 4: Start old binary
# ---------------------------------------------------------------------------
info "=== Step 4: Starting old binary ==="

"$OLD_BINARY" start \
  --pruning nothing \
  --log_level info \
  --minimum-gas-prices="0${DENOM}" \
  --evm.min-tip=0 \
  --home "$HOME_DIR" \
  --json-rpc.api eth,txpool,personal,net,debug,web3 \
  --chain-id "$CHAIN_ID" \
  >"$LOG_OLD" 2>&1 &
NODE_PID=$!
info "Old binary started (PID: $NODE_PID)"

wait_for_block "$OLD_BINARY" 3 60

# ---------------------------------------------------------------------------
# Step 5: Submit upgrade proposal
# ---------------------------------------------------------------------------
info "=== Step 5: Submitting upgrade proposal ==="

CURRENT_HEIGHT=$(get_height "$OLD_BINARY")
UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 20))
info "Current height: $CURRENT_HEIGHT, upgrade height: $UPGRADE_HEIGHT"

# Query gov authority
AUTHORITY=$("$OLD_BINARY" query auth module-account gov \
  --home "$HOME_DIR" --node tcp://127.0.0.1:26657 -o json 2>/dev/null \
  | jq -r '.account.base_account.address // .account.value.address // empty')

if [[ -z "$AUTHORITY" ]]; then
  fail "Could not determine gov authority address"
  exit 1
fi
info "Gov authority: $AUTHORITY"

# Write proposal JSON
PROPOSAL_FILE="$HOME_DIR/upgrade-proposal.json"
cat >"$PROPOSAL_FILE" <<EOF
{
  "messages": [
    {
      "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
      "authority": "$AUTHORITY",
      "plan": {
        "name": "$UPGRADE_NAME",
        "height": "$UPGRADE_HEIGHT"
      }
    }
  ],
  "metadata": "ipfs://CID",
  "deposit": "10000000${DENOM}",
  "title": "Software Upgrade: $UPGRADE_NAME",
  "summary": "Local test upgrade"
}
EOF

info "Submitting proposal..."
SUBMIT_RESULT=$("$OLD_BINARY" tx gov submit-proposal "$PROPOSAL_FILE" \
  --from "$VALKEY" \
  --home "$HOME_DIR" \
  --keyring-backend "$KEYRING" \
  --chain-id "$CHAIN_ID" \
  --node tcp://127.0.0.1:26657 \
  --fees "200000000000000${DENOM}" \
  --yes -o json 2>&1)
SUBMIT_CODE=$(echo "$SUBMIT_RESULT" | jq -r '.code // "unknown"' 2>/dev/null || echo "unknown")
SUBMIT_TXHASH=$(echo "$SUBMIT_RESULT" | jq -r '.txhash // "unknown"' 2>/dev/null || echo "unknown")
info "Submit tx hash: $SUBMIT_TXHASH (code: $SUBMIT_CODE)"
if [[ "$SUBMIT_CODE" != "0" ]]; then
  fail "Proposal submission failed"
  echo "$SUBMIT_RESULT"
  exit 1
fi

# Poll until the proposal appears on-chain (tx needs to land in a block)
info "Waiting for proposal to appear on-chain..."
PROPOSAL_ID=""
for i in $(seq 1 30); do
  PROPOSAL_ID=$("$OLD_BINARY" query gov proposals \
    --home "$HOME_DIR" --node tcp://127.0.0.1:26657 -o json 2>/dev/null \
    | jq -r '.proposals[-1].id // empty' 2>/dev/null || echo "")
  if [[ -n "$PROPOSAL_ID" ]]; then
    break
  fi
  sleep 1
done

if [[ -z "$PROPOSAL_ID" ]]; then
  fail "Could not find proposal after 30s"
  echo "--- Last 30 lines of old binary log ---"
  tail -30 "$LOG_OLD"
  exit 1
fi
info "Proposal ID: $PROPOSAL_ID"

# ---------------------------------------------------------------------------
# Step 6: Vote yes
# ---------------------------------------------------------------------------
info "=== Step 6: Voting YES on proposal ==="

VOTE_RESULT=$("$OLD_BINARY" tx gov vote "$PROPOSAL_ID" yes \
  --from "$VALKEY" \
  --home "$HOME_DIR" \
  --keyring-backend "$KEYRING" \
  --chain-id "$CHAIN_ID" \
  --node tcp://127.0.0.1:26657 \
  --fees "200000000000000${DENOM}" \
  --yes -o json 2>&1)
VOTE_CODE=$(echo "$VOTE_RESULT" | jq -r '.code // "unknown"' 2>/dev/null || echo "unknown")
VOTE_TXHASH=$(echo "$VOTE_RESULT" | jq -r '.txhash // "unknown"' 2>/dev/null || echo "unknown")
info "Vote tx hash: $VOTE_TXHASH (code: $VOTE_CODE)"
if [[ "$VOTE_CODE" != "0" ]]; then
  fail "Vote failed"
  echo "$VOTE_RESULT"
  exit 1
fi

info "Vote submitted, waiting for vote tx to land..."
sleep 5

# ---------------------------------------------------------------------------
# Step 7: Wait for proposal to pass
# ---------------------------------------------------------------------------
info "=== Step 7: Waiting for proposal to pass ==="

for i in $(seq 1 60); do
  STATUS=$("$OLD_BINARY" query gov proposal "$PROPOSAL_ID" \
    --home "$HOME_DIR" --node tcp://127.0.0.1:26657 -o json 2>/dev/null \
    | jq -r '.proposal.status // empty' 2>/dev/null || echo "")
  if [[ "$STATUS" == "PROPOSAL_STATUS_PASSED" ]]; then
    pass "Proposal passed"
    break
  fi
  if [[ "$STATUS" == "PROPOSAL_STATUS_REJECTED" || "$STATUS" == "PROPOSAL_STATUS_FAILED" ]]; then
    fail "Proposal $STATUS"
    exit 1
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# Step 8: Wait for old binary to halt at upgrade height
# ---------------------------------------------------------------------------
info "=== Step 8: Waiting for halt at upgrade height $UPGRADE_HEIGHT ==="

for i in $(seq 1 60); do
  if ! kill -0 "$NODE_PID" 2>/dev/null; then
    info "Old binary exited (upgrade halt)"
    break
  fi

  HEIGHT=$(get_height "$OLD_BINARY")
  if [[ "$HEIGHT" -ge "$UPGRADE_HEIGHT" ]]; then
    info "Reached upgrade height, waiting for halt..."
    sleep 2
    if ! kill -0 "$NODE_PID" 2>/dev/null; then
      info "Old binary exited (upgrade halt)"
      break
    fi
  fi
  sleep 1
done

# Kill if still running
if kill -0 "$NODE_PID" 2>/dev/null; then
  info "Force-killing old binary"
  kill "$NODE_PID" 2>/dev/null || true
  wait "$NODE_PID" 2>/dev/null || true
fi
NODE_PID=""

# Verify upgrade-info.json was written
UPGRADE_INFO="$HOME_DIR/data/upgrade-info.json"
if [[ -f "$UPGRADE_INFO" ]]; then
  pass "upgrade-info.json found: $(cat "$UPGRADE_INFO")"
else
  fail "upgrade-info.json not found — binary may not have halted at upgrade height"
  echo "--- Last 30 lines of old binary log ---"
  tail -30 "$LOG_OLD"
  exit 1
fi

# ---------------------------------------------------------------------------
# Step 9: Start new binary
# ---------------------------------------------------------------------------
info "=== Step 9: Starting new binary ==="

"$NEW_BINARY" start \
  --pruning nothing \
  --log_level info \
  --minimum-gas-prices="0${DENOM}" \
  --evm.min-tip=0 \
  --home "$HOME_DIR" \
  --json-rpc.api eth,txpool,personal,net,debug,web3 \
  --chain-id "$CHAIN_ID" \
  >"$LOG_NEW" 2>&1 &
NODE_PID=$!
info "New binary started (PID: $NODE_PID)"


wait_for_block "$NEW_BINARY" $((UPGRADE_HEIGHT + 2)) 60
pass "Chain resumed after upgrade"

# ---------------------------------------------------------------------------
# Step 10: Verify upgrade
# ---------------------------------------------------------------------------
info "=== Step 10: Verifying upgrade ==="


CHECKS_PASSED=0
CHECKS_TOTAL=0

# Helper: extract the first valid JSON object from output that may contain log lines
extract_json() {
  grep -E '^\s*\{' | head -1
}

# Check preinstall code at each address
for addr in "${PREINSTALL_ADDRS[@]}"; do
  CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
  RAW=$("$NEW_BINARY" query evm code "$addr" \
    --home "$HOME_DIR" --node tcp://127.0.0.1:26657 -o json 2>/dev/null || echo "")
  CODE=$(echo "$RAW" | extract_json | jq -r '.code // empty' 2>/dev/null || echo "")
  if [[ -n "$CODE" && "$CODE" != "0x" && "$CODE" != "" ]]; then
    pass "Preinstall code found at $addr"
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
  else
    fail "No preinstall code at $addr"
  fi
done

# Check TEE precompile is in active static precompiles
CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
RAW=$("$NEW_BINARY" query evm params \
  --home "$HOME_DIR" --node tcp://127.0.0.1:26657 -o json 2>/dev/null || echo "")
ACTIVE_PRECOMPILES=$(echo "$RAW" | extract_json | jq -r '.params.active_static_precompiles[]' 2>/dev/null || echo "")
if echo "$ACTIVE_PRECOMPILES" | grep -qi "$(echo "$TEE_PRECOMPILE" | tr '[:upper:]' '[:lower:]')"; then
  pass "TEE precompile $TEE_PRECOMPILE is active"
  CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
  fail "TEE precompile $TEE_PRECOMPILE NOT found in active precompiles"
  echo "Active precompiles: $ACTIVE_PRECOMPILES"
fi

# Smoke test: bank send
CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
SEND_RESULT=$("$NEW_BINARY" tx bank send "$VAL_ADDR" "$VAL_ADDR" "1${DENOM}" \
  --from "$VALKEY" \
  --home "$HOME_DIR" \
  --keyring-backend "$KEYRING" \
  --chain-id "$CHAIN_ID" \
  --node tcp://127.0.0.1:26657 \
  --fees "200000000000000${DENOM}" \
  --yes -o json 2>/dev/null || echo '{"code":1}')

TX_CODE=$(echo "$SEND_RESULT" | jq -r '.code // "1"' 2>/dev/null || echo "1")
if [[ "$TX_CODE" == "0" ]]; then
  pass "Bank send succeeded post-upgrade"
  CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
  fail "Bank send failed post-upgrade (code: $TX_CODE)"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=========================================="
if [[ "$CHECKS_PASSED" -eq "$CHECKS_TOTAL" ]]; then
  pass "All checks passed ($CHECKS_PASSED/$CHECKS_TOTAL)"
  echo "=========================================="
  # Kill the node before exiting
  if [[ -n "$NODE_PID" ]] && kill -0 "$NODE_PID" 2>/dev/null; then
    kill "$NODE_PID" 2>/dev/null || true
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=""
  fi
  exit 0
else
  fail "$CHECKS_PASSED/$CHECKS_TOTAL checks passed"
  echo "=========================================="
  echo ""
  echo "--- Last 30 lines of new binary log ---"
  tail -30 "$LOG_NEW"
  # Kill the node before exiting
  if [[ -n "$NODE_PID" ]] && kill -0 "$NODE_PID" 2>/dev/null; then
    kill "$NODE_PID" 2>/dev/null || true
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=""
  fi
  exit 1
fi
