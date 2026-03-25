#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GENERATED_DIR="${OUTPUT_DIR:-$ROOT_DIR/scripts/bridge/generated}"

"$ROOT_DIR/scripts/bridge/generate-artifacts.sh" >/dev/null

cat <<EOF
Hyperlane core deployment template
==================================

Generated inputs:
  - $GENERATED_DIR/og-evm-metadata.yaml
  - $GENERATED_DIR/core-config.yaml

Expected inputs:
  - OG_EVM_DOMAIN_ID
  - OG_EVM_CHAIN_ID
  - OG_EVM_RPC_URL
  - OG_EVM_MAILBOX
  - OG_EVM_IGP
  - OG_EVM_VALIDATOR_ANNOUNCE
  - OG_EVM_MERKLE_TREE_HOOK
  - HYPERLANE_OWNER
  - HYPERLANE_ISM_KIND
  - HYPERLANE_ISM_THRESHOLD

Suggested execution flow:
  1. Confirm the metadata file matches the actual chain identifiers.
  2. Run scripts/bridge/validate-artifacts.sh for file-level consistency.
  3. Confirm the mailbox owner multisig is prepared.
  4. Deploy or verify Hyperlane core contracts with the generated files.
  5. Transfer mailbox ownership to the intended owner.
  6. Run ACTIVE_RPC_VALIDATION=true scripts/bridge/validate-artifacts.sh.
  7. Record deployed addresses in the bridge ops log.
EOF
