#!/usr/bin/env python3

import http.server
import json
import os
import socketserver
import sys
import time
import urllib.error
import urllib.request

ZERO_ADDRESS = "0x0000000000000000000000000000000000000000"


def yaml_scalar(path: str, key: str) -> str:
    prefix = f"{key}: "
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            if line.startswith(prefix):
                return line[len(prefix):].strip()
    raise ValueError(f"missing {key} in {path}")


def load_agent_config(path: str) -> dict:
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def env_or_default(name: str, default: str | None = None) -> str | None:
    value = os.getenv(name)
    if value is None or value == "":
        return default
    return value


def normalize_address(value: str, label: str) -> str:
    if not value:
        raise ValueError(f"{label} is empty")
    if not value.startswith("0x") or len(value) != 42:
        raise ValueError(f"{label} must be a 20-byte hex address")
    return value


def request_json(url: str, *, payload: dict | None = None, timeout: float = 10.0) -> dict:
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {}
    method = "GET"
    if payload is not None:
        headers["Content-Type"] = "application/json"
        method = "POST"
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def rpc_call(url: str, method: str, params: list, timeout: float) -> str:
    body = request_json(
        url,
        payload={"jsonrpc": "2.0", "id": 1, "method": method, "params": params},
        timeout=timeout,
    )
    if "error" in body:
        raise RuntimeError(f"rpc error from {url}: {body['error']}")
    if "result" not in body:
        raise RuntimeError(f"rpc result missing from {url}")
    return body["result"]


def encode_balance_of(address: str) -> str:
    normalized = normalize_address(address.lower(), "balanceOf target")[2:]
    return "0x70a08231" + normalized.rjust(64, "0")


def read_collateral_balance(rpc_url: str, token_address: str, router_address: str, timeout: float) -> int:
    result = rpc_call(
        rpc_url,
        "eth_call",
        [{"to": normalize_address(token_address, "BASE_TOKEN_ADDRESS"), "data": encode_balance_of(router_address)}, "latest"],
        timeout,
    )
    if not isinstance(result, str) or not result.startswith("0x"):
        raise RuntimeError(f"unexpected eth_call response: {result!r}")
    return int(result, 16)


def scrape_bridge_status(rest_url: str, timeout: float) -> dict:
    payload = request_json(
        rest_url.rstrip("/") + "/cosmos/bridge/v1/bridge_status",
        timeout=timeout,
    )
    required = ("enabled", "totalMinted", "totalBurned", "authorizedContract")
    missing = [key for key in required if key not in payload]
    if missing:
        raise RuntimeError(f"bridge status response missing fields: {', '.join(missing)}")
    return payload


def load_runtime_config() -> dict:
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
    artifact_dir = env_or_default("ARTIFACT_DIR", os.path.join(repo_root, "scripts", "bridge", "generated"))
    if artifact_dir is None:
        raise RuntimeError("ARTIFACT_DIR could not be resolved")

    warp_file = os.path.join(artifact_dir, "warp-config.yaml")
    agent_file = os.path.join(artifact_dir, "agent-config.json")
    base_chain_name = env_or_default("BASE_CHAIN_NAME", "base")
    if base_chain_name is None:
        raise RuntimeError("BASE_CHAIN_NAME could not be resolved")

    agent_config = load_agent_config(agent_file)
    chains = agent_config.get("chains", {})
    if base_chain_name not in chains:
        raise RuntimeError(f"base chain {base_chain_name!r} missing from agent-config.json")

    base_chain = chains[base_chain_name]
    base_rpc_urls = base_chain.get("customRpcUrls", [])
    base_rpc_url = env_or_default("BASE_RPC_URL", base_rpc_urls[0] if base_rpc_urls else None)
    rest_url = env_or_default("OG_EVM_REST_URL")
    token_address = env_or_default("BASE_TOKEN_ADDRESS", yaml_scalar(warp_file, "collateralToken"))
    router_address = env_or_default("BASE_COLLATERAL_ROUTER", yaml_scalar(warp_file, "remoteRouter"))

    if not rest_url:
        raise RuntimeError("OG_EVM_REST_URL is required")
    if not base_rpc_url:
        raise RuntimeError("BASE_RPC_URL is required or must be present in agent-config.json")

    normalize_address(token_address, "BASE_TOKEN_ADDRESS")
    normalize_address(router_address, "BASE_COLLATERAL_ROUTER")
    if token_address == ZERO_ADDRESS:
        raise RuntimeError("BASE_TOKEN_ADDRESS is still the zero address")
    if router_address == ZERO_ADDRESS:
        raise RuntimeError("BASE_COLLATERAL_ROUTER is still the zero address")

    return {
        "artifact_dir": artifact_dir,
        "base_chain_name": base_chain_name,
        "og_evm_rest_url": rest_url,
        "base_rpc_url": base_rpc_url,
        "base_token_address": token_address,
        "base_collateral_router": router_address,
        "timeout": float(env_or_default("BRIDGE_METRICS_TIMEOUT_SECONDS", "10") or "10"),
    }


