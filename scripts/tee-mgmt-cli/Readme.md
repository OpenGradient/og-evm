# TEE Registry CLI

Command-line tool for managing the TEE Registry contract on the OpenGradient network.

## Quick Start

```bash
cp .env.example .env   # configure registry address, private key
go build -o tee-cli
./tee-cli tee list
```

## Prerequisites

- Go 1.21+
- Access to the OpenGradient network
- A funded account (for write operations)

## Configuration

Connection settings can be provided via flags, environment variables, or a `.env` file.
Flags take precedence over env vars, which take precedence over `.env` defaults.

| Flag | Env Var | Description |
|------|---------|-------------|
| `--rpc-url` | `RPC_URL` | OpenGradient RPC endpoint |
| `--registry` | `TEE_REGISTRY_ADDRESS` | TEE Registry contract address |
| `--private-key` | `TEE_PRIVATE_KEY` | Private key for signing transactions |

## Commands

### `tee` — TEE Instance Management

```bash
tee-cli tee list                          # List all active TEEs
tee-cli tee show <tee_id>                 # Show TEE details (owner, endpoint, PCR, keys)
tee-cli tee register --enclave-host HOST  # Register a new TEE from an enclave
tee-cli tee activate <tee_id>             # Re-activate a deactivated TEE
tee-cli tee deactivate <tee_id>           # Deactivate a TEE
```

Registration flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--enclave-host` | *(required)* | Enclave hostname or IP |
| `--enclave-port` | `443` | Enclave TLS port |
| `--payment-address` | sender address | Payment address for the TEE |
| `--endpoint` | `https://<enclave-host>` | Public endpoint URL |
| `--tee-type` | `0` | TEE type ID (0=LLMProxy, 1=Validator) |

### `pcr` — PCR Measurement Management

```bash
tee-cli pcr list                          # List approved PCR hashes
tee-cli pcr check <pcr_hash>              # Check if a PCR hash is approved
tee-cli pcr compute -m measurements.json  # Compute PCR hash without submitting
tee-cli pcr approve -m measurements.json  # Approve PCR measurements on-chain
tee-cli pcr revoke <pcr_hash>             # Revoke an approved PCR hash
```

PCR values can be provided via a measurements JSON file (`-m`) or individual flags (`--pcr0`, `--pcr1`, `--pcr2`).

Approve flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-m`, `--measurements-file` | | Path to measurements JSON |
| `--pcr0`, `--pcr1`, `--pcr2` | | Individual PCR hex values |
| `-v`, `--version` | `v1.0.0` | Version label |
| `--grace-period` | `0` | Seconds before previous PCR is revoked |
| `--previous-pcr` | | PCR hash being rotated out (bytes32 hex) |

### `type` — TEE Type Definitions

```bash
tee-cli type list                         # List registered TEE types
tee-cli type add <type_id> <name>         # Add a new TEE type
```

### `role` — Access Control

```bash
tee-cli role check <admin|operator> <addr>  # Check if address has role
tee-cli role grant-admin <addr>             # Grant DEFAULT_ADMIN_ROLE
tee-cli role grant-operator <addr>          # Grant TEE_OPERATOR role
tee-cli role revoke-admin <addr>            # Revoke DEFAULT_ADMIN_ROLE
tee-cli role revoke-operator <addr>         # Revoke TEE_OPERATOR role
```

### `cert` — Root Certificates

```bash
tee-cli cert set-aws <cert_file>          # Set AWS Nitro Enclaves root cert (PEM or DER)
```

## Measurements File Format

```json
{
  "Measurements": {
    "PCR0": "abc123...",
    "PCR1": "def456...",
    "PCR2": "789012..."
  }
}
```

## Troubleshooting

**"no unlocked accounts available"** — Set `--private-key` or `TEE_PRIVATE_KEY`.

**"failed to connect to RPC"** — Verify `--rpc-url` is correct and reachable.

**Transaction reverts** — Ensure your account has the required role (admin/operator) and sufficient balance for gas.
