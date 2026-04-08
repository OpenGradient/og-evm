# og-evm Observability

Production-grade monitoring and tracing for og-evm validator and full nodes using Prometheus, Grafana, Tempo, and OpenTelemetry.

## Architecture

```
og-evm node(s)
  ├── :26660/metrics  ──→ Prometheus (scrape)  ──→ Grafana dashboards + alerts
  ├── :8100/metrics   ──→ Prometheus (scrape)  ──→ Grafana dashboards + alerts
  ├── :6065/debug/metrics ──→ Prometheus (scrape)
  └── OTel SDK        ──→ :4317 OTel Collector ──┬→ Tempo (traces)
                                                  └→ Prometheus (metrics via remote write)
node-exporter (:9100) ──→ Prometheus (scrape)  ──→ Infrastructure metrics
```

Two pillars of observability:
- **Metrics** — Prometheus scrapes CometBFT, Geth, JSON-RPC, and node-exporter metrics. OTel custom metrics and Cosmos SDK telemetry flow through the OTel Collector
- **Traces** — OpenTelemetry SDK exports spans (EthereumTx, BeginBlock, etc.) via OTLP gRPC to Tempo

## Prerequisites

- Docker and Docker Compose
- A running `evmd` node (built with OTel support)

## Deployment

### 1. Start the monitoring stack

```bash
cd monitoring
docker compose up -d
```

### 2. Configure your node

Enable the following in your node's `config.toml`:

```toml
[instrumentation]
prometheus = true
prometheus_listen_addr = ":26660"
```

Enable OpenTelemetry in `app.toml`:

```toml
[otel]
enable = true
endpoint = "<collector-host>:4317"
insecure = true
```

Or pass as CLI flags when starting the node:

```bash
evmd start \
  --otel.enable \
  --otel.endpoint <collector-host>:4317 \
  --otel.insecure \
  --otel.chain-id <your-chain-id> \
  --otel.instance-id <validator-0> \
  --metrics
```

### 3. Register your node as a Prometheus target

Add your node to `prometheus/targets/og-evm-nodes.json`:

```json
[
  {
    "targets": ["<node-host>:26660"],
    "labels": {
      "chain_id": "og-evm-mainnet",
      "instance": "validator-0"
    }
  }
]
```

Prometheus reloads target files every 30s — no restart needed. Add additional nodes as separate entries.

### 4. Access Grafana

Open `http://<grafana-host>:3000` (default credentials: `admin` / `admin`).

Change the admin password on first login for production deployments.

## Services

| Service | Port | Purpose |
|---------|------|---------|
| Prometheus | 9099 | Metrics storage, alerting, and PromQL queries |
| Grafana | 3000 | Dashboards, trace exploration, and alert management |
| OTel Collector | 4317 (gRPC) / 4318 (HTTP) | Receives OTLP traces and metrics from nodes |
| Tempo | 3200 | Distributed trace storage with span metrics generation |
| node-exporter | 9100 | Host infrastructure metrics (CPU, memory, disk, network) |

## Metrics Collected

### CometBFT (port 26660)

Consensus, P2P, mempool, and block production metrics. Key metrics:

| Metric | Description |
|--------|-------------|
| `cometbft_consensus_height` | Current block height |
| `cometbft_consensus_validators` | Active / missing / byzantine validator counts |
| `cometbft_consensus_block_interval_seconds` | Time between blocks (histogram) |
| `cometbft_consensus_rounds` | Consensus rounds per height |
| `cometbft_p2p_peers` | Connected peer count |
| `cometbft_consensus_total_txs` | Cumulative transaction count |

### Geth / EVM (port 8100)

Go-Ethereum internals — state reads, cache hits, and EVM execution metrics.

### OTel span-derived metrics (via Tempo metrics generator)

Tempo automatically generates RED (Rate, Error, Duration) metrics from ingested traces:

| Metric | Description |
|--------|-------------|
| `traces_spanmetrics_calls_total` | Request rate per span name |
| `traces_spanmetrics_latency_bucket` | Latency histogram per span name |
| `traces_service_graph_*` | Service-to-service dependency metrics |

## OTel Configuration Reference

| Flag | `app.toml` key | Default | Description |
|------|----------------|---------|-------------|
| `--otel.enable` | `otel.enable` | `false` | Enable trace and metric export |
| `--otel.endpoint` | `otel.endpoint` | `localhost:4317` | OTLP gRPC collector endpoint |
| `--otel.insecure` | `otel.insecure` | `true` | Use non-TLS gRPC connection |
| `--otel.sample-rate` | `otel.sample-rate` | `0.1` | Trace sampling rate (0.0 = none, 1.0 = all) |
| `--otel.chain-id` | `otel.chain-id` | `""` | Chain ID attached as resource attribute on all telemetry |
| `--otel.instance-id` | `otel.instance-id` | `""` (hostname) | Node instance ID attached as resource attribute |

### Sampling