def escape_label_value(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", " ")


def metric_line(name: str, value: int | float, labels: dict[str, str] | None = None) -> str:
    if not labels:
        return f"{name} {value}"
    rendered = ",".join(
        f'{key}="{escape_label_value(labels[key])}"'
        for key in sorted(labels)
    )
    return f"{name}{{{rendered}}} {value}"


def render_metrics() -> str:
    config = load_runtime_config()
    bridge_status = scrape_bridge_status(config["og_evm_rest_url"], config["timeout"])
    collateral_balance = read_collateral_balance(
        config["base_rpc_url"],
        config["base_token_address"],
        config["base_collateral_router"],
        config["timeout"],
    )

    total_minted = int(bridge_status["totalMinted"])
    total_burned = int(bridge_status["totalBurned"])
    outstanding = total_minted - total_burned
    enabled = 1 if bridge_status["enabled"] else 0
    authorized_contract = bridge_status["authorizedContract"]
    collateral_surplus = collateral_balance - outstanding

    lines = [
        "# HELP bridge_scrape_success Whether the bridge metrics exporter completed the latest scrape.",
        "# TYPE bridge_scrape_success gauge",
        metric_line("bridge_scrape_success", 1),
        "# HELP bridge_scrape_timestamp_seconds Unix timestamp of the latest successful bridge scrape.",
        "# TYPE bridge_scrape_timestamp_seconds gauge",
        metric_line("bridge_scrape_timestamp_seconds", int(time.time())),
        "# HELP og_evm_bridge_enabled Whether the og-evm bridge module is enabled.",
        "# TYPE og_evm_bridge_enabled gauge",
        metric_line("og_evm_bridge_enabled", enabled),
        "# HELP og_evm_bridge_total_minted Cumulative native tokens minted through the bridge.",
        "# TYPE og_evm_bridge_total_minted gauge",
        metric_line("og_evm_bridge_total_minted", total_minted),
        "# HELP og_evm_bridge_total_burned Cumulative native tokens burned through the bridge.",
        "# TYPE og_evm_bridge_total_burned gauge",
        metric_line("og_evm_bridge_total_burned", total_burned),
        "# HELP og_evm_bridge_outstanding Outstanding bridged native supply on og-evm.",
        "# TYPE og_evm_bridge_outstanding gauge",
        metric_line("og_evm_bridge_outstanding", outstanding),
        "# HELP base_collateral_balance Locked collateral token balance on Base.",
        "# TYPE base_collateral_balance gauge",
        metric_line("base_collateral_balance", collateral_balance),
        "# HELP bridge_collateral_surplus Base collateral balance minus outstanding bridged supply.",
        "# TYPE bridge_collateral_surplus gauge",
        metric_line("bridge_collateral_surplus", collateral_surplus),
        "# HELP og_evm_bridge_authorized_contract_info Current authorized og-evm bridge contract.",
        "# TYPE og_evm_bridge_authorized_contract_info gauge",
        metric_line(
            "og_evm_bridge_authorized_contract_info",
            1,
            {"authorized_contract": authorized_contract},
        ),
        "# HELP base_collateral_router_info Current Base collateral router and token wiring.",
        "# TYPE base_collateral_router_info gauge",
        metric_line(
            "base_collateral_router_info",
            1,
            {
                "router": config["base_collateral_router"],
                "token": config["base_token_address"],
                "base_chain": config["base_chain_name"],
            },
        ),
    ]
    return "\n".join(lines) + "\n"


def error_metrics(message: str) -> str:
    sanitized = escape_label_value(message)
    lines = [
        "# HELP bridge_scrape_success Whether the bridge metrics exporter completed the latest scrape.",
        "# TYPE bridge_scrape_success gauge",
        metric_line("bridge_scrape_success", 0),
        "# HELP bridge_scrape_error_info Last bridge exporter scrape error.",
        "# TYPE bridge_scrape_error_info gauge",
        metric_line("bridge_scrape_error_info", 1, {"message": sanitized}),
    ]
    return "\n".join(lines) + "\n"


class BridgeMetricsHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(b"ok\n")
            return

        if self.path != "/metrics":
            self.send_response(404)
            self.end_headers()
            return

        try:
            payload = render_metrics().encode("utf-8")
            self.send_response(200)
        except Exception as exc:  # noqa: BLE001
            payload = error_metrics(str(exc)).encode("utf-8")
            self.send_response(500)

        self.send_header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        return


class ThreadingTcpServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True


def main() -> int:
    port = int(env_or_default("BRIDGE_METRICS_PORT", "9300") or "9300")
    host = env_or_default("BRIDGE_METRICS_HOST", "0.0.0.0") or "0.0.0.0"
    with ThreadingTcpServer((host, port), BridgeMetricsHandler) as server:
        print(f"bridge metrics exporter listening on {host}:{port}", file=sys.stderr)
        server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
