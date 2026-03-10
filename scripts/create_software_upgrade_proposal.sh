#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Create a governance software-upgrade proposal JSON, and optionally submit it.

Usage:
  create_software_upgrade_proposal.sh [options]

Required:
  --upgrade-name NAME          Upgrade plan name (must match UpgradeName in app binary)
  --upgrade-height HEIGHT      Upgrade height
  --from KEY_OR_ADDRESS        Key name/address used to submit the proposal

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
  --fees COIN                  Tx fees when submitting (default: 2000000000000ogwei)
  --proposal-file PATH         Output proposal JSON path
  --submit                     Submit proposal tx after generating JSON
  -h, --help                   Show this help
EOF
}

CHAIN_ID="${CHAIN_ID:-10740}"
NODE="${NODE:-tcp://127.0.0.1:26657}"
HOME_DIR="${HOME_DIR:-$HOME/.og-evm-devnet/val0}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"

UPGRADE_NAME=""
UPGRADE_HEIGHT=""
FROM=""
AUTHORITY=""
TITLE=""
SUMMARY="Enable missing preinstalls and optional static precompile activation"
METADATA="ipfs://CID"
DEPOSIT="10000000ogwei"
FEES="2000000000000ogwei"
SUBMIT=false
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
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$UPGRADE_NAME" || -z "$UPGRADE_HEIGHT" || -z "$FROM" ]]; then
  echo "Error: --upgrade-name, --upgrade-height, and --from are required."
  usage
  exit 1
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
else
  echo ""
  echo "Submit command:"
  echo "  evmd tx gov submit-proposal \"$PROPOSAL_FILE\" --from \"$FROM\" --home \"$HOME_DIR\" --keyring-backend \"$KEYRING_BACKEND\" --chain-id \"$CHAIN_ID\" --node \"$NODE\" --fees \"$FEES\" -y"
fi
