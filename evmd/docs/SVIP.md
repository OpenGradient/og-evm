# Sustained Validator Incentive Pool (SVIP)

The SVIP module distributes rewards to validators from a pre-funded token pool using exponential decay. It deposits tokens into the FeeCollector every block, and the existing `x/distribution` module splits them across validators by voting power.

There is no inflation. The pool is funded by bridging tokens from Base.

Proposal templates: `evmd/docs/svip_update_params_proposal.json`, `evmd/docs/svip_activate_proposal.json`

---

## How it works

Every block, SVIP runs in the BeginBlocker:

1. Reads the pool balance snapshot and time since activation.
2. Computes the reward using exponential decay: `reward = R₀ × e^(-λt) × Δt`.
3. Transfers the reward from the SVIP module account to the FeeCollector.
4. `x/distribution` picks it up and distributes to validators.

The initial rate `R₀` is derived from the pool balance and half-life: `R₀ = (ln2 / half_life) × pool_balance`. This guarantees the pool never over-distributes — the total converges to exactly the pool size over infinite time.

### `fund-pool` CLI

- You cannot send tokens directly to the SVIP module address via `bank send` — it will fail with `unauthorized`. This is standard Cosmos SDK behavior for module accounts.
- Instead, use the dedicated command: `evmd tx svip fund-pool <coins>`.
- This command uses a manual CLI handler (not the SDK's auto-generated one) to avoid a known SDK compatibility issue.

---

## Lifecycle

```
1. Genesis      → SVIP module registered, pool empty, not activated
2. Fund pool    → Bridge tokens from Base, call MsgFundPool
3. Set params   → Governance sets half_life_seconds via MsgUpdateParams
4. Activate     → Governance activates via MsgActivate
5. Rewards flow → Every block, decayed reward → FeeCollector → validators
6. (Optional)   → Pause, unpause, reactivate after refund
```

---

## Prerequisites

- A running node connected to the network.
- Access to a funded account for submitting proposals, and access to a validator key (or coordination with validators) for voting.
- The `evmd` binary installed.
- All token amounts are in base units with 18 decimals. To convert: multiply the token amount by 10^18. For example, 1000 tokens = `1000` followed by 18 zeros = `1000000000000000000000` ogwei.

> **Devnet:** Start a local chain with `./local_node.sh -y`. This gives you home `~/.og-evm-devnet`, chain ID `10740`, and test keyring. `mykey` is the genesis validator. `dev0`-`dev3` are funded accounts. The voting period is 30 seconds.

---

## Governance basics

All SVIP operations except funding the pool require a governance proposal.

1. A funded account **submits** a proposal with a deposit.
2. Validators (or their delegators) **vote** (yes / no / abstain / veto).
3. After the voting period, if quorum (33.4%) is met and >50% voted yes, the proposal **passes** and the message executes.
4. If >33.4% vote "NoWithVeto", the deposit is burned. Otherwise it is returned.

**Finding the gov module address** (needed for all proposal templates):

```bash
evmd q auth module-account gov
```

Look for the `address` field (e.g. `og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k`). Use this wherever you see `<GOV_AUTHORITY>` in proposal templates.

**Finding the minimum deposit** (needed for all proposals):

```bash
evmd q gov params
```

Look for `min_deposit` — this is the minimum amount required for a proposal to enter the voting period. Use this value wherever you see `<MIN_DEPOSIT>` in proposal templates.

**Finding the proposal ID** after submitting:

```bash
evmd q gov proposals
```

The last proposal in the list is yours. Note the `id` field — you will need it to vote.

**If a proposal fails:** check the status with `evmd q gov proposal <ID>`. Common reasons:
- `PROPOSAL_STATUS_REJECTED` — not enough yes votes or quorum not met.
- `PROPOSAL_STATUS_FAILED` — the message execution failed (e.g. guardrail violation, empty pool). Submit a new corrected proposal.

> **Devnet:** Add `--home ~/.og-evm-devnet` to all commands. Only `mykey` has staking power — use it for voting. Voting period is 30 seconds; vote immediately after submitting.

---

## Step 1. Fund the pool

Anyone can fund the pool. This is the only permissionless operation. On mainnet, tokens are bridged from Base via Hyperlane first, then the bridger calls `fund-pool`.

```bash
evmd tx svip fund-pool <amount> \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

> **Devnet example** — fund 1000 tokens from dev0:
> ```bash
> evmd tx svip fund-pool 1000000000000000000000ogwei \
>   --from dev0 --home ~/.og-evm-devnet --chain-id 10740 \
>   --keyring-backend test --gas 200000 --gas-prices 10000000ogwei --yes
> ```

Verify:

```bash
evmd q svip pool-state
```

You should see `pool_balance` showing your funded amount. The pool must be funded before activation — `MsgActivate` will reject if the balance is zero.

---

## Step 2. Set half-life (governance)

Before activation, you must set `half_life_seconds` via a governance proposal. The recommended value for mainnet is **15 years** (`473364000` seconds). The minimum allowed value is 1 year (`31536000` seconds).

**2a.** Prepare the proposal file. Copy the template and fill in the gov module address:

```bash
cp evmd/docs/svip_update_params_proposal.json /tmp/svip_set_params.json
```

Open `/tmp/svip_set_params.json` in any editor and replace `<GOV_AUTHORITY>` with the address from the governance basics section. The file should look like:

```json
{
  "messages": [
    {
      "@type": "/cosmos.evm.svip.v1.MsgUpdateParams",
      "authority": "og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k",
      "params": {
        "activated": false,
        "paused": false,
        "half_life_seconds": "473364000"
      }
    }
  ],
  "deposit": "<MIN_DEPOSIT>",
  "title": "Set SVIP half-life to 15 years",
  "summary": "Configure the SVIP exponential decay half-life before activation."
}
```

**2b.** Submit:

```bash
evmd tx gov submit-proposal /tmp/svip_set_params.json \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**2c.** Find the proposal ID:

```bash
evmd q gov proposals
```

**2d.** Coordinate with validators to vote yes before the voting period ends:

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**2e.** After the voting period ends, check the result:

```bash
evmd q gov proposal <PROPOSAL_ID>
```

Status should be `PROPOSAL_STATUS_PASSED`.

> **Devnet:** Use `--from mykey --home ~/.og-evm-devnet --chain-id 10740 --keyring-backend test --gas 300000 --gas-prices 10000000ogwei` for both submit and vote. Deposit is `10000000ogwei`. Wait ~35 seconds after voting.

**Guardrails on UpdateParams:**
- Cannot set `activated` to `false` after activation (irreversible).
- `half_life_seconds` must be ≥ 1 year.
- `half_life_seconds` cannot change by more than 50% in a single proposal.

---

## Step 3. Activate (governance)

Activation snapshots the current pool balance and starts the decay curve. Requirements:
- `half_life_seconds` must be set (step 2).
- The pool must have a non-zero balance (step 1).
- This is a one-time operation — once activated, it cannot be deactivated.

**3a.** Prepare the proposal file:

```bash
cp evmd/docs/svip_activate_proposal.json /tmp/svip_activate.json
```

Open `/tmp/svip_activate.json` and replace `<GOV_AUTHORITY>` with your gov address.

**3b.** Submit:

```bash
evmd tx gov submit-proposal /tmp/svip_activate.json \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**3c.** Find the proposal ID:

```bash
evmd q gov proposals
```

**3d.** Coordinate with validators to vote yes:

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**3e.** After the voting period, verify rewards are flowing:

```bash
evmd q svip pool-state
```

You should see:
- `activated: true`
- `pool_balance` decreasing over time
- `total_distributed` increasing
- `current_rate_per_second` non-zero

To confirm validators are receiving rewards:

```bash
evmd q distribution rewards <VALIDATOR_DELEGATOR_ADDRESS>
```

You should see a non-zero `total` in ogwei.

> **Devnet:** Use the same flags as step 2 devnet note. To quickly check rewards:
> ```bash
> ADDR=$(evmd keys show mykey -a --keyring-backend test --home ~/.og-evm-devnet)
> evmd q distribution rewards $ADDR --home ~/.og-evm-devnet
> ```

---

## Step 4. Pause / unpause (governance)

Emergency pause stops all reward distribution immediately. When unpaused, the module skips the paused gap cleanly — there is no reward spike.

**To pause:**

**4a.** Prepare the proposal file:

```bash
cp evmd/docs/svip_pause_proposal.json /tmp/svip_pause.json
```

Open `/tmp/svip_pause.json` and replace `<GOV_AUTHORITY>` with your gov address.

**4b.** Submit:

```bash
evmd tx gov submit-proposal /tmp/svip_pause.json \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**4c.** Find the proposal ID and coordinate voting:

```bash
evmd q gov proposals
```

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**4d.** After the voting period, verify:

```bash
evmd q svip pool-state
```

You should see `paused: true`, `current_rate_per_second: 0`, and `total_distributed` frozen (query twice a few seconds apart — the number should not change).

**To unpause:** edit `/tmp/svip_pause.json` and change `"paused": true` to `"paused": false`. Then repeat steps 4b through 4d.

---

## Step 5. Reactivate (governance)

If the pool runs out and is refunded, `MsgReactivate` restarts the decay curve with a fresh pool snapshot. It resets `ActivationTime` and `TotalDistributed` — the curve starts over as if freshly activated.

This is needed because the decay formula derives the reward rate from the pool snapshot taken at activation. Without reactivation, refunded tokens would drain at the tail-end rate of the old curve (nearly zero).

**5a.** Fund the pool again (step 1).

**5b.** Prepare the proposal file:

```bash
cp evmd/docs/svip_reactivate_proposal.json /tmp/svip_reactivate.json
```

Open `/tmp/svip_reactivate.json` and replace `<GOV_AUTHORITY>` with your gov address.

**5c.** Submit:

```bash
evmd tx gov submit-proposal /tmp/svip_reactivate.json \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**5d.** Find the proposal ID and coordinate voting:

```bash
evmd q gov proposals
```

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**5e.** After the voting period, verify:

```bash
evmd q svip pool-state
```

`activation_time` should show the current time (not the original), and `total_distributed` should be near zero (counting from the new curve).

---

## Queries

```bash
# Module parameters (activated, paused, half_life_seconds)
evmd q svip params

# Pool state (balance, total distributed, current rate, activation time)
evmd q svip pool-state
```

> **Note:** `q svip params` may show `params: {}` when values are at defaults (false/0). This is cosmetic. The values are stored correctly — use `q svip pool-state` to confirm `activated: true` and see the current rate.

---

## Reward math

Rewards follow exponential decay — they start high and gradually decrease over time. With a 100M token pool and a 15-year half-life, roughly half the pool (50M) is distributed in the first 15 years. After 30 years, 75% is distributed. The rate slows down but never hits zero, and the pool can never over-distribute — the math guarantees total payouts converge to exactly the pool size.
