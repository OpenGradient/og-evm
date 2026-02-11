# TEE Registry Architecture

## Overview

The TEE Registry uses a **hybrid architecture** that separates cryptographic primitives (precompiles) from business logic (Solidity contract).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     TEERegistry.sol                          │
│                   (Business Logic Layer)                     │
│  - Role-based access control (Admin + Operator)              │
│  - TEE type registry                                         │
│  - PCR approval tracking                                     │
│  - TEE storage and lifecycle                                 │
│  - Settlement replay protection                              │
│  - All query functions                                       │
└─────────────────────┬───────────────────┬───────────────────┘
                      │                   │
                      ▼                   ▼
        ┌─────────────────────┐ ┌─────────────────────┐
        │ AttestationVerifier │ │    RSAVerifier      │
        │   Precompile 0x901  │ │  Precompile 0x902   │
        │                     │ │                     │
        │ - AWS Nitro CBOR    │ │ - RSA-PSS verify    │
        │ - X.509 chain       │ │ - SHA-256 hashing   │
        │ - Nitriding binding │ │                     │
        └─────────────────────┘ └─────────────────────┘
```

## Component Breakdown

### TEERegistry.sol
**Location:** `contracts/solidity/precompiles/tee/TEERegistry.sol`

**Purpose:** Main contract implementing all business logic

**Responsibilities:**
- ✅ Role-based access control (OpenZeppelin AccessControl)
  - TEE_ADMIN_ROLE: Protocol admins (manage types, PCRs, certs, override TEE lifecycle)
  - TEE_OPERATOR_ROLE: TEE operators (register and manage own TEEs)
- ✅ TEE type management
- ✅ PCR registry with versioning and grace periods
- ✅ TEE registration workflow
- ✅ Settlement verification and replay protection
- ✅ All query operations (view functions)

**Storage:**
- Standard Solidity mappings (transparent, auditable)
- No custom slot computation
- Compiler-enforced collision safety

**Code Size:** ~550 lines of Solidity

### AttestationVerifier Precompile (0x901)
**Location:** `precompiles/attestation/`

**Purpose:** Verify AWS Nitro attestation documents

**Responsibilities:**
- Parse CBOR-encoded attestation documents
- Verify X.509 certificate chains
- Extract PCR measurements
- Verify Nitriding dual-key binding (TLS cert + signing key)
- Return validated PCR hash

**Code Size:** ~200 lines of Go

**Interface:**
```solidity
function verifyAttestation(
    bytes calldata attestationDocument,
    bytes calldata signingPublicKey,
    bytes calldata tlsCertificate,
    bytes calldata rootCertificate
) external view returns (bool valid, bytes32 pcrHash);
```

### RSAVerifier Precompile (0x902)
**Location:** `precompiles/rsa/`

**Purpose:** Verify RSA-PSS signatures

**Responsibilities:**
- Parse RSA public keys (DER format)
- Verify RSA-PSS signatures with SHA-256
- Return validation result

**Code Size:** ~120 lines of Go

**Interface:**
```solidity
function verifyRSAPSS(
    bytes calldata publicKeyDER,
    bytes32 messageHash,
    bytes calldata signature
) external view returns (bool valid);
```

## Registration Flow

```
Operator or Admin calls TEERegistry.registerTEEWithAttestation()
  ↓
Solidity checks:
  - msg.sender has TEE_OPERATOR_ROLE or TEE_ADMIN_ROLE
  - TEE type is valid
  ↓
Calls AttestationVerifier precompile (0x901)
  ↓
Precompile returns: (valid, pcrHash)
  ↓
Solidity checks:
  - PCR is approved (storage lookup)
  - TEE doesn't already exist
  ↓
Solidity stores TEE info with owner = msg.sender
  ↓
