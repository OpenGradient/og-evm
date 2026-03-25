#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/scripts/bridge/generated}"

mkdir -p "$OUTPUT_DIR" "$OUTPUT_DIR/proposals"

OG_EVM_CHAIN_NAME="${OG_EVM_CHAIN_NAME:-og-evm}"
BASE_CHAIN_NAME="${BASE_CHAIN_NAME:-base}"

OG_EVM_DOMAIN_ID="${OG_EVM_DOMAIN_ID:-10740}"
BASE_DOMAIN_ID="${BASE_DOMAIN_ID:-8453}"
OG_EVM_CHAIN_ID="${OG_EVM_CHAIN_ID:-10740}"

OG_EVM_PROTOCOL="${OG_EVM_PROTOCOL:-ethereum}"
BASE_PROTOCOL="${BASE_PROTOCOL:-ethereum}"

OG_EVM_RPC_URL="${OG_EVM_RPC_URL:-http://127.0.0.1:8545}"
BASE_RPC_URL="${BASE_RPC_URL:-https://mainnet.base.org}"

OG_EVM_MAILBOX="${OG_EVM_MAILBOX:-0x0000000000000000000000000000000000000000}"
BASE_MAILBOX="${BASE_MAILBOX:-0x0000000000000000000000000000000000000000}"
OG_EVM_IGP="${OG_EVM_IGP:-0x0000000000000000000000000000000000000000}"
BASE_IGP="${BASE_IGP:-0x0000000000000000000000000000000000000000}"
OG_EVM_VALIDATOR_ANNOUNCE="${OG_EVM_VALIDATOR_ANNOUNCE:-0x0000000000000000000000000000000000000000}"
BASE_VALIDATOR_ANNOUNCE="${BASE_VALIDATOR_ANNOUNCE:-0x0000000000000000000000000000000000000000}"
OG_EVM_MERKLE_TREE_HOOK="${OG_EVM_MERKLE_TREE_HOOK:-0x0000000000000000000000000000000000000000}"
BASE_MERKLE_TREE_HOOK="${BASE_MERKLE_TREE_HOOK:-0x0000000000000000000000000000000000000000}"

OG_EVM_MAILBOX_OWNER="${OG_EVM_MAILBOX_OWNER:-0x0000000000000000000000000000000000000000}"
HYPERLANE_ENVIRONMENT="${HYPERLANE_ENVIRONMENT:-testnet}"
HYPERLANE_OWNER="${HYPERLANE_OWNER:-0x0000000000000000000000000000000000000000}"
HYPERLANE_FEE_BENEFICIARY="${HYPERLANE_FEE_BENEFICIARY:-0x0000000000000000000000000000000000000000}"
HYPERLANE_ISM_KIND="${HYPERLANE_ISM_KIND:-multisig}"
HYPERLANE_ISM_THRESHOLD="${HYPERLANE_ISM_THRESHOLD:-1}"
HYPERLANE_VALIDATOR_1="${HYPERLANE_VALIDATOR_1:-0x0000000000000000000000000000000000000000}"
HYPERLANE_VALIDATOR_2="${HYPERLANE_VALIDATOR_2:-0x0000000000000000000000000000000000000000}"
HYPERLANE_VALIDATOR_3="${HYPERLANE_VALIDATOR_3:-0x0000000000000000000000000000000000000000}"

HYPERLANE_LOCAL_ROUTER="${HYPERLANE_LOCAL_ROUTER:-0x0000000000000000000000000000000000000000}"
BASE_COLLATERAL_ROUTER="${BASE_COLLATERAL_ROUTER:-0x0000000000000000000000000000000000000000}"
BASE_TOKEN_ADDRESS="${BASE_TOKEN_ADDRESS:-0x0000000000000000000000000000000000000000}"
BASE_RECIPIENT="${BASE_RECIPIENT:-0x0000000000000000000000000000000000000000}"

