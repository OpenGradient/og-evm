# TEE Registry Precompile

EVM precompile at `0x0000000000000000000000000000000000000900` for TEE registration and signature verification.

## Version

- Nitriding Dual-Key Support

## Overview

The TEE Registry precompile enables secure registration and verification of Trusted Execution Environments (TEEs) running on AWS Nitro Enclaves. It provides:

- **Admin-controlled TEE registration** with AWS Nitro attestation verification
- **Nitriding framework support** for TLS termination inside enclaves
- **Dual-key cryptographic binding** (TLS + Signing keys)
- **Global PCR registry** with versioning and upgrade grace periods
- **On-chain signature verification** for inference settlement
- **TEE type management** for different service categories

## Architecture
```
┌──────────┐     ┌───────────────┐     ┌─────────────┐     ┌────────────┐
│ Operator │────►│  LLM Server   │────►│ Facilitator │────►│ Blockchain │
└──────────┘     │  (TEE)        │     │ (x402)      │     │ (0x900)    │
                 │  [Nitriding]  │     └─────────────┘     └────────────┘
                 └───────────────┘
                        │
                        ├─ TLS Cert (HTTPS encryption)
                        └─ Signing Key (Settlement signatures)
```

## Nitriding Dual-Key Architecture

### Why Two Keys?

We generate TWO separate cryptographic keys inside the AWS Nitro Enclave:

#### 1. TLS Certificate (ECDSA/RSA)
**Purpose:** HTTPS termination and encrypted communication

- **Used by:** Client browsers/apps connecting to the enclave
- **Stored on-chain:** Users download to verify certificate before connecting

#### 2. Signing Key (RSA 2048+)
**Purpose:** Settlement signature generation

- **Used by:** TEE to cryptographically sign inference outputs
- **Enables:** Proof of computation for payment settlement
- **Stored on-chain:** Blockchain verifies signatures in `verifySettlement()`


### Why Both Keys Are Essential

| Scenario | TLS Only | Signing Only | Both Keys (Nitriding) |
|----------|----------|--------------|----------------------|
| HTTPS Privacy | ✅ Protected | ❌ Parent can intercept | ✅ Protected |
| Settlement Integrity | ❌ No proof | ✅ Cryptographically proven | ✅ Cryptographically proven |
| End-to-End Security | ❌ Incomplete | ❌ Incomplete | ✅ Complete |

**Without TLS binding:**
- Parent EC2 instance intercepts HTTPS traffic
- User connects to fake endpoint but gets valid settlements
- Privacy breach: inputs/outputs visible to parent

**Without Signing key binding:**
- Settlement signatures could be forged
- No cryptographic proof computation happened in enclave
- Payment fraud risk

**With both:**
- ✅ HTTPS terminates inside enclave (privacy)
- ✅ Settlements cryptographically proven (integrity)
- ✅ Full end-to-end security guarantee

### User Data Format (68 bytes)

Nitriding encodes two SHA256 hashes in multihash format:

```
Offset  Size  Description
──────────────────────────────────────────────────
[0:2]    2    Multihash prefix: 0x1220 (SHA256, 32 bytes)
[2:34]  32    SHA256(TLS Certificate DER)  ← Full certificate
[34:36]  2    Multihash prefix: 0x1220
[36:68] 32    SHA256(Signing Public Key DER)
```

**Important:** The TLS hash is of the ENTIRE certificate DER bytes, not just the public key.

