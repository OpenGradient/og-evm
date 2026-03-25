#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARTIFACT_DIR="${1:-${OUTPUT_DIR:-$ROOT_DIR/scripts/bridge/generated}}"
STRICT="${STRICT:-false}"
ACTIVE_RPC_VALIDATION="${ACTIVE_RPC_VALIDATION:-false}"
RPC_TIMEOUT_SECONDS="${RPC_TIMEOUT_SECONDS:-10}"
OG_EVM_CHAIN_NAME="${OG_EVM_CHAIN_NAME:-og-evm}"
BASE_CHAIN_NAME="${BASE_CHAIN_NAME:-base}"

METADATA_FILE="$ARTIFACT_DIR/og-evm-metadata.yaml"
CORE_FILE="$ARTIFACT_DIR/core-config.yaml"
WARP_FILE="$ARTIFACT_DIR/warp-config.yaml"
AGENT_FILE="$ARTIFACT_DIR/agent-config.json"
ENABLE_PROPOSAL_FILE="$ARTIFACT_DIR/proposals/enable-bridge.json"
AUTHORIZED_PROPOSAL_FILE="$ARTIFACT_DIR/proposals/set-authorized-contract.json"

fail() {
  echo "bridge artifact validation failed: $*" >&2
  exit 1
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

is_truthy() {
  case "$1" in
    true|TRUE|True|1) return 0 ;;
    *) return 1 ;;
  esac
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

yaml_scalar() {
  local file="$1"
  local key="$2"
  local value

  value="$(sed -n "s/^${key}: //p" "$file" | head -n 1)"
  value="$(trim "$value")"
  [[ -n "$value" ]] || fail "missing ${key} in $(basename "$file")"
  printf '%s' "$value"
}

yaml_nested_scalar() {
  local file="$1"
  local section="$2"
  local key="$3"
  local value

  value="$(
    awk -v section="${section}:" -v key="  ${key}: " '
      $0 == section { in_section = 1; next }
      in_section && index($0, key) == 1 {
        sub(key, "", $0)
        print $0
        exit
      }
      in_section && $0 !~ /^  / { exit }
    ' "$file"
  )"
  value="$(trim "$value")"
  [[ -n "$value" ]] || fail "missing ${section}.${key} in $(basename "$file")"
  printf '%s' "$value"
}

json_expr() {
  local file="$1"
  local expr="$2"
  python3 - "$file" "$expr" <<'PY'
import json
import sys

path = sys.argv[1]
expr = sys.argv[2]
data = json.load(open(path, encoding="utf-8"))
allowed = {"data": data, "len": len}
value = eval(expr, {"__builtins__": {}}, allowed)
if isinstance(value, bool):
    print("true" if value else "false")
elif isinstance(value, (int, float)):
    print(value)
else:
    print(value)
PY
}