BRIDGE_AUTHORITY="${BRIDGE_AUTHORITY:-cosmos1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx}"
BRIDGE_MAX_TRANSFER_AMOUNT="${BRIDGE_MAX_TRANSFER_AMOUNT:-0}"
BRIDGE_ENABLED="${BRIDGE_ENABLED:-true}"
BRIDGE_PROPOSAL_DEPOSIT="${BRIDGE_PROPOSAL_DEPOSIT:-10000000ogwei}"
BRIDGE_METADATA_CID="${BRIDGE_METADATA_CID:-ipfs://REPLACE_WITH_METADATA_CID}"

ALLOW_LOCAL_CHECKPOINT_SYNCERS="${HYPERLANE_ALLOW_LOCAL_CHECKPOINT_SYNCERS:-true}"
OG_EVM_RPC_URLS="${OG_EVM_RPC_URLS:-$OG_EVM_RPC_URL}"
BASE_RPC_URLS="${BASE_RPC_URLS:-$BASE_RPC_URL}"

json_array() {
	python3 - "$1" <<'PY'
import json
import sys

items = [item.strip() for item in sys.argv[1].split(",") if item.strip()]
print(json.dumps(items))
PY
}

json_bool() {
	case "$1" in
		true|TRUE|True|1) printf 'true' ;;
		false|FALSE|False|0) printf 'false' ;;
		*)
			echo "invalid boolean value: $1" >&2
			exit 1
			;;
	esac
}

OG_EVM_RPC_URLS_JSON="$(json_array "$OG_EVM_RPC_URLS")"
BASE_RPC_URLS_JSON="$(json_array "$BASE_RPC_URLS")"
ALLOW_LOCAL_CHECKPOINT_SYNCERS_JSON="$(json_bool "$ALLOW_LOCAL_CHECKPOINT_SYNCERS")"
BRIDGE_ENABLED_JSON="$(json_bool "$BRIDGE_ENABLED")"

cat >"$OUTPUT_DIR/og-evm-metadata.yaml" <<EOF
name: $OG_EVM_CHAIN_NAME
displayName: $OG_EVM_CHAIN_NAME
domainId: $OG_EVM_DOMAIN_ID
chainId: $OG_EVM_CHAIN_ID
protocol: $OG_EVM_PROTOCOL
rpcUrl: $OG_EVM_RPC_URL
mailbox: $OG_EVM_MAILBOX
interchainGasPaymaster: $OG_EVM_IGP
validatorAnnounce: $OG_EVM_VALIDATOR_ANNOUNCE
merkleTreeHook: $OG_EVM_MERKLE_TREE_HOOK
EOF

cat >"$OUTPUT_DIR/core-config.yaml" <<EOF
environment: $HYPERLANE_ENVIRONMENT
owner: $HYPERLANE_OWNER
mailbox:
  address: $OG_EVM_MAILBOX
  owner: $OG_EVM_MAILBOX_OWNER
igp:
  address: $OG_EVM_IGP
  beneficiary: $HYPERLANE_FEE_BENEFICIARY
ism:
  kind: $HYPERLANE_ISM_KIND
  threshold: $HYPERLANE_ISM_THRESHOLD
  validators:
    - $HYPERLANE_VALIDATOR_1
    - $HYPERLANE_VALIDATOR_2
    - $HYPERLANE_VALIDATOR_3
EOF

cat >"$OUTPUT_DIR/warp-config.yaml" <<EOF
name: og-native
type: collateral
localDomain: $OG_EVM_DOMAIN_ID
remoteDomain: $BASE_DOMAIN_ID
mailbox: $OG_EVM_MAILBOX
bridgePrecompile: 0x0000000000000000000000000000000000000A00
localRouter: $HYPERLANE_LOCAL_ROUTER
remoteRouter: $BASE_COLLATERAL_ROUTER
collateralToken: $BASE_TOKEN_ADDRESS
recipient: $BASE_RECIPIENT
EOF

