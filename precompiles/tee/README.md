# TEE Registry Precompile

EVM precompile at `0x0000000000000000000000000000000000000900` for TEE registration and signature verification.

## Overview

Enables:
- Registration of TEE nodes with AWS Nitro attestation
- On-chain signature verification for inference settlement



## Architecture
```
┌──────────┐     ┌───────────────┐     ┌─────────────┐     ┌────────────┐
│ Operator │────►│  LLM Server   │────►│ Facilitator │────►│ Blockchain │
└──────────┘     │  (TEE)        │     │ (x402)      │     │ (0x900)    │
                 └───────────────┘     └─────────────┘     └────────────┘
                        │                    │
                        │  Registration      │  Verification
                        └────────────────────┴─────────────────────────►
```
This architecture is under design. 
Where to deploy the llms and other design decision may change
## Workflows

### Registration (LLM Server → Blockchain)
```
Operator                LLM Server              Blockchain
   │                        │                       │
   │ POST /admin/register   │                       │
   │───────────────────────►│                       │
   │                        │                       │
   │                        │        │
   │                        │                       │
   │                        │ registerTEEWithAttestation()
   │                        │──────────────────────►│
   │                        │                       │ Verify AWS signature
   │                        │                       │ Validate PCRs
   │                        │                       │ Store TEE info
   │                        │◄──────────────────────│
   │                        │      teeId            │
   │◄───────────────────────│                       │
   │  {teeId, txHash}       │                       │
```

* The operator should provide attestaion from the Nitro
* New admin/register endpoint should be implemented in the llm Server
* where to store the teeID and 
**LLM Server calls:**
```python
tee_id = contract.functions.registerTEEWithAttestation(
    attestation_bytes,         # Raw CBOR from AWS Nitro
    (pcr0_32, pcr1_32, pcr2_32)  # Expected PCRs (32 bytes each)
).transact()
```

### Verification 

![Verification Flow](./docs/verification_flow.png)
```
┌──────┐    ┌────────────┐    ┌──────────┐    ┌─────────────┐    ┌────────────┐
│ User │───►│ LLM Server │◄──►│ TEE Node │    │ Facilitator │───►│ Blockchain │
└──────┘    └────────────┘    └──────────┘    └─────────────┘    └────────────┘
   │              │                 │               │                  │
   │ 1. Choose    │                 │               │                  │
   │    inference │                 │               │                  │
   │    details   │                 │               │                  │
   │              │                 │               │                  │
   │ 2. Send      │                 │               │                  │
   │    payment   │                 │               │                  │
   │    signature │                 │               │                  │
   │─────────────►│                 │               │                  │
   │              │ 3. Send         │               │                  │
   │              │    inference    │               │                  │
   │              │    request      │               │                  │
   │              │────────────────►│               │                  │
   │              │                 │               │                  │
   │              │◄────────────────│               │                  │
   │              │    Signed       │               │                  │
   │              │    response     │               │                  │
   │              │                 │               │                  │
   │              │ 4. Send settlement data         │                  │
   │              │    (when outputs ready)         │                  │
   │              │────────────────────────────────►│                  │
   │              │                 │               │                  │
   │              │                 │               │ 5. Verify        │
   │              │                 │               │    signature     │
   │              │                 │               │─────────────────►│
   │              │                 │               │                  │
   │              │                 │               │◄─────────────────│
   │              │                 │               │   true/false     │
```

### Steps

1. **User chooses inference details** - Model, prompt, parameters
2. **User sends payment signature** - x402 payment authorization
3. **LLM Server sends to TEE Node** - Inference request
4. **LLM Server sends to Facilitator** - Settlement data when outputs ready
5. **Facilitator verifies on blockchain** - Calls `verifySettlement()`

### Settlement Options (Under Discussion)

| Option | Description | Trade-off |
|--------|-------------|-----------|
| **Op1** | Keep payment, decrease node score if invalid | Fast UX, eventual consistency |
| **Op2** | Wait until validation before releasing payment | Slower UX, immediate consistency |



## Interface

### Registration
```solidity
// Trustless registration (on-chain attestation verification)
function registerTEEWithAttestation(
    bytes calldata attestationDocument,
    PCRMeasurements calldata expectedPcrs
) external returns (bytes32 teeId);

// Trusted registration (off-chain verification)
function registerTEE(
    bytes calldata publicKey,
    PCRMeasurements calldata pcrs
) external returns (bytes32 teeId);
```

### Verification
```solidity
// View function - check validity without gas cost
function verifySignature(
    bytes32 teeId,
    bytes32 inputHash,
    bytes32 outputHash,
    uint256 timestamp,
    bytes calldata signature
) external view returns (bool valid);

// State-changing - for settlement (emits event)
function verifySettlement(
    bytes32 teeId,
    bytes32 inputHash,
    bytes32 outputHash,
    uint256 timestamp,
    bytes calldata signature
) external returns (bool valid);
```

### Queries
```solidity
function getTEE(bytes32 teeId) external view returns (TEEInfo memory);
function isActive(bytes32 teeId) external view returns (bool);
function getPublicKey(bytes32 teeId) external view returns (bytes memory);
```

## Data Structures
```solidity
struct PCRMeasurements {
    bytes32 pcr0;  // Enclave image hash (first 32 bytes of SHA-384)
    bytes32 pcr1;  // Kernel hash
    bytes32 pcr2;  // Application hash
}

struct TEEInfo {
    bytes32 teeId;
    address owner;
    bytes publicKey;
    PCRMeasurements pcrs;
    bool active;
    uint256 registeredAt;
    uint256 lastUpdatedAt;
}
```

## Message Signing

TEE Node signs inference results:
```
inputHash   = keccak256(request)    // 32 bytes
outputHash  = keccak256(response)   // 32 bytes
timestamp   = unix seconds          // uint256

messageHash = keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
signature   = RSA_PSS_SHA256(messageHash)
```

## Usage

### LLM Server (Registration)
```python
tee_id = contract.functions.registerTEEWithAttestation(
    attestation_bytes,
    (pcr0_32, pcr1_32, pcr2_32)
).transact()
```

### Facilitator (Verification)
```typescript
const valid = await contract.verifySettlement(
    teeId,
    inputHash,
    outputHash,
    timestamp,
    signature
);
```

## Testing
```bash
cd precompiles/tee
go test -v ./...
```

## Files
```
precompiles/tee/
├── precompile.go       # Main entry point
├── types.go            # Data structures
├── storage.go          # State management
├── attestation.go      # AWS Nitro verification
├── errors.go           # Error definitions
├── abi.go              # ABI definition
└── precompile_test.go  # Tests
```

## TODO: Integration Tasks

### Blockchain 

- [ ] Add event emission (`TEERegistered`, `SettlementVerified`)
- [ ] Unit tests matching project patterns
- [ ] complete and test `registerTEEWithAttestation` with real AWS attestation
- [ ] Solidity interface file for external contracts
- [ ] Gas optimization review

### LLM Server

- [ ] Implement `/admin/register` endpoint
- [ ] Store TEE ID after registration

### Facilitator (x402)

- [ ] Call `verifySignature()` or `verifySettlement()`
- [ ] Handle verification result (payment release / score decrease) Not yet decided
- [ ] Decide settlement timing (Op1 vs Op2)


### Documentation

- [ ] Update the design document with the new elements
- [ ] Security audit checklist