### Registration Flow
```
┌─────────────────────────────────────────────────────────────────┐
│ 1. Enclave Setup (Inside AWS Nitro Enclave)                     │
├─────────────────────────────────────────────────────────────────┤
│  • Generate TLS keypair (for HTTPS)                             │
│  • Generate Signing keypair (for settlements)                   │
│  • Compute tlsHash = SHA256(TLS certificate DER)                │
│  • Compute appHash = SHA256(signing public key DER)             │
│  • Request attestation with user_data = 0x1220 + tlsHash +      │
│                                          0x1220 + appHash        │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ 2. Admin Registration (On Blockchain)                           │
├─────────────────────────────────────────────────────────────────┤
│  registerTEEWithAttestation(                                    │
│    attestationDoc,      // Contains user_data                   │
│    signingKeyDER,       // Actual signing public key            │
│    tlsCertDER,          // Actual TLS certificate               │
│    paymentAddress,                                              │
│    endpoint,                                                    │
│    teeType                                                      │
│  )                                                              │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ 3. Precompile Verification                                      │
├─────────────────────────────────────────────────────────────────┤
│  ✓ Verify AWS signature chain (attestation authenticity)        │
│  ✓ Parse user_data (68 bytes → extract 2 hashes)               │
│  ✓ Verify SHA256(tlsCertDER) == user_data[2:34]                │
│  ✓ Verify SHA256(signingKeyDER) == user_data[36:68]            │
│  ✓ Verify PCRs match approved list (code integrity)            │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ 4. TEE Registered                                               │
├─────────────────────────────────────────────────────────────────┤
│  • teeId = keccak256(signingKeyDER)                             │
│  • Stores: signingKey (for verifySettlement)                    │
│  •         tlsCert (users download for HTTPS)                   │
│  •         endpoint, PCRs, metadata                             │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Start

```solidity
// Import interface
import "./ITEERegistry.sol";
```

### Basic Setup
```solidity
ITEERegistry tee = ITEERegistry(0x0000000000000000000000000000000000000900);

// 1. Bootstrap first admin
tee.addAdmin(msg.sender);

// 2. Add TEE type
tee.addTEEType(0, "LLMProxy");

// 3. Approve PCR configuration
PCRMeasurements memory pcrs = PCRMeasurements(pcr0, pcr1, pcr2);
tee.approvePCR(pcrs, "v1.0.0", bytes32(0), 0);

