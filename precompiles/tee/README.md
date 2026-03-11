# TEE Registry

TEE registration, lifecycle management, and settlement verification for AWS Nitro Enclaves.

## Architecture

```
┌─────────────────────────┐         ┌─────────────────────────┐
│   TEERegistry.sol       │         │   Precompile (0x900)    │
│   (Deployed Contract)   │────────►│   (Built into node)     │
│                         │         │                         │
│  • Storage & Logic      │         │  • verifyAttestation()  │
│  • Access Control       │         │  • verifyRSAPSS()       │
│  • PCR Management       │         └─────────────────────────┘
│  • Heartbeat            │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐         ┌─────────────────────────┐
│ TEEInferenceVerifier.sol│────────►│ InferenceSettlement     │
│                         │         │ Relay.sol               │
│  • verifySignature()    │         │                         │
│  • Timestamp bounds     │         │  • settleIndividual()   │
│  • RSA-PSS via 0x900    │         │  • batchSettle()        │
└─────────────────────────┘         │  • Merkle proof verify  │
                                    └─────────────────────────┘
```

## Workflow

### 1. Setup (Admin)

```solidity
// Add a TEE type (e.g., LLMProxy, Validator)
registry.addTEEType(0, "LLMProxy");

// Approve PCR measurements for that type
// PCRs identify the exact enclave code allowed to register
registry.approvePCR(pcrs, "v1.0.0", 0);
```

### 2. Registration (TEE Operator)

```solidity
// Register TEE with attestation document from AWS Nitro
// Precompile verifies attestation, extracts PCR hash, binds signing key + TLS cert
bytes32 teeId = registry.registerTEEWithAttestation(
    attestationDoc, signingKey, tlsCert, paymentAddr, endpoint, 0
);
// TEE is now enabled and in the active TEE list
```

### 3. Heartbeat (TEE)

```solidity
// TEE periodically proves liveness with RSA-PSS signed timestamp
// Also enforces PCR validity — if PCR was revoked, heartbeat fails
registry.heartbeat(teeId, timestamp, signature);
```

### 4. Inference Verification (Settlement)

```solidity
// TEEInferenceVerifier checks: TEE is active, timestamp in bounds, RSA-PSS signature valid
bool valid = verifier.verifySignature(teeId, inputHash, outputHash, timestamp, signature);

// InferenceSettlementRelay uses this for on-chain settlement
relay.settleIndividual(teeId, inputHash, outputHash, timestamp, ethAddress, blobId, signature);
```

### 5. PCR Revocation (Admin)

```solidity
// Immediate revocation — disables all TEEs running this PCR
registry.revokePCR(pcrHash, 0);
```

PCR revocation is immediate — all TEEs using the revoked PCR are disabled.
Additionally, `heartbeat()` will fail for TEEs with revoked PCRs.

### 6. TEE Lifecycle (Owner/Admin)

```solidity
// Owner or admin can disable/enable
registry.disableTEE(teeId);
registry.enableTEE(teeId);  // requires PCR to still be approved
```

## Key Functions

| Function | Who | Purpose |
|----------|-----|---------|
| `addTEEType()` | Admin | Add TEE category |
| `approvePCR()` | Admin | Approve enclave code hash for a TEE type |
| `revokePCR()` | Admin | Revoke PCR (immediate, disables affected TEEs) |
| `registerTEEWithAttestation()` | Operator | Register new TEE via attestation |
| `enableTEE()` | Owner/Admin | Re-enable TEE (checks PCR validity) |
| `disableTEE()` | Owner/Admin | Disable TEE |
| `heartbeat()` | Anyone (relayed) | Prove TEE liveness (checks PCR validity) |
| `verifySignature()` | TEEInferenceVerifier | Verify TEE inference signature |
| `settleIndividual()` | Settlement Relay | Settle with signature verification |
| `batchSettle()` | Settlement Relay | Emit batch settlement root |
| `getTEE()` | Anyone | Get TEE info |
| `getTLSCertificate()` | Anyone | Get TLS cert for HTTPS |
| `getEnabledTEEs()` | Anyone | List enabled TEE IDs by type |
| `getActiveTEEs()` | Anyone | List active TEE details by type |
| `getApprovedPCRs()` | Anyone | List all approved PCR hashes |

## Access Control

Uses OpenZeppelin AccessControl:

| Role | Can Do |
|------|--------|
| `DEFAULT_ADMIN_ROLE` | Manage PCRs, TEE types, certificates, roles, heartbeat config |
| `TEE_OPERATOR` | Register TEEs |
| `SETTLEMENT_RELAY_ROLE` | Submit settlements (on InferenceSettlementRelay) |

## CLI

The `tee-mgmt-cli` tool (`scripts/tee-mgmt-cli/`) provides commands for all admin and operator operations:

```bash
# PCR management
tee-mgmt-cli pcr approve --measurements-file measurements.json --version v1.0.0 --tee-type 0
tee-mgmt-cli pcr revoke <pcr_hash>
tee-mgmt-cli pcr list
tee-mgmt-cli pcr check <pcr_hash>
tee-mgmt-cli pcr compute --measurements-file measurements.json

# TEE management
tee-mgmt-cli tee list
tee-mgmt-cli tee info <tee_id>
tee-mgmt-cli tee register --host <enclave_host>
tee-mgmt-cli tee disable <tee_id>
tee-mgmt-cli tee enable <tee_id>
```

## Environment Variables

| Variable | Required by | Description |
|---|---|---|
| `TEE_ENCLAVE_HOST` | Integration tests & scripts | IP or hostname of the live enclave |
| `TEE_REGISTRY_ADDRESS` | `local_tee_workflow.go` | Optional — reuse an already-deployed TEERegistry contract |

## Testing

### Unit Tests
```bash
cd precompiles/tee
go test -v ./...
```

### Solidity Tests
```bash
cd tests/solidity/suites/tee
npm test
```

### Integration Tests
Require a live AWS Nitro enclave and a running local node.

```bash
# Timestamp freshness test (precompile level)
TEE_ENCLAVE_HOST=127.0.0.1 go test -tags=integration -v -run TestVerifyAttestation_TimestampFreshness ./precompiles/tee/...

# Full TEE registry workflow (deploy, register, local verification)
cd scripts/integration
TEE_ENCLAVE_HOST=127.0.0.1 go run local_tee_workflow.go

# Reuse existing deployed registry
TEE_REGISTRY_ADDRESS=0x... TEE_ENCLAVE_HOST=127.0.0.1 go run local_tee_workflow.go
```

> Integration tests are excluded from CI via `//go:build integration` tag and skip automatically if `TEE_ENCLAVE_HOST` is not set.

## TODO

### Planned Features
- [ ] TEE health monitoring hooks
- [ ] Batch TEE registration

### Integration Tasks
- [ ] Facilitator (x402): Call `verifySignature()` via FacilitatorSettlementRelay before payment
- [ ] Frontend/dashboard: Download and pin TLS certificates
- [ ] Monitoring: Track TEE active status
