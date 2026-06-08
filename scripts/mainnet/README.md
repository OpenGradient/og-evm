# OG-EVM Mainnet Genesis Bootstrap

Steps to bootstrap chain id `opengradient_1486-1` from scratch.

Some steps run on every genesis machine. Others run once for the whole network.
Sections are marked **[per node]** or **[coordinator]** so the split is clear.
If one team owns all 4 genesis machines that team plays both roles. If external
validators run some nodes, the foundation typically plays the coordinator role.

Inputs:
- a release tag of this repo
- 4 machines (private subnet) for the genesis nodes
- a chosen `genesis_time` (RFC3339 UTC)

Outputs:
- a final `genesis.json` with all 4 gentxs collected
- 4 nodes producing block 1 at `genesis_time`

L1 supply at genesis is intentionally minimal: 4 × 10 OPG validator self-stakes
(40 OPG total), Foundation 0, `x/svip` module 0. The canonical 1B OPG lives as
ERC-20 on Base and bridges to L1 on demand. Foundation must bridge in OPG before
submitting any L1 transaction (including governance proposals); the SVIP pool is
funded the same way before activation. See [section 17](#17-post-launch-bridge-dependencies).

For chain settings (denoms, decimals, commission rules, supply, etc.) see
[section 16](#16-chain-settings-reference).

## What's in this guide

1. [Prereqs](#1-prereqs) [per node]
2. [Install evmd](#2-install-evmd) [per node]
3. [Set up the node home](#3-set-up-the-node-home) [per node]
4. [Generate keys](#4-generate-keys) [per node]
5. [Gather node identifiers](#5-gather-node-identifiers) [coordinator]
6. [Build the pre-gentx genesis](#6-build-the-pre-gentx-genesis) [coordinator]
7. [Place the pre-gentx genesis](#7-place-the-pre-gentx-genesis) [per node]
8. [Run gentx](#8-run-gentx) [per node]
9. [Collect gentxs and produce the final genesis](#9-collect-gentxs-and-produce-the-final-genesis) [coordinator]
10. [Place the final genesis](#10-place-the-final-genesis) [per node]
11. [Edit config files](#11-edit-config-files) [per node]
12. [Launch](#12-launch) [per node]
13. [Check it's working](#13-check-its-working) [per node]
14. [Security checklist](#14-security-checklist)
15. [Troubleshooting](#15-troubleshooting)
16. [Chain settings reference](#16-chain-settings-reference)
17. [Post-launch bridge dependencies](#17-post-launch-bridge-dependencies)


## 1. Prereqs

[per node]

### Hardware

| Resource | Recommended |
|---|---|
| CPU | 16 vCPU (modern x86_64 with AVX2) |
| RAM | 64 GB |
| Disk | 2 TB NVMe SSD |
| Network | 1 Gbps both ways, low jitter |
| Filesystem | ext4 or zfs (not btrfs) |

### OS

Tested on Ubuntu 22.04 or 24.04. Run as a regular user (call it `evmd`) with
`sudo` for setup only.

### Software

```bash
sudo apt update
sudo apt install -y build-essential git curl jq make pkg-config libssl-dev clang

# Go (use 1.26.2 or the latest patch release at install time)
curl -L https://go.dev/dl/go1.26.2.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc
go version    # should print 1.26.x
```

### Time sync

Clock has to be accurate. If it drifts more than a few seconds the node falls
out of consensus.

```bash
sudo apt install -y chrony
sudo systemctl enable --now chrony
chronyc tracking         # offset should be under 100 ms
```

### Firewall

| Port | What it is | Open to |
|---|---|---|
| 22    | SSH                | bastion only |
| 26656 | P2P                | private subnet (other genesis nodes) only |
| 26657 | Cosmos RPC         | localhost only |
| 8545  | Ethereum JSON-RPC  | localhost only |
| 8546  | Ethereum WebSocket | localhost only |
| 9090  | gRPC               | localhost only |
| 1317  | REST               | localhost only |
| 26660 | Prometheus         | observability subnet only |


## 2. Install evmd

[per node]

```bash
git clone https://github.com/OpenGradient/og-evm.git ~/og-evm
cd ~/og-evm
git checkout <release-tag>
make install                   # builds ~/go/bin/evmd
evmd version                   # prints the commit hash that was just built
```

Every node must build from the **same commit**. A different commit will produce
a different `app_hash` and fork off at block 1.


## 3. Set up the node home

[per node]

The node home is `$HOME/.evmd-mainnet`. Use this exact path so the rest of the
guide works without changes.

```bash
evmd init <moniker> \
  --chain-id opengradient_1486-1 \
  --home ~/.evmd-mainnet
```

Pick a short, recognisable moniker (`og-validator-tokyo-1` or similar). It ends
up in monitoring and alerting, so don't change it later.

Lock down the directory permissions:

```bash
chmod 700 ~/.evmd-mainnet
chmod 700 ~/.evmd-mainnet/config
chmod 600 ~/.evmd-mainnet/config/*.json
```


## 4. Generate keys

[per node]

| Key | What it's for | Where it lives | Recoverable? |
|---|---|---|---|
| Operator key (eth_secp256k1) | Signs withdrawals, governance votes, edit-validator | keyring | yes, from the mnemonic |
| Consensus key (ed25519)      | Signs every block. Most sensitive. | `priv_validator_key.json` | no |
| Node key (ed25519)           | Peer ID on the network | `node_key.json` | no, but safe to rotate |

### 4.1 Operator key

```bash
evmd keys add <key-name> \
  --algo eth_secp256k1 \
  --keyring-backend file \
  --home ~/.evmd-mainnet
```

Pick a strong passphrase. Save the **24-word mnemonic** in a hardware-backed
password manager. Losing it means losing any rewards or governance power tied
to this address.

To print the operator address:

```bash
evmd keys show <key-name> -a --keyring-backend file --home ~/.evmd-mainnet
# prints og1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 4.2 Consensus key

`evmd init` already created `~/.evmd-mainnet/config/priv_validator_key.json`.
Treat this file like cash:

- Don't copy it off the host.
- Back it up once, encrypted, somewhere offline. If the host dies and the file
  is gone, the validator slot is gone.
- **Never run two nodes with the same `priv_validator_key.json` at the same
  time.** That's a double-sign and will get the validator slashed once slashing
  is on.

To print the consensus pubkey:

```bash
evmd comet show-validator --home ~/.evmd-mainnet
# prints {"@type":"/cosmos.crypto.ed25519.PubKey","key":"<base64>"}
```

The base64 string after `"key":` is what goes into `validators.yaml`.

### 4.3 Node key

`evmd init` also created `~/.evmd-mainnet/config/node_key.json`. This is a
peer identity (libp2p), not a signing key. To print the node ID:

```bash
evmd comet show-node-id --home ~/.evmd-mainnet
# prints e.g. 4a1b2c3d4e5f6789...
```

Node IDs show up in `persistent_peers` strings, like
`<node-id>@<host>:26656`.


## 5. Gather node identifiers

[coordinator]

Collect the following from each of the 4 genesis nodes (over a channel that
preserves integrity, e.g. signed git commit, Signal, GPG-signed file):

| What | From step |
|---|---|
| Moniker | step 3 |
| Operator address (`og1...`) | step 4.1 |
| Consensus pubkey (base64 ed25519) | step 4.2 |
| Node ID | step 4.3 |
| P2P endpoint (`<host>:26656`) | infra |

**Never collect or transmit `priv_validator_key.json`, `node_key.json`, or
operator mnemonics.** Each of those stays on the node that generated it.

Fill in `scripts/mainnet/validators.yaml` with one entry per node, and
`scripts/mainnet/foundation.yaml` with the foundation multisig address (and
member pubkeys for the record).


## 6. Build the pre-gentx genesis

[coordinator]

```bash
bash scripts/mainnet/genesis.sh \
  --validators-yaml scripts/mainnet/validators.yaml \
  --foundation-yaml scripts/mainnet/foundation.yaml \
  --genesis-time <RFC3339-UTC, at least 30 minutes in the future> \
  --out-dir ~/.evmd-mainnet-foundation
```

What the script does:

1. Runs `evmd init` to start a chain home.
2. Patches every genesis parameter (chain id, denoms, staking, mint set to 0,
   distribution set to 0, governance, slashing-disabled, EVM, feemarket,
   ERC-20, SVIP dormant, crisis, IBC, consensus block-max-gas).
3. Adds genesis accounts: each validator's symbolic 10 OPG. (Foundation gets
   0 at genesis by default; the `x/svip` module account is NOT pre-funded.
   Both are bridged in post-launch, see section 17.)
4. Sets `bank.supply` equal to the L1 sum (~40 OPG), enforcing the SDK
   invariant `supply == Σ balances`.
5. Runs sanity checks: inflation is 0, community tax is 0, max gas isn't
   `-1`, SVIP is not activated, denoms match, total balances equal
   `bank.supply`, and L1 supply is ≤ 1% of canonical (catches yaml mistakes).
6. Runs `evmd genesis validate`.

To rehearse the script without real keys:

```bash
bash scripts/mainnet/genesis.sh --dry-run --out-dir /tmp/og-dryrun
```

Publish `~/.evmd-mainnet-foundation/config/genesis.json` and its sha256:

```bash
shasum -a 256 ~/.evmd-mainnet-foundation/config/genesis.json
```


## 7. Place the pre-gentx genesis

[per node]

```bash
curl -fSL <genesis-url> -o /tmp/genesis.json
echo "<expected-sha256>  /tmp/genesis.json" | sha256sum -c -

cp /tmp/genesis.json ~/.evmd-mainnet/config/genesis.json
chmod 600 ~/.evmd-mainnet/config/genesis.json
```

Quick sanity checks:

```bash
jq -r '.chain_id' ~/.evmd-mainnet/config/genesis.json
# should print: opengradient_1486-1

jq -r '.app_state.staking.params.bond_denom' ~/.evmd-mainnet/config/genesis.json
# should print: ogwei

jq '.app_state.bank.balances | length' ~/.evmd-mainnet/config/genesis.json
# should print: 4  (validators only; foundation and svip module are unfunded at genesis)

jq -r '.app_state.bank.supply[0].amount' ~/.evmd-mainnet/config/genesis.json
# should print: 40000000000000000000  (40 OPG = 4 × 10 OPG validator self-stakes)
```


## 8. Run gentx

[per node]

Run on the host that holds the consensus key:

```bash
# 10 OPG = 10000000000000000000 ogwei
evmd genesis gentx <key-name> 10000000000000000000ogwei \
  --commission-rate 0.05 \
  --commission-max-rate 0.50 \
  --commission-max-change-rate 0.01 \
  --min-self-delegation 10000000000000000000 \
  --keyring-backend file \
  --chain-id opengradient_1486-1 \
  --home ~/.evmd-mainnet \
  --moniker <moniker> \
  --gas-prices 1000000000ogwei
```

This creates `~/.evmd-mainnet/config/gentx/gentx-*.json`. Hand that file (only
that file) to the coordinator.


## 9. Collect gentxs and produce the final genesis

[coordinator]

When all 4 gentxs are in:

```bash
cp received-gentx-*.json ~/.evmd-mainnet-foundation/config/gentx/
evmd genesis collect-gentxs --home ~/.evmd-mainnet-foundation
evmd genesis validate --home ~/.evmd-mainnet-foundation
shasum -a 256 ~/.evmd-mainnet-foundation/config/genesis.json
```

Publish the final `genesis.json` and its new sha256.


## 10. Place the final genesis

[per node]

```bash
curl -fSL <final-genesis-url> -o /tmp/genesis-final.json
echo "<final-sha256>  /tmp/genesis-final.json" | sha256sum -c -

# Replace the pre-gentx file. The gentx is now embedded in this version.
cp /tmp/genesis-final.json ~/.evmd-mainnet/config/genesis.json

evmd genesis validate --home ~/.evmd-mainnet
```

Post the locally computed sha256 back to the coordination channel. All 4 nodes
should report the same hash.


## 11. Edit config files

[per node]

### 11.1 `config.toml`: timing

Must match across the network. Don't change.

```bash
TOML=~/.evmd-mainnet/config/config.toml

sed -i 's/^timeout_propose =.*/timeout_propose = "2s"/'                $TOML
sed -i 's/^timeout_propose_delta =.*/timeout_propose_delta = "200ms"/' $TOML
sed -i 's/^timeout_prevote =.*/timeout_prevote = "500ms"/'             $TOML
sed -i 's/^timeout_prevote_delta =.*/timeout_prevote_delta = "200ms"/' $TOML
sed -i 's/^timeout_precommit =.*/timeout_precommit = "500ms"/'         $TOML
sed -i 's/^timeout_precommit_delta =.*/timeout_precommit_delta = "200ms"/' $TOML
sed -i 's/^timeout_commit =.*/timeout_commit = "1s"/'                  $TOML
```

For Prometheus and OpenTelemetry, follow the
[`monitoring/README.md`](../../monitoring/README.md) "Configure your node"
section before continuing.

### 11.2 `config.toml`: peering

Build the peer list from `validators.yaml`:

```bash
# All peer strings (one per line)
yq -r '.validators[] | "\(.node_id)@\(.p2p_endpoint)"' \
  scripts/mainnet/validators.yaml

# All node IDs (CSV, for unconditional_peer_ids / private_peer_ids)
yq -r '[.validators[].node_id] | join(",")' \
  scripts/mainnet/validators.yaml
```

For each node, drop its own entry from `persistent_peers` and `*_peer_ids`
(node N peers with the 3 others, not itself). Edit `config.toml`:

```toml
laddr = "tcp://0.0.0.0:26656"
external_address = ""                     # private node, do not advertise
seeds = ""
persistent_peers = "<3 other validators' peer strings, comma-separated>"
pex = false                               # do not gossip peers
addr_book_strict = true
unconditional_peer_ids = "<3 other validators' node IDs, comma-separated>"
private_peer_ids = "<same 3 node IDs>"
allow_duplicate_ip = false
```

### 11.3 `app.toml`

```bash
APP=~/.evmd-mainnet/config/app.toml

# Minimum gas price (matches the genesis floor of 1 ogwei)
sed -i 's|^minimum-gas-prices =.*|minimum-gas-prices = "1ogwei"|' $APP

# Pruning
sed -i 's|^pruning =.*|pruning = "default"|' $APP

# Turn on the APIs
sed -i '/^\[api\]/,/^\[/ s|^enable =.*|enable = true|'      $APP
sed -i '/^\[grpc\]/,/^\[/ s|^enable =.*|enable = true|'     $APP
sed -i '/^\[json-rpc\]/,/^\[/ s|^enable =.*|enable = true|' $APP
```

Bind addresses: `evmd init` already binds the host-facing services to localhost
by default (Cosmos REST `tcp://localhost:1317`, gRPC `localhost:9090`, EVM
JSON-RPC `127.0.0.1:8545`, EVM WebSocket `127.0.0.1:8546`, Cosmos RPC
`tcp://127.0.0.1:26657`). Prometheus instrumentation is **disabled** by
default (`prometheus = false`), so its world-bound `prometheus_listen_addr`
is dormant until [`monitoring/README.md`](../../monitoring/README.md) turns
it on. When you do, lock the listen address down to the subnet your
monitoring stack scrapes from:

```bash
sed -i 's|^prometheus_listen_addr = ":26660"|prometheus_listen_addr = "127.0.0.1:26660"|' $TOML
```


## 12. Launch

[per node]

Set up `evmd` as a systemd service. Replace `<user>` with the Linux account
that owns `~/.evmd-mainnet` (the user that ran the steps in §3).

```ini
# /etc/systemd/system/evmd.service
[Unit]
Description=evmd mainnet
After=network-online.target
Wants=network-online.target

[Service]
User=<user>
Group=<user>
ExecStart=/home/<user>/go/bin/evmd start --home /home/<user>/.evmd-mainnet
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable evmd
```

Don't start yet. About 5 minutes before `genesis_time`:

```bash
sudo systemctl start evmd
journalctl -u evmd -f
```

The log shows `Starting node` then `Waiting for genesis_time`. At
`genesis_time` consensus tries to start. As soon as 3 of 4 validators are
online (the 2/3 threshold), block 1 is produced.


## 13. Check it's working

[per node]

```bash
# PoA module is loaded (params is the only query this module exposes;
# call succeeding is the signal; the response body is intentionally empty)
evmd q poa params --node tcp://localhost:26657

# Validator records exist (validators are stored in x/staking; x/poa just
# gates who can be added to the set via /poa.MsgAddValidator)
evmd q staking validators --node tcp://localhost:26657
# expect 4 entries

# Active consensus set
evmd q comet-validator-set --node tcp://localhost:26657
# the consensus pubkey should match the one from step 4.2

# Node is signing blocks
journalctl -u evmd -f | grep -i 'committed state\|signed proposal\|signed vote'
# expect a signed vote about every 2 seconds
```

PoA enforcement runs in `xrplevm/node/v10/x/poa/ante/poa.go:18-23`, which
rejects any tx whose top-level message is `MsgDelegate`, `MsgUndelegate`,
`MsgBeginRedelegate`, or `MsgCancelUnbondingDelegation`. The decorator is
wired in at `ante/cosmos.go:44`, last in the antehandler chain. Validators
are created at genesis-time via `MsgCreateValidator` inside each gentx;
that message type is not on the blocked list, which is why genesis works.
Post-launch, new validators are admitted only through `/poa.MsgAddValidator`
governance proposals (see `evmd/docs/POA_ADD_VALIDATOR_VIA_GOV.md`).

Hook the node into the monitoring stack. The full how-to is in
[`monitoring/README.md`](../../monitoring/README.md).


## 14. Security checklist

- [ ] `priv_validator_key.json` is on the validator host only
- [ ] `~/.evmd-mainnet` is `chmod 700`. Key files are `chmod 600`.
- [ ] Operator mnemonic is in a hardware-backed password manager, not on the host
- [ ] SSH is key-only, root login is off, only the bastion can reach port 22
- [ ] P2P (port 26656) only accepts traffic from other genesis nodes
- [ ] Public ingress to 26657, 8545, 8546, 9090, and 1317 is blocked
- [ ] `pex = false`
- [ ] `private_peer_ids` lists every other validator's node ID
- [ ] Time is in sync (chrony offset under 100 ms)
- [ ] Disk encryption at rest is on
- [ ] A cold backup of `priv_validator_key.json` exists, encrypted, off-host
- [ ] `priv_validator_key.json` has never been copied to a second host


## 15. Troubleshooting

### Node won't start: "genesis doc hash mismatch"

The local `genesis.json` differs from what other nodes have. Re-fetch the final
file from the coordinator and check the sha256.

### Node starts but no blocks

- `journalctl -u evmd -f` shows "no peers" → typo in `persistent_peers` or a
  firewall blocking port 26656.
- `curl -s localhost:26657/status | jq '.result.sync_info'` → `catching_up:
  true` means the node is behind. `false` with no height increment means
  consensus is stuck (probably fewer than 3 of 4 validators online).

### `eth_chainId` returns the wrong value

The EVM chain ID is `1486` decimal, `0x5ce` hex. Anything else means the wrong
`genesis.json` was placed. Stop, re-fetch, restart.


## 16. Chain settings reference

| Setting | Value |
|---|---|
| Chain ID | `opengradient_1486-1` (EVM chain ID 1486 / `0x5ce`) |
| Binary | `evmd` |
| Bech32 prefix | `og` (validators: `ogvaloper`) |
| Base denom | `ogwei` (18 decimals) |
| Display denom and symbol | `OPG` |
| Canonical supply (Base ERC-20) | 1,000,000,000 OPG. The fully-bridgeable supply lives here. |
| L1 `bank.supply` at genesis | 40 OPG (4 × 10 OPG validator self-stakes). Everything else stays on Base and bridges in on demand. |
| L1 inflation | 0 |
| Community tax | 0 |
| Block time | about 2 seconds |
| Block max gas | 40,000,000 |
| Min gas price | 1 ogwei |
| Base fee at genesis | 1 Gwei (1,000,000,000 ogwei). Goes to validators, not burned. |
| Slashing at launch | off |
| Validator set at launch | PoA (xrplevm `x/poa`). New validators added via `/poa.MsgAddValidator` gov proposals. `MsgDelegate` / `MsgUndelegate` / `MsgBeginRedelegate` / `MsgCancelUnbondingDelegation` are blocked at the ante handler. |
| Unbonding period | 21 days |
| Min commission | 5% |
| Max commission per validator | 50% |
| Max commission daily change | 1% |
| Governance min deposit | 5,000 OPG |
| Voting period | 5 days |
| Quorum / threshold / veto | 33.4% / 50% / 33.4% |
| SVIP allocation | 10% of canonical supply (100 million OPG). Bridged from Base into the `x/svip` module account by Foundation BEFORE governance activates SVIP. Not present in genesis. |


## 17. Post-launch bridge dependencies

The chain has 40 OPG at block 1. Every other on-chain action depends on Foundation
bridging OPG from Base to L1 first.

**Foundation operational liquidity (immediate post-launch).**
The Foundation multisig holds 0 OPG at genesis. To submit any L1 transaction
(including the first governance proposal), the Foundation must bridge OPG from
Base into its multisig address. The minimum that makes sense is:

- 1× governance proposal min deposit (5,000 OPG)
- ~1 OPG for gas runway

So a first bridge of ~10,000 OPG to the Foundation multisig unblocks day-1
governance.

**SVIP pool funding (before activation).**
The 100 million OPG SVIP pool is bridged from Base into the `x/svip` module
account on L1 BEFORE governance activates SVIP. The module account address is
deterministic:

```
og157j8m5l05q0theh7fep9ejqkqkejtwxdxtwzqh
```

It is derived as `bech32(og, sha256("svip")[:20])`, which equals the Cosmos SDK's
`authtypes.NewModuleAddress("svip").String()` with the chain's `og` bech32 prefix.
Confirm the address pre-bridge with:

```bash
evmd debug addr "$(printf 'svip' | shasum -a 256 | awk '{print substr($1,1,40)}')"
# Bech32 Acc og157j8m5l05q0theh7fep9ejqkqkejtwxdxtwzqh
```

Activation flow:

1. Foundation locks 100M OPG on the Base ERC-20 contract via the bridge.
2. Bridge mints 100M OPG into the L1 svip module address.
   Verify with `evmd q bank balance og157j8m5l05q0theh7fep9ejqkqkejtwxdxtwzqh`.
3. Governance proposal sets `svip.params.half_life_seconds` and flips
   `svip.activated = true`. The keeper snapshots `pool_balance_at_activation`
   from the module's bank balance at activation block.

**Why not just put 100M in the genesis?**
Tokens seated in genesis without a corresponding lock on Base are unbacked: if
all L1 OPG holders try to bridge to Base for trading, the Base contract runs out
of unlocked supply for that 10%. Bridging the pool from Base post-launch keeps
the canonical-on-Base invariant intact and avoids that depeg/run risk.


If anything in this guide is wrong or unclear, flag it on the coordination
channel before launch.
