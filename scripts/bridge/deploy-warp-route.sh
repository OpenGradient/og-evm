#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GENERATED_DIR="${OUTPUT_DIR:-$ROOT_DIR/scripts/bridge/generated}"

"$ROOT_DIR/scripts/bridge/generate-artifacts.sh" >/dev/null

cat <<EOF
Hyperlane warp-route deployment template
========================================

This bridge uses a custom og-evm router contract named HypOGNative and a Base collateral router.
Generated input:
  - $GENERATED_DIR/warp-config.yaml

Expected inputs:
  - OG_EVM_DOMAIN_ID
  - BASE_DOMAIN_ID
  - OG_EVM_MAILBOX
  - HYPERLANE_LOCAL_ROUTER
  - BASE_COLLATERAL_ROUTER
  - BASE_TOKEN_ADDRESS
  - BASE_RECIPIENT

Suggested execution flow:
  1. Deploy HypOGNative on og-evm with constructor args (mailbox, igp, owner).
  2. Enroll the Base router address on og-evm.
  3. Deploy or verify the Base collateral router.
  4. Submit the generated governance proposal to set the authorized router.
  5. Start validators and relayer with the generated agent config.
  6. Run a small round-trip transfer.
EOF
