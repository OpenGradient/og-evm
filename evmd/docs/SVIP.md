# Sustained Validator Incentive Pool (SVIP)

SVIP pays validators from a pre-funded token pool. The rewards start high and slowly decrease over time (exponential decay). Every block, the module sends a small amount to the FeeCollector, and the existing `x/distribution` module splits it across validators based on their voting power.

There's no inflation. The pool is funded by bridging tokens from Base.

Proposal templates: `evmd/docs/svip_update_params_proposal.json`, `evmd/docs/svip_activate_proposal.json`

---

## How it works

Every block, SVIP does this:

1. Checks how much time has passed since activation.
2. Calculates the reward for this block using exponential decay: `reward = R₀ × e^(-λt) × Δt`.
3. Sends that amount from the SVIP module account to the FeeCollector.
4. `x/distribution` takes it from there and pays validators.

The starting rate `R₀` comes from the pool size and half-life: `R₀ = (ln2 / half_life) × pool_balance`. This math guarantees the pool can never pay out more than it holds. The total converges to exactly the pool size over infinite time.

### Funding the pool

You can't send tokens directly to the SVIP module address with `bank send`. That will fail with `unauthorized` (standard Cosmos SDK behavior for module accounts). Use the dedicated command instead:

```bash
evmd tx svip fund-pool <coins>
```

