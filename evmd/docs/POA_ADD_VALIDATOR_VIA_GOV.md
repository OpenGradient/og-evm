# Add a PoA Validator via Governance — Step by Step

This guide walks through adding a new validator using the PoA module and governance. Use it when the E2E script is not suitable or you want to run each step manually.

**Paths in this repo (from repo root):**

- **This guide:** `evmd/docs/POA_ADD_VALIDATOR_VIA_GOV.md`
- **Example proposal (template):** `evmd/docs/poa_add_validator_proposal.json` — replace `authority`, `validator_address`, and `pubkey` with your values from Steps 1–2.

---

## How PoA prevents direct validator changes

Under PoA (Proof of Authority), **normal staking is disabled** so the validator set is controlled only by governance:

- The PoA **ante decorator** rejects any Cosmos tx that contains:
  - `MsgDelegate`
  - `MsgUndelegate`
  - `MsgBeginRedelegate`
  - `MsgCancelUnbondingDelegation`
- So users **cannot** delegate, undelegate, redelegate, or cancel unbonding. The validator set and delegations cannot be changed through regular txs.
- **Adding a validator** is only possible via a **governance proposal** whose message is `MsgAddValidator` with **authority** = gov module. When the proposal passes, the PoA module mints initial stake and creates the validator; it does **not** use a user’s self-bond or delegation.

So “direct” validator creation (e.g. a user sending create-validator with their own funds) is not the path here; the only supported path is **gov proposal → MsgAddValidator**.

### How to test that PoA is active

Before running the add-validator flow, you can confirm PoA is blocking delegation:

1. **Delegate (should fail)**  
   With the chain running and keys/chain-id matching your setup (e.g. `--home ~/.og-evm-devnet --chain-id 10740 --keyring-backend test`):

   ```bash
   VALIDATOR=$(evmd query staking validators --home ~/.og-evm-devnet -o json 2>/dev/null | sed -n '/^{/,$ p' | jq -r '.validators[0].operator_address')
   evmd tx staking delegate "$VALIDATOR" 1000000ogwei --from dev0 --keyring-backend test --home ~/.og-evm-devnet --chain-id 10740 -y
   ```

   **Expected:** The tx is **rejected** with an error containing **`tx type not allowed`**. That indicates the PoA ante is active and delegation is blocked.

2. **Optional — other Cosmos txs still work**  
   For example, a bank send should succeed; only the staking messages above are blocked.

Once you’ve confirmed that, you can safely follow the steps below to add a validator via governance.

---

**Prerequisites**

- Chain is running (e.g. from repo root: `./local_node.sh -y`).
- All commands below use: `--home ~/.og-evm-devnet --chain-id 10740 --keyring-backend test`.
- `jq` is optional but helpful for parsing; otherwise copy values from the command output.

---

## Step 1. Get the gov module address (authority)

Run:

```bash
evmd query auth module-account gov --home ~/.og-evm-devnet -o json
```

In the output, ignore any lines like `=== REGISTERING TEE PRECOMPILE ===`. Find the JSON object and copy **`account.value.address`** (e.g. `og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k`). This is the **authority** for the proposal message.

With jq (only the JSON line is parsed; if your evmd prints extra lines, copy the JSON block to a file first or use the next command):

```bash
evmd query auth module-account gov --home ~/.og-evm-devnet -o json 2>/dev/null | sed -n '/^{/,$ p' | jq -r '.account.value.address'
```

Save this as **GOV_AUTHORITY** for the proposal.

---

## Step 2. Create the new validator account and consensus key

### 2a. Validator account (bech32 address)

Add a key that will be the new validator’s account. **Do not fund this address.**

```bash
evmd keys add newvalidator --keyring-backend test --home ~/.og-evm-devnet --no-backup
```

From the output, copy the **address** (e.g. `og1...`). This is **validator_address** in the proposal. The account must have no balance, no existing validator, and no delegations/unbonding when the proposal runs.

### 2b. Consensus pubkey (ed25519)

The chain expects a **consensus** key (ed25519), not the account key. Generate it in a **temporary directory** (do not use the running node’s config):

```bash
mkdir -p /tmp/poa-consensus-keys && cd /tmp/poa-consensus-keys
cometbft gen-validator
```

`cometbft gen-validator` prints JSON to stdout. Find the **`pub_key.value`** field (a base64 string). Example:

```json
"pub_key": {"type": "tendermint/PubKeyEd25519", "value": "aHlSZw+054g3XnuDblDq6TafZrXVK7xGuk9081g1FAk="}
```

Copy the **base64 value** (e.g. `aHlSZw+054g3XnuDblDq6TafZrXVK7xGuk9081g1FAk=`). You will use it in the proposal as the ed25519 pubkey.

Optional: clean up the temp dir so no validator keys remain:

```bash
rm -rf /tmp/poa-consensus-keys
```

---

## Step 3. Build the proposal JSON

Create a file (e.g. `proposal.json`) with this structure, or copy and edit the repo example **`evmd/docs/poa_add_validator_proposal.json`**. Replace:

