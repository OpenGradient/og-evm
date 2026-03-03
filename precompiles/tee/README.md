# TEE Registry

TEE registration and settlement verification for AWS Nitro Enclaves.

## Architecture

```
┌─────────────────────────┐         ┌─────────────────────────┐
│   TEERegistry.sol       │         │   Precompile (0x900)    │
│   (Deployed Contract)   │────────►│   (Built into node)     │
│                         │         │                         │
│  • Storage & Logic      │         │  • verifyAttestation()  │
│  • Access Control       │         │  • verifyRSAPSS()       │
└─────────────────────────┘         └─────────────────────────┘
```

## Quick Start

```solidity
TEERegistry registry = TEERegistry(registryAddress);

// 1. Admin: Setup TEE type and PCRs
registry.addTEEType(0, "LLMProxy");
registry.approvePCR(pcrs, "v1.0.0", bytes32(0), 0);

// 2. Operator: Register TEE
bytes32 teeId = registry.registerTEEWithAttestation(
    attestationDoc, signingKey, tlsCert, paymentAddr, endpoint, 0
);

// 3. Verifier: Check TEE inference signature
bool valid = verifier.verifySignature(teeId, inputHash, outputHash, timestamp, signature);

// 4. Client: Get TLS cert for HTTPS verification
bytes memory cert = registry.getTLSCertificate(teeId);
```

## Key Functions

| Function | Who | Purpose |
|----------|-----|---------|
| `addTEEType()` | Admin | Add TEE category |
| `approvePCR()` | Admin | Approve enclave code hash |
| `registerTEEWithAttestation()` | Operator | Register new TEE |
| `verifySignature()` | TEEInferenceVerifier | Verify TEE inference signature |
| `getTEE()` | Anyone | Get TEE info |
| `getTLSCertificate()` | Anyone | Get TLS cert for HTTPS |
| `isActive()` | Anyone | Check TEE status |

## Access Control

Uses OpenZeppelin AccessControl:

| Role | Can Do |
|------|--------|
| `DEFAULT_ADMIN_ROLE` | Manage PCRs, TEE types, roles |
| `TEE_OPERATOR` | Register TEEs |

## Environment Variables

| Variable | Required by | Description |
|---|---|---|
| `TEE_ENCLAVE_HOST` | Integration tests & scripts | IP or hostname of the live enclave  |
| `TEE_REGISTRY_ADDRESS` | `local_tee_workflow.go` | Optional — reuse an already-deployed TEERegistry contract |

## Testing

### Unit Tests
```bash
cd precompiles/tee
go test -v ./...
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
- [ ] LLM Server: 
- [ ] Facilitator (x402): Call `verifySignature()` via FacilitatorSettlementRelay before payment
- [ ] Frontend/dashboard: Download and pin TLS certificates
- [ ] Monitoring: Track TEE active status