// 4. Register TEE with dual keys
bytes32 teeId = tee.registerTEEWithAttestation(
    attestationDocument,    // Raw CBOR from enclave
    signingPublicKeyDER,    // RSA key for settlements
    tlsCertificateDER,      // TLS cert for HTTPS
    paymentAddress,
    "https://tee.example.com",
    0 // LLMProxy type
);
```

### Usage Examples

#### Verify Settlement Signature
```solidity
// Facilitator verifies settlement before payment
bool valid = tee.verifySettlement(
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

#### Get TLS Certificate for HTTPS
```solidity
// User downloads TLS cert to verify HTTPS connection
bytes memory tlsCert = tee.getTLSCertificate(teeId);

// User's app uses this cert to verify the enclave endpoint
// No certificate warning, proven to be from enclave
```

## Interface Summary

### Admin Functions

| Function | Description |
|----------|-------------|
| `addAdmin(address)` | Add new admin (first caller bootstraps) |
| `removeAdmin(address)` | Remove admin (cannot remove last) |
| `isAdmin(address) → bool` | Check if address is admin |
| `getAdmins() → address[]` | Get all active admins |

### TEE Type Functions

| Function | Description |
|----------|-------------|
| `addTEEType(uint8, string)` | Add new TEE type |
| `deactivateTEEType(uint8)` | Deactivate type (no new registrations) |
| `isValidTEEType(uint8) → bool` | Check if type is valid |
| `getTEETypes() → TEETypeInfo[]` | Get all types |

### PCR Registry Functions

| Function | Description |
|----------|-------------|
| `approvePCR(pcrs, version, prevHash, grace)` | Approve PCR with optional grace period |
| `revokePCR(bytes32)` | Revoke PCR approval |
| `isPCRApproved(pcrs) → bool` | Check if PCR is approved |
| `getActivePCRs() → bytes32[]` | Get all active PCR hashes |
| `getPCRDetails(bytes32) → ApprovedPCR` | Get PCR details |
| `computePCRHash(pcrs) → bytes32` | Compute hash for PCRs |

### Certificate Functions

| Function | Description |
|----------|-------------|
| `setAWSRootCertificate(bytes)` | Set custom AWS root cert (admin) |
| `getAWSRootCertificateHash() → bytes32` | Get current root cert hash |

### Registration Functions

| Function | Description |
|----------|-------------|
| `registerTEEWithAttestation(attestation, signingKey, tlsCert, ...)` | Register TEE with dual-key verification |
| `deactivateTEE(bytes32)` | Deactivate TEE (owner or admin) |
| `activateTEE(bytes32)` | Reactivate TEE (owner or admin) |

### Verification Functions

| Function | Description |
|----------|-------------|
| `verifySignature(request) → bool` | Verify signature (view, no replay protection) |
| `verifySettlement(...) → bool` | Verify and record settlement (replay protected) |

### Query Functions

| Function | Description |
|----------|-------------|
| `getTEE(bytes32) → TEEInfo` | Get complete TEE details (includes TLS cert) |
| `getActiveTEEs() → bytes32[]` | Get all active TEE IDs |
| `getTEEsByType(uint8) → bytes32[]` | Get TEEs by type |
| `getTEEsByOwner(address) → bytes32[]` | Get TEEs by owner |
| `getPublicKey(bytes32) → bytes` | Get signing key (for settlements) |
| `getTLSCertificate(bytes32) → bytes` | Get TLS certificate (for HTTPS) |
| `isActive(bytes32) → bool` | Check if TEE is active |

### Utility Functions

| Function | Description |
|----------|-------------|
| `computeTEEId(bytes) → bytes32` | Compute TEE ID from public key |
| `computeMessageHash(bytes32, bytes32, uint256) → bytes32` | Compute message hash for signing |

## Data Structures

### TEEInfo
```solidity
struct TEEInfo {
    bytes32 teeId;           // keccak256(signingPublicKey)
    address owner;           // TEE owner (registrar)
    address paymentAddress;  // Payment recipient
    string endpoint;         // HTTPS endpoint
    bytes publicKey;         // RSA signing key (for settlements)
    bytes tlsCertificate;    // TLS certificate (for HTTPS)
    bytes32 pcrHash;         // Reference to approved PCR
    uint8 teeType;           // TEE type ID
    bool active;             // Active status
    uint256 registeredAt;    // Registration timestamp
    uint256 lastUpdatedAt;   // Last update timestamp
}
```

### PCRMeasurements
```solidity
struct PCRMeasurements {
    bytes pcr0;  // 48 bytes - Enclave image file hash
    bytes pcr1;  // 48 bytes - Linux kernel and bootstrap hash
    bytes pcr2;  // 48 bytes - Application hash
}
```

### ApprovedPCR
```solidity
struct ApprovedPCR {
    bytes32 pcrHash;
    bool active;
    uint256 approvedAt;
    uint256 expiresAt;    // 0 = no expiry
    string version;
}
```

### VerificationRequest
```solidity
struct VerificationRequest {
    bytes32 teeId;
    bytes32 requestHash;
    bytes32 responseHash;
    uint256 timestamp;
    bytes signature;
}
```

## Signature Format

TEEs must sign settlements using **RSA-PSS with SHA-256**:

```
messageHash = keccak256(requestHash || responseHash || timestamp)  // 96 bytes
signature = RSA-PSS-Sign(SHA256(messageHash), privateKey)
```

**Parameters:**
- Message hash: Keccak256 (Ethereum standard)
- RSA-PSS internal hash: SHA-256
- Salt length: Hash length (32 bytes)
- Key size: Minimum 2048 bits

**Note:** The message is first hashed with Keccak256, then SHA256 is applied internally by RSA-PSS during signing.

## Testing

### Prerequisites
```bash
# Ensure measurements.txt exists with your PCR values
cat measurements.txt
{
  "Measurements": {
    "PCR0": "9baef83909784e4d2cb84466c02931bb...",
    "PCR1": "...",
    "PCR2": "..."
  }
}
```

### Run Integration Tests
```bash
# Start local blockchain node
make start-node

# Run full integration tests
cd scripts/integration
go run test_tee_workflow.go
```

### Expected Output
```
==========================================
  TEE Registry Full Integration Test
==========================================
📍 Primary account: 0x...

------------------------------------------
SECTION 1: Admin Management
------------------------------------------
  ✅ Add first admin (bootstrap)
  ✅ isAdmin returns true for admin
  ✅ isAdmin returns false for non-admin
  ✅ getAdmins returns list

------------------------------------------
SECTION 2: TEE Type Management
------------------------------------------
  ✅ Add TEE type 0 (LLMProxy)
  ✅ Add TEE type 1 (Validator)
  ✅ isValidTEEType returns false for unknown type

------------------------------------------
SECTION 3: PCR Management
------------------------------------------
  ✅ Approve PCR v1.0.0
  ✅ isPCRApproved returns false for unknown PCR
  ✅ getActivePCRs returns list

------------------------------------------
SECTION 4: TEE Registration
------------------------------------------
  🎲 Nonce: 0123456789abcdef...
  ✅ Fetch attestation from enclave
  ✅ Fetch signing public key
  ✅ Fetch TLS certificate
  ✅ Register TEE with attestation

------------------------------------------
SECTION 5: TEE Queries
------------------------------------------
  ✅ isActive returns true for registered TEE
  ✅ getPublicKey returns correct key
  ✅ getTLSCertificate returns cert
  ✅ getActiveTEEs includes registered TEE
  ✅ getTEEsByType(0) includes registered TEE
  ✅ getTEEsByOwner includes registered TEE

------------------------------------------
SECTION 6: TEE Lifecycle
------------------------------------------
  ✅ Deactivate TEE
  ✅ Deactivated TEE not in getActiveTEEs
  ✅ Reactivate TEE

------------------------------------------
SECTION 7: Signature Verification
------------------------------------------
  ✅ Local RSA-PSS signature verification
  ✅ Reject invalid signature

------------------------------------------
SECTION 8: Utility Functions
------------------------------------------
  ✅ computeTEEId matches keccak256
  ✅ computeMessageHash returns hash

==========================================
  Test Summary
==========================================

  Total:  22
  Passed: 22 ✅
  Failed: 0 ❌
```

## Security Considerations

### Dual-Key Security Model

| Attack | Without TLS Binding | Without Signing Binding | With Both Keys |
|--------|-------------------|------------------------|----------------|
| Parent intercepts HTTPS | ❌ Vulnerable | ❌ Vulnerable | ✅ Protected |
| Forge settlement signatures | ✅ Protected | ❌ Vulnerable | ✅ Protected |
| User connects to fake endpoint | ❌ Vulnerable | ✅ Protected | ✅ Protected |
| Payment fraud | ✅ Protected | ❌ Vulnerable | ✅ Protected |

### Best Practices

1. **Always verify both keys** during registration
2. **Download TLS cert from blockchain** before connecting to endpoint
3. **Verify settlement signatures** before processing payments
4. **Keep private keys inside enclave** - never export
5. **Use PCR grace periods** for smooth upgrades
6. **Monitor TEE active status** before routing requests

## Error Codes

| Error | Description |
|-------|-------------|
| `tee: not found` | TEE ID does not exist |
| `tee: already exists` | TEE with this public key exists |
| `tee: not active` | TEE is deactivated |
| `tee: caller is not owner` | Only owner/admin can modify |
| `tee: caller is not admin` | Admin required |
| `tee: PCR not in approved list` | PCR not approved |
| `tee: PCR has expired` | PCR grace period ended |
| `tee: invalid or inactive TEE type` | TEE type invalid |
| `tee: invalid attestation` | Attestation verification failed |
| `tee: public key does not match attestation binding` | Nitriding hash mismatch |
| `tee: invalid signature` | Signature verification failed |
| `tee: settlement already verified` | Replay attack prevented |
| `tee: timestamp too old` | Settlement timestamp expired |
| `tee: timestamp in future` | Settlement timestamp invalid |

## Gas Costs

| Operation | Gas |
|-----------|-----|
| Admin operations | 50,000 |
| Registration with attestation (dual-key) | 600,000 |
| Signature verification (view) | 20,000 |
| Settlement verification (writes) | 25,000 |
| Activate/Deactivate | 10,000 |
| PCR management | 50,000 |
| TEE type management | 30,000 |
| Certificate management | 100,000 |
| Single queries | 1,000 |
| List queries | 5,000 |


## TODO

### Planned Features
- [ ] Event emission for registration/deactivation
- [ ] TLS certificate expiry tracking
- [ ] Unit tests
- [ ] Multi-signature admin operations
- [ ] TEE health monitoring hooks 
- [ ] Batch TEE registration

### Integration Tasks
- [ ] LLM Server: 
- [ ] Facilitator (x402): Call `verifySettlement()` before payment
- [ ] Frontend/dashboard: Download and pin TLS certificates
- [ ] Monitoring: Track TEE active status