For high-throughput chains, reduce `sample-rate` to control trace volume. A rate of `0.1` samples 10% of traces. Metrics are always exported regardless of sampling rate.

## Grafana Dashboards

| Dashboard | Location | Description |
|-----------|----------|-------------|
| OG-EVM Chain Overview | og-evm folder | Block production, consensus, mempool, validators, P2P, ABCI timing |
| OG-EVM EVM & Application Metrics | og-evm folder | EVM transactions, gas, base fee, ERC20 conversions, IBC transfers |
| OG-EVM Geth Internals | og-evm folder | TxPool, RPC, P2P, state DB, chain head |
| OG-EVM Infrastructure | og-evm folder | CPU, memory, disk, network (node-exporter) |

Dashboard variables:
- **Chain ID** — filters CometBFT panels to a specific chain
- **Instance** — filters to a specific validator/node

Trace waterfalls are available under Explore → Tempo.

## Alerting

12 alert rules are provisioned automatically via `grafana/provisioning/alerting/`:

| Alert | Condition | Severity |
|-------|-----------|----------|
| Chain Halted | No blocks in 5 min | Critical |
| No Peers | Peer count = 0 | Critical |
| Byzantine Validator | Byzantine count > 0 | Critical |
| Low Peer Count | Peers < 3 | Warning |
| High Consensus Rounds | Rounds > 2 | Warning |
| Slow Block Time | Avg > 10s | Warning |
| Missing Validators | Missing > 0 | Warning |
| Mempool Congestion | Size > 500 | Warning |
| IBC Errors Spike | Error rate > 0.1/s | Warning |
| Disk Usage High | > 85% | Warning |
| CPU Usage High | > 90% | Warning |
| Memory Usage High | > 90% | Warning |

Configure notification channels (Slack, PagerDuty, email) under Alerting → Contact points in the Grafana UI. Without a contact point, alerts are visible in the Grafana UI only.

## Retention

| Store | Default | Configuration |
|-------|---------|---------------|
| Prometheus (metrics) | 30 days | `--storage.tsdb.retention.time` in `docker-compose.yml` |
| Tempo (traces) | 14 days | `compactor.compaction.block_retention` in `tempo/config.yml` |

## Production Considerations

- **Change Grafana admin password** on first login
- **Use TLS** for the OTel endpoint (`otel.insecure = false`) when the collector is on a different host
- **Tune sampling rate** — `1.0` exports every trace; lower this on high-throughput chains
- **Persistent volumes** — the `docker-compose.yml` uses named Docker volumes; back these up or mount to host paths for durability
- **Resource limits** — consider setting memory/CPU limits on Tempo and Prometheus for production workloads
- **Network security** — restrict access to Grafana (3000), Prometheus (9099), and Tempo (3200) ports
- **Alert notifications** — configure Slack, PagerDuty, or email contact points under Alerting → Contact points in Grafana. Without a contact point, alerts fire in the Grafana UI only

## Remote Deployment (Separate Monitoring Server)

By default, the monitoring stack runs co-located with the validator node (`insecure=true`). For production mainnet with a dedicated monitoring server, enable TLS on the OTel endpoint.

### 1. Generate TLS certificates

```bash
# Self-signed (for testing)
openssl req -x509 -newkey rsa:4096 \
  -keyout server.key -out server.crt \
  -days 365 -nodes -subj "/CN=og-evm-otel-collector"

# For production, use CA-signed certificates
```

### 2. Configure the OTel Collector

Create `monitoring/otel-collector/config-tls.yml` with TLS receivers:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
        tls:
          cert_file: /certs/server.crt
          key_file: /certs/server.key
      http:
        endpoint: 0.0.0.0:4318
        tls:
          cert_file: /certs/server.crt
          key_file: /certs/server.key
# ... rest of config same as config.yml
```

Mount certs in docker-compose override:

```yaml
  otel-collector:
    volumes:
      - ./otel-collector/config-tls.yml:/etc/otelcol-contrib/config.yaml:ro
      - ./certs:/certs:ro
```

### 3. Configure validator nodes

In each validator's `app.toml`:

```toml
[otel]
enable = true
endpoint = "<monitoring-host>:4317"
insecure = false
sample-rate = 0.1
chain-id = "og-evm-mainnet"
instance-id = "validator-0"
```

---

## Local Development / Testnet

For local testing, convenience scripts are provided:

```bash
# Single-node local devnet with OTel
OTEL_ENABLE=true ./local_node.sh -y --no-install

# 4-node Docker testnet (prometheus + OTel enabled automatically by wrapper.sh)
make localnet-start
```

Local testnet port mapping:

| Node | CometBFT metrics | Geth metrics | JSON-RPC | P2P |
|------|------------------|-------------|----------|-----|
| node0 | 26660 | 8100 | 8545 | 26656 |
| node1 | 26661 | 8101 | 8555 | 26666 |
| node2 | 26662 | 8102 | 8565 | 26676 |
| node3 | 26663 | 8103 | 8575 | 26686 |
