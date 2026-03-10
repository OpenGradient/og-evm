#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Create a governance software-upgrade proposal JSON, and optionally submit it.

Usage:
  create_software_upgrade_proposal.sh [options]

Required:
  --upgrade-name NAME          Upgrade plan name (must match UpgradeName in app binary)
  --from KEY_OR_ADDRESS        Key name/address used to submit the proposal

Optional (auto-detected):
  --upgrade-height HEIGHT      Upgrade height (default: current_height + 60)

Optional:
  --chain-id ID                Chain ID (default: env CHAIN_ID or 10740)
  --node NODE                  RPC node (default: env NODE or tcp://127.0.0.1:26657)
  --home PATH                  Node home for keyring/query (default: env HOME_DIR or ~/.og-evm-devnet/val0)
  --keyring-backend BACKEND    Keyring backend (default: env KEYRING_BACKEND or test)
  --authority ADDRESS          Gov module authority address (auto-queried if omitted)
  --title TEXT                 Proposal title (default: "Software Upgrade: <name>")
  --summary TEXT               Proposal summary
  --metadata URI               Proposal metadata URI (default: ipfs://CID)
  --deposit COIN               Deposit (default: 10000000ogwei)
  --fees COIN                  Tx fees when submitting (default: 3000000000000ogwei)
  --proposal-file PATH         Output proposal JSON path
  --submit                     Submit proposal tx after generating JSON
  --vote                       Vote YES from all validators after submitting (implies --submit)
  --basedir PATH               Base directory for validator homes (default: parent of --home)
  --num-validators N           Number of validators to vote from (default: 3)
  -h, --help                   Show this help
EOF
}

CHAIN_ID="${CHAIN_ID:-10740}"
NODE="${NODE:-https://ogrpcdevnet.opengradient.ai/}"
HOME_DIR="${HOME_DIR:-$HOME/.evmd}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"

UPGRADE_NAME="v0.6.0-enable-missing-preinstalls"
UPGRADE_HEIGHT=""
FROM=""
AUTHORITY=""
TITLE=""
SUMMARY="Enable missing preinstalls and optional static precompile activation"
METADATA="ipfs://CID"
DEPOSIT="10000000ogwei"
FEES="4000000000000ogwei"
SUBMIT=false
VOTE=false
BASEDIR=""
NUM_VALIDATORS=2
VAL_KEY_PREFIX="val-0-"
PROPOSAL_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --upgrade-name) UPGRADE_NAME="$2"; shift 2 ;;
    --upgrade-height) UPGRADE_HEIGHT="$2"; shift 2 ;;
    --from) FROM="$2"; shift 2 ;;
    --chain-id) CHAIN_ID="$2"; shift 2 ;;
    --node) NODE="$2"; shift 2 ;;
    --home) HOME_DIR="$2"; shift 2 ;;
    --keyring-backend) KEYRING_BACKEND="$2"; shift 2 ;;
    --authority) AUTHORITY="$2"; shift 2 ;;
    --title) TITLE="$2"; shift 2 ;;
    --summary) SUMMARY="$2"; shift 2 ;;
    --metadata) METADATA="$2"; shift 2 ;;
    --deposit) DEPOSIT="$2"; shift 2 ;;
    --fees) FEES="$2"; shift 2 ;;
    --proposal-file) PROPOSAL_FILE="$2"; shift 2 ;;
    --submit) SUBMIT=true; shift ;;
    --vote) VOTE=true; shift ;;
    --basedir) BASEDIR="$2"; shift 2 ;;
    --num-validators) NUM_VALIDATORS="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if [[ "$VOTE" == "true" ]]; then
  SUBMIT=true
fi

if [[ -z "$UPGRADE_NAME" || -z "$FROM" ]]; then
  echo "Error: --upgrade-name and --from are required."
  usage
  exit 1
fi

if [[ -z "$UPGRADE_HEIGHT" ]]; then
  echo "Querying current block height..."
  CURRENT_HEIGHT=$(evmd status --node "$NODE" --home "$HOME_DIR" -o json 2>/dev/null \
    | jq -r '.sync_info.latest_block_height // .SyncInfo.latest_block_height // empty')
  if [[ -z "$CURRENT_HEIGHT" ]]; then
    echo "Error: could not query block height. Pass --upgrade-height explicitly."
    exit 1
  fi
  UPGRADE_HEIGHT=$((CURRENT_HEIGHT + 60))
  echo "Current block height: $CURRENT_HEIGHT"
  echo "Upgrade height:       $UPGRADE_HEIGHT (current + 60)"