Emits TEERegistered event
```

## Why This Architecture?

### Separation of Concerns

**Precompiles (Go):** Only handle cryptographic operations that can't be done efficiently in EVM
- AWS Nitro attestation parsing (CBOR format)
- X.509 certificate chain verification
- RSA-PSS signature verification

**Smart Contract (Solidity):** All business logic
- Admin access control
- Registry management
- State storage
- Query operations

### Benefits

| Aspect | Value |
|--------|-------|
| **Auditability** | Standard Solidity patterns, tools like Slither/Foundry work |
| **Testing** | Foundry test suite with fuzzing and coverage |
| **Upgradeability** | Deploy new contract, no hardfork needed |
| **Storage Safety** | Compiler-enforced, no slot collision bugs |
| **Gas Efficiency** | EVM-optimized for standard operations |
| **Debugging** | Tenderly, Remix, standard block explorers |
| **Maintainability** | Standard patterns, easy to understand |

## Permission Model

The contract uses OpenZeppelin's AccessControl for role-based permissions:

### TEE_ADMIN_ROLE (Protocol Administrators)
**Can do:**
- ✅ Manage TEE types (`addTEEType`, `deactivateTEEType`)
- ✅ Approve/revoke PCR measurements (`approvePCR`, `revokePCR`)
- ✅ Set AWS root certificate (`setAWSRootCertificate`)
- ✅ Register TEEs (becomes owner)
- ✅ Deactivate/activate ANY TEE (even if not owner)

**Use case:** Protocol-level control and emergency TEE management

### TEE_OPERATOR_ROLE (TEE Operators)
**Can do:**
- ✅ Register TEEs (becomes owner)
- ✅ Deactivate/activate only TEEs they own

**Cannot do:**
- ❌ Manage TEE types or PCRs
- ❌ Override other operators' TEEs

**Use case:** Enclave operators who run TEE services

### Ownership Model
- When someone registers a TEE, they become the `owner` (stored in TEEInfo)
- Owners can manage their own TEE lifecycle (activate/deactivate)
- Admins can override and manage any TEE regardless of ownership
- TEE metadata is immutable - must deactivate + re-register to change

## Storage Layout

The contract uses standard Solidity storage patterns:

```solidity
// Access control (OpenZeppelin AccessControl)
// - TEE_ADMIN_ROLE: Protocol administrators
// - TEE_OPERATOR_ROLE: TEE operators
// Managed by AccessControl base contract

// TEE type registry
mapping(uint8 => TEETypeInfo) private _teeTypes;
uint8[] private _teeTypeList;

// PCR approval registry
mapping(bytes32 => ApprovedPCR) private _approvedPCRs;
bytes32[] private _pcrList;

// TEE registry
mapping(bytes32 => TEEInfo) private _tees;
bytes32[] private _activeTEEList;
mapping(bytes32 => uint256) private _activeTEEIndex;

// TEE indexes
mapping(uint8 => bytes32[]) private _teesByType;

// Settlement replay protection
mapping(bytes32 => bool) private _usedSettlements;

// AWS root certificate
bytes private _awsRootCertificate;
```

All storage is managed by the Solidity compiler - no manual slot computation needed.

## Testing

The contract includes a comprehensive Foundry test suite:

```bash
# Run all tests
forge test

# Run with verbosity
forge test -vv

# Run specific test
forge test --match-test test_AddFirstAdmin

# Run fuzzing
forge test --fuzz-runs 10000

# Generate coverage report
forge coverage

# Generate gas snapshot
forge snapshot
```

**Test Coverage:**
- Role-based access control (admin and operator permissions)
- TEE type management (add, deactivate, query)
- PCR registry (approve, revoke, grace periods)
- TEE registration and ownership
- Certificate management
- Settlement verification and replay protection
- Utility functions (hash computation, ID generation)

## Security Considerations

### Precompile Trust Boundary

The two precompiles (0x901, 0x902) are **trust-critical**:
- AttestationVerifier: Must correctly verify AWS signatures
- RSAVerifier: Must correctly verify RSA-PSS signatures

These should be thoroughly audited and tested.

### Solidity Layer

The TEERegistry.sol contract uses:
- Standard Solidity patterns
- Well-tested access control patterns
- No custom cryptography (delegates to precompiles)
- Clear upgrade path via proxy pattern

### Best Practices

1. **Always verify both keys** during registration
2. **Download TLS cert from blockchain** before connecting to endpoint
3. **Verify settlement signatures** before processing payments
4. **Keep private keys inside enclave** - never export
5. **Use PCR grace periods** for smooth upgrades
6. **Monitor TEE active status** before routing requests

## Gas Costs

| Operation | Estimated Gas |
|-----------|---------------|
| Admin operations | ~30,000 |
| Registration with attestation | ~520,000 |
| Signature verification (view) | 20,000 |
| Settlement verification | 25,000 |
| Activate/Deactivate | ~10,000 |
| PCR management | ~50,000 |
| TEE type management | ~30,000 |
| Certificate management | ~100,000 |
| Query functions | Free (view) |

## File Structure

```
contracts/solidity/precompiles/tee/
├── ITEERegistry.sol         # Interface definition
├── TEERegistry.sol          # Main contract implementation
├── ARCHITECTURE.md          # This file
└── test/
    └── TEERegistry.t.sol    # Foundry tests

