#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARTIFACT_DIR="${1:-${OUTPUT_DIR:-$ROOT_DIR/scripts/bridge/generated}}"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT_DIR/scripts/bridge/infra/docker-compose.yml}"
OG_EVM_CHAIN_NAME="${OG_EVM_CHAIN_NAME:-og-evm}"
BASE_CHAIN_NAME="${BASE_CHAIN_NAME:-base}"
OG_EVM_LCD_URL="${OG_EVM_LCD_URL:-}"
BASE_TOKEN_ADDRESS="${BASE_TOKEN_ADDRESS:-${BASE_COLLATERAL_TOKEN:-}}"
VALIDATOR_METRICS_URL="${VALIDATOR_METRICS_URL:-http://127.0.0.1:${HYPERLANE_VALIDATOR_METRICS_PORT:-9090}/metrics}"
RELAYER_METRICS_URL="${RELAYER_METRICS_URL:-http://127.0.0.1:${HYPERLANE_RELAYER_METRICS_PORT:-9091}/metrics}"
CHECK_DOCKER="${CHECK_DOCKER:-false}"

AGENT_FILE="$ARTIFACT_DIR/agent-config.json"
WARP_FILE="$ARTIFACT_DIR/warp-config.yaml"

fail() {
  echo "bridge monitor failed: $*" >&2
  exit 1
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

is_truthy() {
  case "$(lower "$1")" in
    1|true|yes|on)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
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

http_get() {
  local url="$1"
  python3 - "$url" <<'PY'
import sys
import urllib.error
import urllib.request

url = sys.argv[1]
try:
    with urllib.request.urlopen(url, timeout=10) as response:
        print(response.read().decode("utf-8"))
except urllib.error.URLError as exc:
    print(f"http request failed: {exc}", file=sys.stderr)
    sys.exit(1)
PY
}

rpc_call() {
  local url="$1"
  local method="$2"
  local params_json="${3:-[]}"

  python3 - "$url" "$method" "$params_json" <<'PY'
import json
import sys
import urllib.error
import urllib.request

url = sys.argv[1]
method = sys.argv[2]
params = json.loads(sys.argv[3])
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
    with urllib.request.urlopen(request, timeout=10) as response:
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
print(body["result"])
PY
}

hex_to_dec() {
  local value="$1"
  python3 - "$value" <<'PY'
import sys

value = sys.argv[1].strip()
print(int(value, 16))
PY
}

prom_metrics_ok() {
  local url="$1"
  local body

  body="$(http_get "$url")" || fail "unable to reach metrics endpoint $url"
  [[ "$body" == \#\ HELP* || "$body" == *$'\n# HELP '* || "$body" == *$'\n# TYPE '* ]] || fail "metrics endpoint at $url did not return Prometheus text"
}

docker_service_state() {
  local service="$1"
  docker compose -f "$COMPOSE_FILE" ps --format json \
    | python3 - "$service" <<'PY'
import json
import sys

service = sys.argv[1]
text = sys.stdin.read().strip()
if not text:
    print("")
    sys.exit(0)
if text.startswith("["):
    data = json.loads(text)
else:
    data = [json.loads(line) for line in text.splitlines() if line.strip()]

for item in data:
    if item.get("Service") == service:
        print(item.get("State", ""))
        break
PY
}

[[ -f "$AGENT_FILE" ]] || fail "missing agent config at $AGENT_FILE"
[[ -f "$WARP_FILE" ]] || fail "missing warp config at $WARP_FILE"

og_rpc="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['customRpcUrls'][0]")"
base_rpc="$(json_expr "$AGENT_FILE" "data['chains']['$BASE_CHAIN_NAME']['customRpcUrls'][0]")"
og_domain="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['domain']")"
base_domain="$(json_expr "$AGENT_FILE" "data['chains']['$BASE_CHAIN_NAME']['domain']")"
og_mailbox="$(json_expr "$AGENT_FILE" "data['chains']['$OG_EVM_CHAIN_NAME']['mailbox']")"
og_router="$(yaml_scalar "$WARP_FILE" "localRouter")"
base_router="$(yaml_scalar "$WARP_FILE" "remoteRouter")"
base_token="${BASE_TOKEN_ADDRESS:-$(yaml_scalar "$WARP_FILE" "collateralToken")}"

echo "Bridge monitor summary"
echo "  og-evm domain: $og_domain"
echo "  base domain: $base_domain"
echo "  og-evm mailbox: $og_mailbox"
echo "  og-evm router: $og_router"
echo "  base router: $base_router"
echo "  og-evm rpc: $og_rpc"
echo "  base rpc: $base_rpc"
echo "  validator metrics: $VALIDATOR_METRICS_URL"
echo "  relayer metrics: $RELAYER_METRICS_URL"

og_chain_id_hex="$(rpc_call "$og_rpc" "eth_chainId" "[]")" || fail "unable to query og-evm RPC chainId"
base_chain_id_hex="$(rpc_call "$base_rpc" "eth_chainId" "[]")" || fail "unable to query base RPC chainId"
echo "RPC chain IDs"
echo "  og-evm: $(hex_to_dec "$og_chain_id_hex")"
echo "  base: $(hex_to_dec "$base_chain_id_hex")"

if [[ -n "$OG_EVM_LCD_URL" ]]; then
  bridge_status="$(http_get "${OG_EVM_LCD_URL%/}/cosmos/bridge/v1/bridge_status")" || fail "unable to query og-evm bridge status from $OG_EVM_LCD_URL"
  echo "Bridge module status"
  python3 - "$bridge_status" <<'PY'
import json
import sys

data = json.loads(sys.argv[1])
print(f"  enabled: {data.get('enabled')}")
print(f"  total_minted: {data.get('totalMinted')}")
print(f"  total_burned: {data.get('totalBurned')}")
print(f"  authorized_contract: {data.get('authorizedContract')}")
PY
fi

if [[ -n "$base_token" ]]; then
  router_word="000000000000000000000000$(lower "${base_router#0x}")"
  balance_word="$(rpc_call "$base_rpc" "eth_call" "[{\"to\":\"$base_token\",\"data\":\"0x70a08231${router_word}\"},\"latest\"]")" || fail "unable to query collateral balance from $base_token"
  echo "Base collateral balance"
  echo "  token: $base_token"
  echo "  router: $base_router"
  echo "  balance_wei: $(hex_to_dec "$balance_word")"
fi

prom_metrics_ok "$VALIDATOR_METRICS_URL"
prom_metrics_ok "$RELAYER_METRICS_URL"
echo "Metrics endpoints"
echo "  validator: ok"
echo "  relayer: ok"

if is_truthy "$CHECK_DOCKER"; then
  command -v docker >/dev/null 2>&1 || fail "docker is required when CHECK_DOCKER=true"
  [[ -f "$COMPOSE_FILE" ]] || fail "missing compose file at $COMPOSE_FILE"

  validator_state="$(docker_service_state validator)"
  relayer_state="$(docker_service_state relayer)"
  [[ "$validator_state" == "running" ]] || fail "validator service is not running (state: ${validator_state:-unknown})"
  [[ "$relayer_state" == "running" ]] || fail "relayer service is not running (state: ${relayer_state:-unknown})"

  echo "Docker services"
  echo "  validator: $validator_state"
  echo "  relayer: $relayer_state"
fi