- **authority** → gov module address from Step 1  
- **validator_address** → new validator account address from Step 2a  
- **key** (inside pubkey) → base64 value from Step 2b  

```json
{
  "messages": [
    {
      "@type": "/poa.MsgAddValidator",
      "authority": "<GOV_AUTHORITY from Step 1>",
      "validator_address": "<VALIDATOR_ADDRESS from Step 2a>",
      "description": {
        "moniker": "newvalidator",
        "identity": "",
        "website": "",
        "security_contact": "",
        "details": ""
      },
      "pubkey": {
        "@type": "/cosmos.crypto.ed25519.PubKey",
        "key": "<BASE64 from Step 2b>"
      }
    }
  ],
  "metadata": "Add PoA validator",
  "deposit": "10000000ogwei",
  "title": "Add validator",
  "summary": "Add a second validator via PoA governance",
  "expedited": false
}
```

Example with real-looking values:

```json
{
  "messages": [
    {
      "@type": "/poa.MsgAddValidator",
      "authority": "og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k",
      "validator_address": "og1xu4krmxl40ec0vlfx3lk38hkj0scw8z79ck225",
      "description": {
        "moniker": "newvalidator",
        "identity": "",
        "website": "",
        "security_contact": "",
        "details": ""
      },
      "pubkey": {
        "@type": "/cosmos.crypto.ed25519.PubKey",
        "key": "aHlSZw+054g3XnuDblDq6TafZrXVK7xGuk9081g1FAk="
      }
    }
  ],
  "metadata": "Add PoA validator",
  "deposit": "10000000ogwei",
  "title": "Add validator",
  "summary": "Add a second validator via PoA governance",
  "expedited": false
}
```

---

## Step 4. Submit the proposal

Use **enough gas** (e.g. 500000) so the tx does not run out of gas. From repo root you can use the example file:

```bash
evmd tx gov submit-proposal evmd/docs/poa_add_validator_proposal.json \
  --from dev0 \
  --gas-prices 3000ogwei \
  --gas 500000 \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  -y \
  --keyring-backend test
```

Or use your own file (e.g. `proposal.json`) after replacing authority, validator_address, and pubkey:

```bash
evmd tx gov submit-proposal proposal.json \
  --from dev0 \
  --gas-prices 3000ogwei \
  --gas 500000 \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  -y \
  --keyring-backend test
```

Note the **proposal id** from the output (or list proposals):

```bash
evmd query gov proposals --home ~/.og-evm-devnet -o json
```

Again, if output has non-JSON lines, use only the JSON part when parsing. The latest proposal’s `id` is the one you need.

---

## Step 5. Vote

Vote **soon** after the proposal enters the voting period (with `local_node.sh`, voting period is 30s). Use the key that has staking power (e.g. `mykey`):

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from mykey \
  --gas-prices 3000ogwei \
  --gas 300000 \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  -y \
  --keyring-backend test
```

Replace `<PROPOSAL_ID>` with the id from Step 4 (e.g. `1` or `2`).

---

## Step 6. Wait and verify

Wait for the voting period to end (e.g. 30–35 seconds with `local_node.sh`). Then check the proposal status:

```bash
evmd query gov proposal <PROPOSAL_ID> --home ~/.og-evm-devnet
```

You should see **status: PROPOSAL_STATUS_PASSED**.

List validators to confirm the new one is in the set:

```bash
evmd query staking validators --home ~/.og-evm-devnet -o json
```

You should see **two** validators: the original and the new one (moniker e.g. `newvalidator`).

---

## Troubleshooting

| Issue | What to check |
|-------|----------------|
| jq parse error | evmd may print TEE/precompile lines before JSON. Use only the JSON part (e.g. copy from first `{` to last `}`) or use `sed -n '/^{/,$ p'` before piping to jq. |
| Submit out of gas | Use `--gas 500000` (or higher) on submit-proposal. |
| Vote: "inactive proposal" | Voting period (30s) ended before your vote was included. Submit a new proposal and vote immediately. |
| Execution fails | Ensure the new validator account has **no** balance, no existing validator, and no delegations/unbonding. |

---

## Quick reference

- **Gov authority:** `evmd query auth module-account gov --home ~/.og-evm-devnet -o json` → `account.value.address`
- **New validator address:** `evmd keys add newvalidator ...` → address in output
- **Consensus pubkey:** `cometbft gen-validator` in a temp dir → `pub_key.value` (base64)
- **Proposal:** One message `@type: /poa.MsgAddValidator` with authority, validator_address, description, pubkey; deposit `10000000ogwei`
- **Submit:** `evmd tx gov submit-proposal evmd/docs/poa_add_validator_proposal.json --from dev0 --gas 500000 ...` (from repo root; or use your own `proposal.json`)
- **Vote:** `evmd tx gov vote <id> yes --from mykey ...`
- **Verify:** `evmd query staking validators --home ~/.og-evm-devnet`
