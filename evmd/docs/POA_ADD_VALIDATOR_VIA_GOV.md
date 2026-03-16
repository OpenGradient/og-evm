# Add a PoA Validator via Governance

This guide walks you through adding a new validator using the PoA module and governance. The commands here are written for a local devnet. If you are setting this up on mainnet, the steps are the same but some values will differ. Check the [Notes for mainnet](#notes-for-mainnet) section at the end before you start.

Proposal template: `evmd/docs/poa_add_validator_proposal.json` (fill in your values from Steps 1 and 2)

---

## How PoA works

Under PoA (Proof of Authority), normal staking is disabled. The validator set is controlled only through governance.

The PoA ante decorator rejects any transaction that contains these message types:

- `MsgDelegate`
- `MsgUndelegate`
- `MsgBeginRedelegate`
- `MsgCancelUnbondingDelegation`

This means nobody can delegate, undelegate, or redelegate through regular transactions. The only way to add a validator is through a governance proposal using `MsgAddValidator`. When the proposal passes, the PoA module mints the initial stake and creates the validator on its own. It does not rely on a user's self-bond or delegation.

---

## Prerequisites

- The chain is running. You can start it from the repo root with `./local_node.sh -y`.
- The examples in this guide use the devnet defaults: home directory `~/.og-evm-devnet`, chain ID `10740`, and the test keyring. Adjust these if your setup is different. See the "Notes for mainnet" section at the bottom for production differences.
- `jq` is helpful for parsing JSON output but not strictly required.
- `cometbft` CLI is needed for generating the consensus key in Step 2b. This guide was tested with cometbft v0.38.x. If you don't have it installed, follow the official install guide: https://docs.cometbft.com/v0.38/guides/install

### Quick check: is PoA active?

Before going through the full flow, you can verify that PoA is actually blocking delegation. Try sending a delegate transaction (it should fail):

```bash
VALIDATOR=$(evmd query staking validators \
  --home ~/.og-evm-devnet -o json 2>/dev/null \
  | sed -n '/^{/,$ p' \
  | jq -r '.validators[0].operator_address')

evmd tx staking delegate "$VALIDATOR" 1000000ogwei \
  --from dev0 \
  --keyring-backend test \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  --gas-prices 300000ogwei \
  --gas 300000 \
  -y
```

You should see an error with `tx type not allowed`. That confirms the PoA ante decorator is working. Other transactions like bank sends will still go through normally.

---

## Step 1. Get the governance module address

Every governance proposal needs an `authority`, which is the address of the gov module. Query it like this:

```bash
evmd query auth module-account gov \
  --home ~/.og-evm-devnet -o json 2>/dev/null \
  | sed -n '/^{/,$ p' \
  | jq -r '.account.value.address'
```

You should get something like `og10d07y265gmmuvt4z0w9aw880jnsr700jrdya3k`. Save this value, you will need it as the `authority` field in the proposal JSON.

> **Note:** `evmd` sometimes prints extra lines like `=== REGISTERING TEE PRECOMPILE ===` before the JSON. The `sed` command above filters those out. If you're not using jq, just look for the JSON block in the output and copy the address from `account.value.address`.

---

## Step 2. Create the new validator's keys

A validator in the Cosmos ecosystem needs two separate keys:

- An **account key** (Step 2a): this is the validator's on-chain identity, a regular bech32 address.
- A **consensus key** (Step 2b): this is an ed25519 key that the validator uses to sign blocks. It is different from the account key. You will need this consensus key later in the proposal JSON.

### 2a. Validator account

Create a new key for the validator. The PoA module expects a fresh account, so do not fund this address. If the address already has funds or an existing validator record, the proposal will fail.

```bash
evmd keys add newvalidator \
  --keyring-backend test \
  --home ~/.og-evm-devnet \
  --no-backup
```

> The `--no-backup` flag skips showing the mnemonic. This is fine for devnet/testing but you should back up the mnemonic for any real deployment.

Copy the `address` from the output (looks like `og1...`). This will be the `validator_address` in the proposal.

### 2b. Consensus key

The consensus key is what the validator actually uses to participate in consensus and sign blocks. In Cosmos chains, this is always an ed25519 key, separate from the account key you created above.

Generate one using the `cometbft` CLI:

```bash
cometbft gen-validator | jq -r '.Key.pub_key.value'
```

This prints just the consensus key value you need. Copy it. You will plug this into the proposal JSON in the next step.

> **Heads up:** Every time you run `cometbft gen-validator`, it generates a new key. If you run it again, you will get a different key. So copy the value the first time and save it somewhere.

If you want to see the full output including the private key, run `cometbft gen-validator | jq '.'` instead. The consensus key is the `value` string inside `Key.pub_key`.

> **Important:** In a production setup, the consensus key's private portion must be securely stored on the machine that will actually run the validator node. For this devnet walkthrough, you only need the public key for the proposal.

---

## Step 3. Build the proposal JSON

Now put together the governance proposal. You can either create a new file or copy and edit the template at `evmd/docs/poa_add_validator_proposal.json`.

Fill in the three values you collected:

- `authority`: the gov module address from Step 1
- `validator_address`: the new validator's account address from Step 2a
- `key` (inside `pubkey`): the consensus key from Step 2b

`moniker` is the validator's display name. The other description fields (`identity`, `website`, `security_contact`, `details`) are optional and can be left empty.

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
        "key": "<CONSENSUS_KEY from Step 2b>"
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

Save this as `proposal.json` (or whatever name you prefer).

---

## Step 4. Submit the proposal

Submit the proposal with enough gas. The chain uses EIP-1559 style fee pricing, so the base fee can rise over time. A gas price of `300000ogwei` works on devnet. For mainnet values, see the "Notes for mainnet" section below.

```bash
evmd tx gov submit-proposal proposal.json \
  --from dev0 \
  --gas-prices 300000ogwei \
  --gas 500000 \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  --keyring-backend test \
  -y
```

Note the proposal ID from the output. You can also list all proposals to find it:

```bash
evmd query gov proposals \
  --home ~/.og-evm-devnet -o json 2>/dev/null \
  | sed -n '/^{/,$ p' \
  | jq '.proposals[-1].id'
```

---

## Step 5. Vote

Vote yes as soon as possible. With `local_node.sh`, the voting period is only 30 seconds, so you need to be quick.

Use the key that has staking power (on the devnet, that's `mykey`, not `dev0`):

```bash
evmd tx gov vote <PROPOSAL_ID> yes \
  --from mykey \
  --gas-prices 300000ogwei \
  --gas 300000 \
  --home ~/.og-evm-devnet \
  --chain-id 10740 \
  --keyring-backend test \
  -y
```

Replace `<PROPOSAL_ID>` with the ID from Step 4.

> **Why `mykey` and not `dev0`?** Only accounts with staking power (existing validators or their delegators) can cast votes that count toward the tally. On the devnet, `mykey` is the initial validator's key.

---

## Step 6. Wait and verify

Wait about 35 seconds for the voting period to end, then check the proposal status:

```bash
evmd query gov proposal <PROPOSAL_ID> --home ~/.og-evm-devnet
```

You should see `status: PROPOSAL_STATUS_PASSED`.

Now list the validators:

```bash
evmd query staking validators --home ~/.og-evm-devnet -o json 2>/dev/null \
  | sed -n '/^{/,$ p' \
  | jq '[.validators[] | {moniker: .description.moniker, status: .status}]'
```

You should see two validators: the original one and the new one (with moniker `newvalidator`).

> **What happens next?** The validator is now registered on-chain, but for it to actually produce blocks, a node needs to be running with the matching consensus private key in its `config/priv_validator_key.json`. On devnet this does not matter since you are just testing the governance flow.

---

## Troubleshooting

| Issue | What to check |
|-------|---------------|
| jq parse error | `evmd` sometimes prints TEE/precompile log lines before the JSON. Use `sed -n '/^{/,$ p'` to filter them out, or just copy the JSON block manually. |
| Submit fails with out of gas | Increase `--gas` (try 500000 or higher). |
| Submit fails with gas price too low | The EIP-1559 base fee rises over time. Increase `--gas-prices` (try 300000ogwei or higher). |
| Vote says "inactive proposal" | The 30s voting period ended before your vote landed. Submit a new proposal and vote right away. |
| Proposal execution fails | Make sure the new validator account has no balance, no existing validator record, and no delegations. |
| Key already exists | You already created a key with that name. Use `evmd keys delete newvalidator --keyring-backend test --home ~/.og-evm-devnet` or pick a different name. |

---

## Notes for mainnet

This guide is written for a local devnet. If you are doing this on mainnet, a few things will be different:

- **Keyring backend.** You would not use `--keyring-backend test` on mainnet. Use `os` or `file` instead, and make sure you have the passphrase ready. The test backend stores keys unencrypted and is not safe for real funds.
- **Chain ID and home directory.** Replace `--chain-id 10740` and `--home ~/.og-evm-devnet` with your actual mainnet chain ID and node home directory.
- **Voting period.** On mainnet the voting period is much longer (days, not 30 seconds). You don't need to rush the vote. Coordinate with other validators to make sure the proposal reaches quorum.
- **Gas prices.** The base fee on mainnet will be different from devnet. Check current gas prices before submitting. You can use `evmd query feemarket base-fee` to see the current base fee.
- **Consensus key security.** On devnet we only care about the public key for the proposal. On mainnet, the private key must live on the validator machine in a secure location (typically `config/priv_validator_key.json`). Treat it like a password. If someone gets your consensus private key, they can double-sign on your behalf and get your validator slashed.
- **Validator account.** On mainnet, back up the mnemonic when creating the validator key. Do not use `--no-backup`.
- **Deposit amount.** The minimum deposit for a proposal may differ on mainnet. Check your chain's gov params with `evmd query gov params`.
- **Who submits and who votes.** On devnet we use `dev0` to submit and `mykey` to vote because that is how the local setup is configured. On mainnet, any funded account can submit a proposal, and all validators (or their delegators) with staking power can vote.