rpc_call() {
  local url="$1"
  local method="$2"
  local params_json="${3:-[]}"

  python3 - "$url" "$method" "$params_json" "$RPC_TIMEOUT_SECONDS" <<'PY'
import json
import sys
import urllib.error
import urllib.request

url = sys.argv[1]
method = sys.argv[2]
params = json.loads(sys.argv[3])
timeout = float(sys.argv[4])
payload = json.dumps(
    {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
).encode("utf-8")
request = urllib.request.Request(
    url,
    data=payload,
    headers={"Content-Type": "application/json"},
    method="POST",
)

try:
    with urllib.request.urlopen(request, timeout=timeout) as response:
        body = json.load(response)
except urllib.error.URLError as exc:
    print(f"rpc request failed: {exc}", file=sys.stderr)
    sys.exit(1)

if "error" in body:
    print(f"rpc response returned error: {body['error']}", file=sys.stderr)
    sys.exit(1)

if "result" not in body:
    print("rpc response missing result field", file=sys.stderr)
    sys.exit(1)

result = body["result"]
if isinstance(result, (dict, list)):
    print(json.dumps(result))
else:
    print(result)
PY
}

hex_to_dec() {
  local value="$1"
  python3 - "$value" <<'PY'
import sys

value = sys.argv[1].strip()
if not value.startswith("0x"):
    raise SystemExit(f"expected hex value, got: {value}")
print(int(value, 16))
PY
}

abi_word_to_address() {
  local value="$1"
  python3 - "$value" <<'PY'
import sys

value = sys.argv[1].strip().lower()
if not value.startswith("0x"):
    raise SystemExit(f"expected abi-encoded hex word, got: {value}")
raw = value[2:]
if len(raw) != 64:
    raise SystemExit(f"expected 32-byte abi word, got length {len(raw)}")
print("0x" + raw[-40:])
PY
}

assert_equal() {
  local lhs="$1"
  local rhs="$2"
  local message="$3"
  [[ "$lhs" == "$rhs" ]] || fail "$message ($lhs != $rhs)"
}

assert_not_zero_address() {
  local value="$1"
  local label="$2"
  if [[ "$STRICT" == "true" || "$STRICT" == "1" ]]; then
    [[ "$value" != "0x0000000000000000000000000000000000000000" ]] || fail "${label} is zero"
  fi
}

assert_nonempty() {
  local value="$1"
  local label="$2"
  [[ -n "$value" ]] || fail "${label} is empty"
}

for file in \
  "$METADATA_FILE" \
  "$CORE_FILE" \
  "$WARP_FILE" \
  "$AGENT_FILE" \
  "$ENABLE_PROPOSAL_FILE" \
  "$AUTHORIZED_PROPOSAL_FILE"
do
  [[ -f "$file" ]] || fail "missing artifact $(basename "$file")"
done

python3 -m json.tool "$AGENT_FILE" >/dev/null
python3 -m json.tool "$ENABLE_PROPOSAL_FILE" >/dev/null
python3 -m json.tool "$AUTHORIZED_PROPOSAL_FILE" >/dev/null

metadata_name="$(yaml_scalar "$METADATA_FILE" "name")"
metadata_domain="$(yaml_scalar "$METADATA_FILE" "domainId")"
metadata_chain_id="$(yaml_scalar "$METADATA_FILE" "chainId")"
metadata_rpc="$(yaml_scalar "$METADATA_FILE" "rpcUrl")"
metadata_mailbox="$(yaml_scalar "$METADATA_FILE" "mailbox")"
metadata_igp="$(yaml_scalar "$METADATA_FILE" "interchainGasPaymaster")"
metadata_validator_announce="$(yaml_scalar "$METADATA_FILE" "validatorAnnounce")"
metadata_merkle_hook="$(yaml_scalar "$METADATA_FILE" "merkleTreeHook")"

core_environment="$(yaml_scalar "$CORE_FILE" "environment")"
core_owner="$(yaml_scalar "$CORE_FILE" "owner")"
core_mailbox_address="$(yaml_nested_scalar "$CORE_FILE" "mailbox" "address")"
core_mailbox_owner="$(yaml_nested_scalar "$CORE_FILE" "mailbox" "owner")"
core_igp_address="$(yaml_nested_scalar "$CORE_FILE" "igp" "address")"

warp_local_domain="$(yaml_scalar "$WARP_FILE" "localDomain")"
warp_remote_domain="$(yaml_scalar "$WARP_FILE" "remoteDomain")"
warp_mailbox="$(yaml_scalar "$WARP_FILE" "mailbox")"
warp_local_router="$(yaml_scalar "$WARP_FILE" "localRouter")"
warp_remote_router="$(yaml_scalar "$WARP_FILE" "remoteRouter")"
warp_collateral_token="$(yaml_scalar "$WARP_FILE" "collateralToken")"
warp_recipient="$(yaml_scalar "$WARP_FILE" "recipient")"

agent_og_domain="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['domain']")"
agent_base_domain="$(json_expr "$AGENT_FILE" "data['chains']['$BASE_CHAIN_NAME']['domain']")"
agent_og_mailbox="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['mailbox']")"
agent_og_igp="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['interchainGasPaymaster']")"
agent_og_rpc_len="$(json_expr "$AGENT_FILE" "len(data['chains']['$OG_EVM_CHAIN_NAME']['customRpcUrls'])")"
agent_base_rpc_len="$(json_expr "$AGENT_FILE" "len(data['chains']['$BASE_CHAIN_NAME']['customRpcUrls'])")"
agent_og_rpc_0="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['customRpcUrls'][0]")"
agent_base_rpc_0="$(json_expr "$AGENT_FILE" "data['chains']['$BASE_CHAIN_NAME']['customRpcUrls'][0]")"
agent_og_has_metadata_rpc="$(json_expr "$AGENT_FILE" "'true' if '$metadata_rpc' in data['chains']['$OG_EVM_CHAIN_NAME']['customRpcUrls'] else 'false'")"

enable_authorized_contract="$(json_expr "$ENABLE_PROPOSAL_FILE" "data['messages'][0]['params']['authorizedContract']")"
enable_mailbox="$(json_expr "$ENABLE_PROPOSAL_FILE" "data['messages'][0]['params']['hyperlaneMailbox']")"
enable_base_domain="$(json_expr "$ENABLE_PROPOSAL_FILE" "data['messages'][0]['params']['baseDomainId']")"
authorized_contract="$(json_expr "$AUTHORIZED_PROPOSAL_FILE" "data['messages'][0]['contractAddress']")"