This command uses a manual CLI handler (not the SDK's auto-generated one) to work around a known SDK compatibility issue.

---

## Lifecycle

```
1. Genesis      → Module registered, pool empty, not activated
2. Fund pool    → Bridge tokens from Base, call MsgFundPool
3. Set params   → Governance sets half_life_seconds via MsgUpdateParams
4. Activate     → Governance activates via MsgActivate
5. Rewards flow → Every block, decayed reward → FeeCollector → validators
6. (Optional)   → Pause, unpause, reactivate after refund
```

---

## Before you start

You'll need:

- A running node connected to the network.
- A funded account for submitting proposals, and a validator key (or coordination with validators) for voting.
- The `evmd` binary installed.
- Token amounts are always in base units with 18 decimals. Multiply by 10^18 to convert. For example, 1000 tokens = `1000000000000000000000` ogwei.

> **Devnet:** Run `./local_node.sh -y` to start a local chain. Home dir is `~/.og-evm-devnet`, chain ID is `10740`. `mykey` is the genesis validator. `dev0` through `dev3` are funded accounts. Voting period is 30 seconds.

---

## Governance basics

Everything except funding the pool goes through governance.

1. Someone **submits** a proposal with a deposit.
2. Validators (or delegators) **vote**: yes, no, abstain, or veto.
3. After the voting period, if quorum (33.4%) is met and >50% voted yes, the proposal **passes** and the message runs.
4. If >33.4% vote "NoWithVeto", the deposit is burned. Otherwise it's returned.

**Get the gov module address** (you'll need this for every proposal template):

```bash
evmd q auth module-account gov
```

Look for the `address` field (e.g. `og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k`). Use this wherever you see `<GOV_AUTHORITY>` in the templates.

**Get the minimum deposit:**

```bash
evmd q gov params
```

Look for `min_deposit`. Use this wherever you see `<MIN_DEPOSIT>` in the templates.

**Find your proposal ID** after submitting:

```bash
evmd q gov proposals
```

Your proposal is the last one in the list. Note the `id`, you'll need it to vote.

**Check proposal outcome** with `evmd q gov proposal <ID>`:
- `PROPOSAL_STATUS_PASSED`: votes passed and the message executed successfully.
- `PROPOSAL_STATUS_REJECTED`: not enough yes votes, or quorum wasn't met. Deposit is returned (unless vetoed).
- `PROPOSAL_STATUS_FAILED`: votes passed, but the message itself failed (e.g. a guardrail blocked it, or the pool was empty). This is what you'll see when something like `MsgActivate` gets rejected at the keeper level. Fix the issue and submit a new proposal.

> **Devnet:** Add `--home ~/.og-evm-devnet` to all commands. Only `mykey` has staking power, so use it for voting. Voting period is 30 seconds, so vote right after submitting.

---

## Step 1. Fund the pool

Anyone can do this. It's the only permissionless operation. On mainnet, tokens get bridged from Base via Hyperlane first, then the bridger calls `fund-pool`.

```bash
evmd tx svip fund-pool <amount> \
  --from <key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

> **Devnet example**, fund 1000 tokens from dev0:
> ```bash
> evmd tx svip fund-pool 1000000000000000000000ogwei \
>   --from dev0 --home ~/.og-evm-devnet --chain-id 10740 \
>   --keyring-backend test --gas 200000 --gas-prices 10000000ogwei --yes
> ```

Verify:

```bash
evmd q svip pool-state
```

You should see `pool_balance` with your funded amount. The pool must have funds before you can activate. `MsgActivate` will reject if the balance is zero.

---

## Step 2. Set half-life (governance)

Before activating, you need to set `half_life_seconds` through a governance proposal. The recommended mainnet value is **15 years** (`473364000` seconds). Minimum allowed is 1 year (`31536000` seconds).

**2a.** Copy the template and fill in the gov module address:

```bash
cp evmd/docs/svip_update_params_proposal.json /tmp/svip_set_params.json
```

Open `/tmp/svip_set_params.json` and replace `<GOV_AUTHORITY>` with the address from the governance basics section. It should look like:

```json
{
  "messages": [
    {
      "@type": "/cosmos.svip.v1.MsgUpdateParams",
      "authority": "og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k",
      "params": {
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

**2d.** Get validators to vote yes before the voting period ends:

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**2e.** After the voting period, check the result:

```bash
evmd q gov proposal <PROPOSAL_ID>
```

Status should be `PROPOSAL_STATUS_PASSED`.

**2f.** Verify the half-life was set:

```bash
evmd q svip params
```

> **Heads up:** Don't be alarmed if this shows `params: {}`. `half_life_seconds` is an integer, and proto3 serialization drops fields that equal their default value (`0`), so the whole thing looks empty even though the value is stored correctly. If you want proof, just move on to step 3. `MsgActivate` will reject with "half_life_seconds must be set before activation" if the value didn't stick.

> **Devnet:** Use `--from mykey --home ~/.og-evm-devnet --chain-id 10740 --keyring-backend test --gas 300000 --gas-prices 10000000ogwei` for both submit and vote. Deposit is `10000000ogwei`. Wait ~35 seconds after voting.

**Guardrails:**
- `half_life_seconds` must be at least 1 year.
- After activation, `half_life_seconds` can't change by more than 50% in a single proposal.

---

## Step 3. Activate (governance)

Activation snapshots the current pool balance and starts the decay curve. Before submitting, make sure:
- `half_life_seconds` is set (step 2).
- The pool has funds (step 1).
- You're ready. Once activated, it can't be deactivated.

**3a.** Copy the template:

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

**3d.** Get validators to vote yes:

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from <validator-key> \
  --chain-id <chain-id> \
  --keyring-backend <os|file> \
  --gas auto --gas-adjustment 1.3 \
  --yes
```

**3e.** After the voting period, check that rewards are flowing:

```bash
evmd q svip pool-state
```

You should see:
- `activated: true`
- `pool_balance` going down over time
- `total_distributed` going up
- `current_rate_per_second` showing a non-zero value

To confirm validators are actually getting paid:

```bash
evmd q distribution rewards <VALIDATOR_DELEGATOR_ADDRESS>
```

You should see a non-zero `total` in ogwei.

> **Devnet:** Same flags as the step 2 devnet note. Quick rewards check:
> ```bash
> ADDR=$(evmd keys show mykey -a --keyring-backend test --home ~/.og-evm-devnet)
> evmd q distribution rewards $ADDR --home ~/.og-evm-devnet
> ```

---

## Step 4. Pause / unpause (governance)

Emergency pause stops all rewards immediately. When you unpause later, the module skips over the paused time cleanly. There's no reward spike.

**To pause:**

**4a.** Copy the template:

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

**4c.** Find the proposal ID and vote:

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

**4d.** Verify it's paused:

```bash
evmd q svip pool-state
```

You should see `paused: true`, `current_rate_per_second: 0`, and `total_distributed` frozen. Run the query twice a few seconds apart. The number shouldn't change.

**To unpause:** Edit `/tmp/svip_pause.json` and change `"paused": true` to `"paused": false`. Then repeat steps 4b through 4d. After unpausing, `paused` will disappear from the output (proto3 hides `false`). Confirm it worked by checking that `current_rate_per_second` is non-zero again.

---

## Step 5. Reactivate (governance)

If the pool runs dry and you refund it, use `MsgReactivate` to restart the decay curve with a fresh snapshot. This resets `ActivationTime` and `TotalDistributed`. The curve starts over as if you just activated for the first time.

Why is this needed? The decay formula bases the reward rate on the pool snapshot from activation time. Without reactivating, refunded tokens would trickle out at the tail-end rate of the old curve (basically zero).

**5a.** Fund the pool again (step 1).

**5b.** Copy the template:

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

**5d.** Find the proposal ID and vote:

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

**5e.** Verify:

```bash
evmd q svip pool-state
```

`activation_time` should show the current time (not the original one), and `total_distributed` should be near zero (counting fresh from the new curve).

---

## Queries

```bash
# Module parameters (half_life_seconds)
evmd q svip params

# Pool state (balance, total distributed, current rate, activation time)
evmd q svip pool-state
```

> **About `params: {}`:** The params query uses proto3 serialization, which drops fields that equal their default value (`0` for integers). This means you'll see `params: {}` even when `half_life_seconds` is set to 0. Don't worry, the value is stored correctly. Use `q svip pool-state` for the full picture including activated/paused status.

---

## Reward math

Rewards follow exponential decay. They start high and gradually taper off. With a 100M token pool and a 15-year half-life, roughly half the pool (50M) gets distributed in the first 15 years. After 30 years, 75% is out. The rate keeps slowing down but never hits zero, and the pool can never overpay. The math guarantees total payouts converge to exactly the pool size.