fi

if [[ -z "$BASEDIR" ]]; then
  BASEDIR="$(dirname "$HOME_DIR")"
fi

if [[ -z "$TITLE" ]]; then
  TITLE="Software Upgrade: $UPGRADE_NAME"
fi

if [[ -z "$PROPOSAL_FILE" ]]; then
  PROPOSAL_FILE="/tmp/software-upgrade-${UPGRADE_NAME}-${UPGRADE_HEIGHT}.json"
fi

if [[ -z "$AUTHORITY" ]]; then
  command -v jq >/dev/null 2>&1 || { echo "Error: jq is required to auto-detect --authority."; exit 1; }
  AUTHORITY_JSON="$(evmd query auth module-account gov --node "$NODE" --home "$HOME_DIR" -o json)"
  AUTHORITY="$(echo "$AUTHORITY_JSON" | jq -r '.account.base_account.address // .account.value.address // empty')"
  if [[ -z "$AUTHORITY" ]]; then
    echo "Error: unable to determine gov authority address. Pass --authority explicitly."
    exit 1
  fi
fi

mkdir -p "$(dirname "$PROPOSAL_FILE")"

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
  "metadata": "$METADATA",
  "deposit": "$DEPOSIT",
  "title": "$TITLE",
  "summary": "$SUMMARY"
}
EOF

echo "Wrote proposal file: $PROPOSAL_FILE"
echo "Upgrade Name:        $UPGRADE_NAME"
echo "Upgrade Height:      $UPGRADE_HEIGHT"
echo "Gov Authority:       $AUTHORITY"
echo "Deposit:             $DEPOSIT"

if [[ "$SUBMIT" == "true" ]]; then
  echo "Submitting proposal..."
  evmd tx gov submit-proposal "$PROPOSAL_FILE" \
    --from "$FROM" \
    --home "$HOME_DIR" \
    --keyring-backend "$KEYRING_BACKEND" \
    --chain-id "$CHAIN_ID" \
    --node "$NODE" \
    --fees "$FEES" \
    -y

  if [[ "$VOTE" == "true" ]]; then
    echo ""
    echo "Waiting for tx inclusion..."
    sleep 10

    echo "Querying latest proposal ID..."
    PROPOSAL_ID=$(evmd query gov proposals --node "$NODE" --home "$HOME_DIR" -o json \
      | jq -r '.proposals[-1].id // .proposals[-1].proposal_id // empty')

    if [[ -z "$PROPOSAL_ID" ]]; then
      echo "Error: could not determine proposal ID. Vote manually."
      exit 1
    fi

    echo "Voting YES on proposal $PROPOSAL_ID from $NUM_VALIDATORS validator(s)..."
    echo ""

    for i in $(seq 0 $((NUM_VALIDATORS - 1))); do
      VAL_KEY="${VAL_KEY_PREFIX}${i}"

      echo "  ${VAL_KEY} voting YES..."
      evmd tx gov vote "$PROPOSAL_ID" yes \
        --from "$VAL_KEY" \
        --home "$HOME_DIR" \
        --keyring-backend "$KEYRING_BACKEND" \
        --chain-id "$CHAIN_ID" \
        --node "$NODE" \
        --fees "$FEES" \
        -y
    done

    echo ""
    echo "All votes submitted for proposal $PROPOSAL_ID"
    echo "Voting period is 30s. Check status:"
    echo "  evmd query gov proposal $PROPOSAL_ID --node $NODE -o json | jq '.status'"
  fi
else
  echo ""
  echo "Submit command:"
  echo "  evmd tx gov submit-proposal \"$PROPOSAL_FILE\" --from \"$FROM\" --home \"$HOME_DIR\" --keyring-backend \"$KEYRING_BACKEND\" --chain-id \"$CHAIN_ID\" --node \"$NODE\" --fees \"$FEES\" -y"
fi
