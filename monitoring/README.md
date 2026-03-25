# og-evm Observability

Production-grade monitoring and tracing for og-evm validator and full nodes using Prometheus, Grafana, Tempo, and Loki.

## Architecture

```
og-evm node(s)
  ├── :26660/metrics ──→ Prometheus (scrape)  ──→ Grafana dashboards + alerts
  ├── :8100/metrics  ──→ Prometheus (scrape)  ──→ Grafana dashboards + alerts
  └── OTel SDK       ──→ :4317 OTel Collector ──┬→ Tempo (traces)
                                                 └→ Prometheus (span-derived metrics via remote write)
```

Three pillars of observability:
- **Metrics** — Prometheus scrapes CometBFT consensus and geth EVM metrics every 15s
- **Traces** — OpenTelemetry SDK exports spans (EthereumTx, BeginBlock, etc.) via OTLP gRPC to Tempo
- **Logs** — Promtail ships node logs to Loki

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
| Loki | 3100 | Log aggregation and querying |
| Promtail | — | Ships node logs to Loki |

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
| `--otel.sample-rate` | `otel.sample-rate` | `1.0` | Trace sampling rate (0.0 = none, 1.0 = all) |
| `--otel.chain-id` | `otel.chain-id` | `""` | Chain ID attached as resource attribute on all telemetry |

### Sampling

For high-throughput chains, reduce `sample-rate` to control trace volume. A rate of `0.1` samples 10% of traces. Metrics are always exported regardless of sampling rate.

## Grafana Views

| View | Location | Datasource |
|------|----------|------------|
| Chain overview | Dashboards → Cosmos Dashboard | Prometheus |
| Trace waterfalls | Explore → Tempo | Tempo |
| Ad-hoc metric queries | Explore → Prometheus | Prometheus |
| Log search | Explore → Loki | Loki |

Dashboard variables:
- **Chain ID** — filters all panels to a specific chain
- **Instance** — filters to a specific validator/node

## Alerting

Create alert rules in Grafana under Alerting → Alert rules using Prometheus as the datasource.

Recommended alerts:

| Alert | PromQL | Severity |
|-------|--------|----------|
| Chain halted | `increase(cometbft_consensus_height[5m]) == 0` | Critical |
| Peer count low | `cometbft_p2p_peers < 3` | Warning |
| Missed blocks | `cometbft_consensus_missing_validators > 0` | Warning |
| High consensus rounds | `cometbft_consensus_rounds > 2` | Warning |
| Slow block time | `rate(cometbft_consensus_block_interval_seconds_sum[5m]) / rate(cometbft_consensus_block_interval_seconds_count[5m]) > 10` | Warning |

Configure notification channels (Slack, PagerDuty, email) under Alerting → Contact points.

## Retention

| Store | Default | Configuration |
|-------|---------|---------------|
| Prometheus (metrics) | 30 days | `--storage.tsdb.retention.time` in `docker-compose.yml` |
| Tempo (traces) | 14 days | `compactor.compaction.block_retention` in `tempo/config.yml` |
| Loki (logs) | Per Loki defaults | `limits_config.retention_period` in `loki/config.yml` |

## Production Considerations

- **Change Grafana admin password** on first login
- **Use TLS** for the OTel endpoint (`otel.insecure = false`) when the collector is on a different host
- **Tune sampling rate** — `1.0` exports every trace; lower this on high-throughput chains
- **Persistent volumes** — the `docker-compose.yml` uses named Docker volumes; back these up or mount to host paths for durability
- **Resource limits** — consider setting memory/CPU limits on Tempo and Prometheus for production workloads
- **Network security** — restrict access to Grafana (3000), Prometheus (9099), and Tempo (3200) ports

---

## Local Development / Testnet

For local testing, convenience scripts are provided:

```bash
# Single-node local devnet with OTel
OTEL_ENABLE=true ./local_node.sh -y --no-install

# 4-node Docker testnet
make localnet-start
# Then enable metrics:
for i in 0 1 2 3; do
  sed -i.bak 's/prometheus = false/prometheus = true/' .testnets/node${i}/evmd/config/config.toml
done
docker compose restart
```

Local testnet port mapping:

| Node | CometBFT metrics | Geth metrics | JSON-RPC | P2P |
|------|------------------|-------------|----------|-----|
| node0 | 26660 | 8100 | 8545 | 26656 |
| node1 | 26661 | 8101 | 8555 | 26666 |
| node2 | 26662 | 8102 | 8565 | 26676 |
| node3 | 26663 | 8103 | 8575 | 26686 |