python3 - "$OUTPUT_DIR/agent-config.json" <<PY
import json
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
config = {
    "allowLocalCheckpointSyncers": json.loads('$ALLOW_LOCAL_CHECKPOINT_SYNCERS_JSON'),
    "chains": {
        "$OG_EVM_CHAIN_NAME": {
            "domain": int("$OG_EVM_DOMAIN_ID"),
            "protocol": "$OG_EVM_PROTOCOL",
            "mailbox": "$OG_EVM_MAILBOX",
            "interchainGasPaymaster": "$OG_EVM_IGP",
            "validatorAnnounce": "$OG_EVM_VALIDATOR_ANNOUNCE",
            "merkleTreeHook": "$OG_EVM_MERKLE_TREE_HOOK",
            "customRpcUrls": $OG_EVM_RPC_URLS_JSON,
        },
        "$BASE_CHAIN_NAME": {
            "domain": int("$BASE_DOMAIN_ID"),
            "protocol": "$BASE_PROTOCOL",
            "mailbox": "$BASE_MAILBOX",
            "interchainGasPaymaster": "$BASE_IGP",
            "validatorAnnounce": "$BASE_VALIDATOR_ANNOUNCE",
            "merkleTreeHook": "$BASE_MERKLE_TREE_HOOK",
            "customRpcUrls": $BASE_RPC_URLS_JSON,
        },
    },
}
output.write_text(json.dumps(config, indent=2) + "\n", encoding="ascii")
PY

python3 - "$OUTPUT_DIR/proposals/enable-bridge.json" <<PY
import json
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
proposal = {
    "messages": [
        {
            "@type": "/cosmos.bridge.v1.MsgUpdateParams",
            "authority": "$BRIDGE_AUTHORITY",
            "params": {
                "authorizedContract": "$HYPERLANE_LOCAL_ROUTER",
                "hyperlaneMailbox": "$OG_EVM_MAILBOX",
                "baseDomainId": int("$BASE_DOMAIN_ID"),
                "enabled": json.loads('$BRIDGE_ENABLED_JSON'),
                "maxTransferAmount": "$BRIDGE_MAX_TRANSFER_AMOUNT",
            },
        }
    ],
    "metadata": "$BRIDGE_METADATA_CID",
    "deposit": "$BRIDGE_PROPOSAL_DEPOSIT",
    "title": "Enable Hyperlane Bridge",
    "summary": "Enable the bridge module and point it at the deployed Hyperlane mailbox and router.",
}
output.write_text(json.dumps(proposal, indent=2) + "\n", encoding="ascii")
PY

python3 - "$OUTPUT_DIR/proposals/set-authorized-contract.json" <<PY
import json
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
proposal = {
    "messages": [
        {
            "@type": "/cosmos.bridge.v1.MsgSetAuthorizedContract",
            "authority": "$BRIDGE_AUTHORITY",
            "contractAddress": "$HYPERLANE_LOCAL_ROUTER",
        }
    ],
    "metadata": "$BRIDGE_METADATA_CID",
    "deposit": "$BRIDGE_PROPOSAL_DEPOSIT",
    "title": "Set Authorized Bridge Contract",
    "summary": "Update the bridge precompile authorization target to the deployed HypOGNative contract.",
}
output.write_text(json.dumps(proposal, indent=2) + "\n", encoding="ascii")
PY

cat <<EOF
Generated bridge deployment artifacts in:
  $OUTPUT_DIR

Files:
  - $OUTPUT_DIR/og-evm-metadata.yaml
  - $OUTPUT_DIR/core-config.yaml
  - $OUTPUT_DIR/warp-config.yaml
  - $OUTPUT_DIR/agent-config.json
  - $OUTPUT_DIR/proposals/enable-bridge.json
  - $OUTPUT_DIR/proposals/set-authorized-contract.json
EOF