assert_equal "$metadata_name" "$OG_EVM_CHAIN_NAME" "metadata chain name mismatch"
assert_equal "$metadata_domain" "$warp_local_domain" "og-evm domain mismatch between metadata and warp config"
assert_equal "$metadata_domain" "$agent_og_domain" "og-evm domain mismatch between metadata and agent config"
assert_equal "$warp_remote_domain" "$agent_base_domain" "base domain mismatch between warp config and agent config"
assert_equal "$metadata_mailbox" "$core_mailbox_address" "mailbox mismatch between metadata and core config"
assert_equal "$metadata_mailbox" "$warp_mailbox" "mailbox mismatch between metadata and warp config"
assert_equal "$metadata_mailbox" "$agent_og_mailbox" "mailbox mismatch between metadata and agent config"
assert_equal "$metadata_igp" "$core_igp_address" "IGP mismatch between metadata and core config"
assert_equal "$metadata_igp" "$agent_og_igp" "IGP mismatch between metadata and agent config"
assert_equal "$warp_local_router" "$enable_authorized_contract" "local router mismatch between warp config and enable proposal"
assert_equal "$warp_local_router" "$authorized_contract" "local router mismatch between warp config and authorized-contract proposal"
assert_equal "$metadata_mailbox" "$enable_mailbox" "mailbox mismatch between metadata and enable proposal"
assert_equal "$warp_remote_domain" "$enable_base_domain" "base domain mismatch between warp config and enable proposal"

[[ "$metadata_domain" != "$warp_remote_domain" ]] || fail "og-evm and base domains must differ"
[[ "$metadata_chain_id" =~ ^[0-9]+$ ]] || fail "metadata chainId must be numeric"
[[ "$metadata_domain" =~ ^[0-9]+$ ]] || fail "metadata domainId must be numeric"
[[ "$warp_remote_domain" =~ ^[0-9]+$ ]] || fail "warp remoteDomain must be numeric"
[[ "$agent_og_rpc_len" -gt 0 ]] || fail "agent config is missing og-evm RPC URLs"
[[ "$agent_base_rpc_len" -gt 0 ]] || fail "agent config is missing base RPC URLs"
[[ "$agent_og_has_metadata_rpc" == "true" ]] || fail "metadata rpcUrl is not included in the og-evm agent RPC list"

assert_nonempty "$metadata_rpc" "metadata rpcUrl"
assert_nonempty "$agent_og_rpc_0" "agent og-evm RPC URL"
assert_nonempty "$agent_base_rpc_0" "agent base RPC URL"
assert_nonempty "$core_environment" "core environment"
assert_nonempty "$core_owner" "core owner"
assert_nonempty "$core_mailbox_owner" "core mailbox owner"
assert_nonempty "$metadata_validator_announce" "validatorAnnounce"
assert_nonempty "$metadata_merkle_hook" "merkleTreeHook"

assert_not_zero_address "$metadata_mailbox" "metadata mailbox"
assert_not_zero_address "$metadata_igp" "metadata IGP"
assert_not_zero_address "$core_owner" "core owner"
assert_not_zero_address "$core_mailbox_owner" "core mailbox owner"
assert_not_zero_address "$metadata_validator_announce" "validatorAnnounce"
assert_not_zero_address "$metadata_merkle_hook" "merkleTreeHook"
assert_not_zero_address "$warp_local_router" "warp localRouter"
assert_not_zero_address "$warp_remote_router" "warp remoteRouter"
assert_not_zero_address "$warp_collateral_token" "warp collateralToken"
assert_not_zero_address "$warp_recipient" "warp recipient"

if is_truthy "$ACTIVE_RPC_VALIDATION"; then
  og_chain_id_hex="$(rpc_call "$metadata_rpc" "eth_chainId" "[]")" || fail "unable to query og-evm eth_chainId from $metadata_rpc"
  og_chain_id="$(hex_to_dec "$og_chain_id_hex")" || fail "unable to decode og-evm eth_chainId response"
  assert_equal "$metadata_chain_id" "$og_chain_id" "og-evm chainId mismatch between metadata and live RPC"

  rpc_call "$agent_base_rpc_0" "eth_chainId" "[]" >/dev/null || fail "unable to query base eth_chainId from $agent_base_rpc_0"

  [[ "$metadata_mailbox" != "0x0000000000000000000000000000000000000000" ]] || fail "cannot live-validate mailbox ownership with zero mailbox address"
  mailbox_owner_word="$(rpc_call "$metadata_rpc" "eth_call" "[{\"to\":\"$metadata_mailbox\",\"data\":\"0x8da5cb5b\"},\"latest\"]")" || fail "unable to call mailbox owner() on $metadata_mailbox"
  mailbox_owner="$(abi_word_to_address "$mailbox_owner_word")" || fail "unable to decode mailbox owner() response"
  assert_equal "$(lower "$core_mailbox_owner")" "$(lower "$mailbox_owner")" "mailbox owner mismatch between core config and live RPC"
fi

echo "bridge artifact validation passed for $ARTIFACT_DIR"