precompiles/
├── attestation/             # Attestation verification precompile
│   ├── precompile.go
│   ├── verification.go
│   ├── abi.go
│   └── errors.go
└── rsa/                     # RSA signature verification precompile
    ├── precompile.go
    ├── abi.go
    └── errors.go
```

## Deployment

### 1. Build Precompiles

```bash
go build ./precompiles/attestation
go build ./precompiles/rsa
```

The precompiles automatically register at addresses 0x901 and 0x902.

### 2. Deploy Contract

```bash
cd contracts/solidity/precompiles/tee

forge create TEERegistry \
  --rpc-url $RPC_URL \
  --private-key $PRIVATE_KEY
```

### 3. Bootstrap Registry

```bash
REGISTRY_ADDRESS="0x..."  # Your deployed contract
TEE_ADMIN_ROLE="0x..."    # keccak256("TEE_ADMIN_ROLE")
TEE_OPERATOR_ROLE="0x..." # keccak256("TEE_OPERATOR_ROLE")

# Grant admin role to protocol administrator
cast send $REGISTRY_ADDRESS \
  "grantRole(bytes32,address)" \
  $TEE_ADMIN_ROLE $ADMIN_ADDRESS \
  --rpc-url $RPC_URL \
  --private-key $PRIVATE_KEY

# Grant operator role to TEE operator
cast send $REGISTRY_ADDRESS \
  "grantRole(bytes32,address)" \
  $TEE_OPERATOR_ROLE $OPERATOR_ADDRESS \
  --rpc-url $RPC_URL \
  --private-key $PRIVATE_KEY

# Add TEE types (admin only)
cast send $REGISTRY_ADDRESS \
  "addTEEType(uint8,string)" \
  0 "LLMProxy" \
  --rpc-url $RPC_URL \
  --private-key $PRIVATE_KEY

# Approve PCRs (admin only)
cast send $REGISTRY_ADDRESS \
  "approvePCR((bytes,bytes,bytes),string,bytes32,uint256)" \
  "($PCR0,$PCR1,$PCR2)" "v1.0.0" "0x0" 0 \
  --rpc-url $RPC_URL \
  --private-key $PRIVATE_KEY
```

## Future Improvements

1. **Proxy Pattern:** Make TEERegistry upgradeable via TransparentUpgradeableProxy
2. **Multi-sig Admin:** Use TimelockController for admin operations
3. **Event Indexing:** Add more indexed parameters for better querying
4. **Batch Operations:** Add batch registration for efficiency
5. **EIP-712 Signatures:** Support off-chain admin approvals
6. **Health Monitoring:** TEE heartbeat/health check system

## Integration Example

```javascript
import { ethers } from "ethers";
import ITEERegistryABI from "./ITEERegistry.json";

const REGISTRY_ADDRESS = "0x...";

const registry = new ethers.Contract(
  REGISTRY_ADDRESS,
  ITEERegistryABI,
  signer
);

// Register a TEE
const tx = await registry.registerTEEWithAttestation(
  attestationDoc,
  signingKey,
  tlsCert,
  paymentAddress,
  "https://tee.example.com",
  0  // TEE type
);

await tx.wait();
console.log("TEE registered!");

// Verify a settlement
const valid = await registry.verifySettlement(
  teeId,
  inputHash,
  outputHash,
  timestamp,
  signature
);

if (valid) {
  // Process payment
}
```

## References

- Interface: `contracts/solidity/precompiles/tee/ITEERegistry.sol`
- README: `precompiles/tee/README.md`
- Nitriding Docs: [https://github.com/brave/nitriding](https://github.com/brave/nitriding)
- AWS Nitro Enclaves: [https://aws.amazon.com/ec2/nitro/nitro-enclaves/](https://aws.amazon.com/ec2/nitro/nitro-enclaves/)
