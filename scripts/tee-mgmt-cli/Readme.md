# TEE Registry CLI

A dev command-line tool for managing the TEE Registry on OpenGradient.

## Quick Start

```bash
# 1. Copy environment file
cp .env.example .env

# 2. Edit with your settings
nano .env

# 3. Build
go build -o tee-cli

# 4. Run
./tee-cli list
```

## Installation

### Prerequisites

- Go 1.21+
- Access to OpenGradient RPC endpoint

### Build

```bash
go build -o tee-cli
```

## Configuration

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

| Variable | Description | Default |
|----------|-------------|---------|
| `TEE_RPC_URL` | RPC endpoint | `http://13.59.43.94:8545` |
| `TEE_REGISTRY_ADDRESS` | Contract address | `0x3d641a2791533b4a...` |
| `TEE_PRIVATE_KEY` | Private key for signing | (uses node account if empty) |
| `ENCLAVE_HOST` | Enclave hostname | - |
| `ENCLAVE_PORT` | Enclave port | `443` |
| `MEASUREMENTS_FILE` | Path to measurements.txt | `measurements.txt` |

## Commands

### TEE Management

```bash
# List all active TEEs
./tee-cli list

# Show TEE details
./tee-cli show <tee_id>

# Register new TEE from enclave
./tee-cli register

# Activate/Deactivate TEE
./tee-cli activate <tee_id>
./tee-cli deactivate <tee_id>
```

### PCR Management

```bash
# List approved PCRs
./tee-cli pcr-list

# Approve PCR (from measurements file or env vars)
./tee-cli pcr-approve

# Check if PCR is approved
./tee-cli pcr-check <pcr_hash>

# Revoke PCR
./tee-cli pcr-revoke <pcr_hash>

# Compute PCR hash only
./tee-cli pcr-compute

```

### TEE Types

```bash
# List TEE types
./tee-cli type-list

# Add new type
./tee-cli type-add 0 LLMProxy
./tee-cli type-add 1 Validator

# Deactivate type
./tee-cli type-deactivate <type_id>
```

### Role Management

```bash
# Check role
./tee-cli check-role admin <address>
./tee-cli check-role operator <address>

# Grant roles
./tee-cli add-admin <address>
./tee-cli add-operator <address>

# Revoke roles
./tee-cli revoke-admin <address>
./tee-cli revoke-operator <address>
```


## Examples

### Register a TEE

```bash
# Using .env file
./tee-cli register

# Or with inline env vars
ENCLAVE_HOST=13.59.207.188 ./tee-cli register
```

### Approve PCR Measurements

```bash
# From measurements.txt file
./tee-cli pcr-approve

# From environment variables
PCR0=abc123... PCR1=def456... PCR2=789... PCR_VERSION=v1.0.0 ./tee-cli pcr-approve

# With rotation (grace period for old PCR)
PREVIOUS_PCR=0xoldpcrhash GRACE_PERIOD=86400 ./tee-cli pcr-approve
```

### Using Private Key

```bash
# Set in .env file
TEE_PRIVATE_KEY=0xf621c7ef...

# Or inline
TEE_PRIVATE_KEY=0xf621c7ef... ./tee-cli add-operator 0xNewAddress
```

## Measurements File Format

The `measurements.txt` file should be JSON:

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

### "no unlocked accounts available"

Either:
1. Set `TEE_PRIVATE_KEY` in `.env`
2. Or ensure the node has unlocked accounts

### "failed to connect to RPC"

Check `TEE_RPC_URL` is correct and accessible.

### Transaction reverts

Check that your account has:
- Required role (admin/operator)
- Sufficient balance for gas

