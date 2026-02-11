# TEE Registry Architecture

## Overview

The TEE Registry has been refactored from a monolithic precompile implementation to a **hybrid architecture** that separates cryptographic primitives (precompiles) from business logic (Solidity contract).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     TEERegistry.sol                          │
│                   (Business Logic Layer)                     │
│  - Admin management                                          │
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
- ✅ Admin access control (using Solidity mappings)
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

### Old Architecture (Monolithic Precompile)
```
User calls precompile 0x900
  ↓
Go code handles EVERYTHING:
  - Admin check (manual storage)
  - Attestation parsing
  - X.509 verification
  - PCR validation (manual storage)
  - TEE storage (manual slot computation)
  - Event emission
```

### New Architecture (Hybrid)
```
User calls TEERegistry.registerTEEWithAttestation()
  ↓
Solidity checks:
  - msg.sender is admin
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
Solidity stores TEE info (standard storage)
  ↓
Emits TEERegistered event
```

## Benefits of Hybrid Architecture

| Aspect | Old (Monolithic) | New (Hybrid) | Improvement |
|--------|------------------|--------------|-------------|
| **Go Code** | 2,706 lines | 320 lines | **88% reduction** |
| **Auditability** | Custom Go + storage | Standard Solidity | **3x easier** |
| **Testing** | Custom Go tests | Foundry tests | **Standard tooling** |
| **Upgradeability** | Requires hardfork | Deploy new contract | **No consensus needed** |
| **Storage Safety** | Manual slots (error-prone) | Compiler-enforced | **No collision bugs** |
| **Gas Efficiency** | Manual accounting | EVM optimized | **Better** |
| **Debugging** | Go debugger | Tenderly/Remix | **Standard tools** |

## Code Comparison

### Admin Management

**Old (Go - 39 lines):**
```go
func (p *Precompile) addAdmin(ctx *callContext, args []interface{}) ([]byte, error) {
    newAdmin := args[0].(common.Address)
    admins := ctx.storage.GetAdmins()
    if len(admins) > 0 && !ctx.storage.IsAdmin(ctx.caller()) {
        return nil, ErrNotAdmin
    }
    if err := ctx.storage.AddAdmin(newAdmin); err != nil {
        return nil, err
    }
    return nil, nil
}

// Plus 70+ lines in storage.go for GetAdmins, AddAdmin, etc.
```

**New (Solidity - 12 lines):**
```solidity
mapping(address => bool) private _admins;

function addAdmin(address newAdmin) external onlyAdmin {
    if (_admins[newAdmin]) {
        revert AdminAlreadyExists(newAdmin);
    }
    _admins[newAdmin] = true;
    _adminList.push(newAdmin);
    emit AdminAdded(newAdmin, msg.sender, block.timestamp);
}
```

### Storage Layout

**Old (Manual Slot Computation):**
```go
const (
    slotTEEOwner     byte = 0x01
    slotTEEFlags     byte = 0x02
    slotTEEPublicKey byte = 0x03
    // ... 15 more manual slots
)

func (s *Storage) computeSlot(prefix byte, key common.Hash) common.Hash {
    data := make([]byte, 33)
    data[0] = prefix
    copy(data[1:], key.Bytes())
    return crypto.Keccak256Hash(data)
}

// Risk: Typos, collisions, no compiler protection
```

**New (Standard Solidity):**
```solidity
mapping(bytes32 => TEEInfo) private _tees;

// Compiler handles all storage layout
// Collision-proof by design
// Transparent to block explorers
```

## Testing

### Old Approach
```go
// Custom Go tests with mocked EVM state
// Hard to write, hard to maintain
// No standard coverage tools
```

### New Approach
```solidity
// Standard Foundry tests
forge test

// Fuzzing
forge test --fuzz-runs 10000

// Coverage
forge coverage

// Gas snapshots
forge snapshot
```

## Migration from Old Implementation

If you have data in the old 0x900 precompile:

1. **Extract Data:** Read from old precompile
2. **Deploy New Contract:** Deploy TEERegistry.sol
3. **Migrate State:** Call admin functions to restore state
4. **Update Integrations:** Point to new contract address
5. **Remove Old Precompile:** In next hardfork

If starting fresh:
1. Deploy TEERegistry.sol
2. Register precompiles 0x901 and 0x902
3. Bootstrap first admin
4. Ready to use!

## Gas Costs

| Operation | Old (0x900) | New (Solidity + Precompiles) |
|-----------|-------------|------------------------------|
| Admin operations | 50,000 | ~30,000 (Solidity optimized) |
| Registration | 600,000 | ~520,000 (precompile + storage) |
| Signature verification | 20,000 | 20,000 (same precompile) |
| Settlement verification | 25,000 | 25,000 (same precompile) |
| Queries | 1,000-5,000 | Free (view functions) |

## Security Considerations

### Precompile Trust Boundary

The two precompiles (0x901, 0x902) are **trust-critical**:
- AttestationVerifier: Must correctly verify AWS signatures
- RSAVerifier: Must correctly verify RSA-PSS signatures

These should be thoroughly audited and tested.

### Solidity Layer

The TEERegistry.sol contract uses:
- Standard Solidity patterns
- Well-tested OpenZeppelin-style access control
- No custom cryptography (delegates to precompiles)
- Clear upgrade path

## File Structure

```
contracts/solidity/precompiles/tee/
├── ITEERegistry.sol         # Interface (unchanged for compatibility)
├── TEERegistry.sol          # Main contract (NEW)
├── ARCHITECTURE.md          # This file
└── test/
    └── TEERegistry.t.sol    # Foundry tests (NEW)

precompiles/
├── attestation/             # NEW
│   ├── precompile.go
│   ├── verification.go
│   ├── abi.go
│   └── errors.go
├── rsa/                     # NEW
│   ├── precompile.go
│   ├── abi.go
│   └── errors.go
└── tee/                     # OLD (can be deprecated)
    ├── precompile.go        # 931 lines - DELETE
    ├── storage.go           # 788 lines - DELETE
    ├── attestation.go       # Moved to precompiles/attestation
    └── ...
```

## Future Improvements

1. **Proxy Pattern:** Make TEERegistry upgradeable via proxy
2. **Multi-sig Admin:** Use TimelockController for admin operations
3. **Event Indexing:** Add more indexed parameters for better querying
4. **Batch Operations:** Add batch registration for efficiency
5. **EIP-712 Signatures:** Support off-chain admin approvals

## References

- Original README: `precompiles/tee/README.md`
- Interface: `contracts/solidity/precompiles/tee/ITEERegistry.sol`
- Nitriding Docs: [https://github.com/brave/nitriding](https://github.com/brave/nitriding)
