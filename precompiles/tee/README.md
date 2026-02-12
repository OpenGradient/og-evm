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

// 3. Facilitator: Verify settlement
bool valid = registry.verifySettlement(teeId, inputHash, outputHash, timestamp, signature);

// 4. Client: Get TLS cert for HTTPS verification
bytes memory cert = registry.getTLSCertificate(teeId);
```

## Key Functions

| Function | Who | Purpose |
|----------|-----|---------|
| `addTEEType()` | Admin | Add TEE category |
| `approvePCR()` | Admin | Approve enclave code hash |
| `registerTEEWithAttestation()` | Operator | Register new TEE |
| `verifySettlement()` | Facilitator/toBeupdated | Verify & record settlement |
| `getTEE()` | Anyone | Get TEE info |
| `getTLSCertificate()` | Anyone | Get TLS cert for HTTPS |
| `isActive()` | Anyone | Check TEE status |

## Access Control

Uses OpenZeppelin AccessControl:

| Role | Can Do |
|------|--------|
| `DEFAULT_ADMIN_ROLE` | Manage PCRs, TEE types, roles |
| `TEE_OPERATOR` | Register TEEs |

## Testing

```bash
# Unit tests
cd precompiles/tee
go test -v

# Integration test
cd scripts/integration
go run test_tee_workflow.go
```

## Files

```
precompiles/tee/
├── precompile.go           # Precompile implementation
├── precompile_test.go      # Unit tests
├── nitro_attestation.go    # AWS Nitro verification
├── abi.json                # Precompile ABI
└── testdata/
    └── attestation_doc.bin # Test attestation

contracts/solidity/
├── TEERegistry.sol         # Main contract
└── precompiles/tee/
    └── ITEEVerifier.sol    # Precompile interface
```

## TODO

### Planned Features
- [ ] TEE health monitoring hooks 
- [ ] Batch TEE registration

### Integration Tasks
- [ ] LLM Server: 
- [ ] Facilitator (x402): Call `verifySettlement()` before payment
- [ ] Frontend/dashboard: Download and pin TLS certificates
- [ ] Monitoring: Track TEE active status