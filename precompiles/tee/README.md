# TEE Registry Precompile

EVM precompile at `0x0000000000000000000000000000000000000900` for TEE registration and signature verification.

## Version

v1.2.0 - Includes:
- **Nitriding framework support** (SHA256 public key binding)
- Dynamic AWS root certificate management
- Settlement replay protection
- Timestamp validation
- First admin bootstrap

## Overview

Enables:
- Admin-controlled TEE registration with AWS Nitro attestation
- **Support for Nitriding TLS termination framework**
- Global PCR registry with versioning and grace periods
- On-chain signature verification for inference settlement
- TEE type management for different service categories

## Architecture

```
┌──────────┐     ┌───────────────┐     ┌─────────────┐     ┌────────────┐
│ Operator │────►│  LLM Server   │────►│ Facilitator │────►│ Blockchain │
└──────────┘     │  (TEE)        │     │ (x402)      │     │ (0x900)    │
                 │  [Nitriding]  │     └─────────────┘     └────────────┘
                 └───────────────┘
```

## Key Features

### 1. Admin Management
- First caller automatically becomes initial admin (bootstrap)
- Multiple admins supported
- Cannot remove last admin
- Admin required for all management operations

### 2. Global PCR Registry
- OpenGradient approves PCR configurations
- Version tracking (e.g., "v1.0.0")
- Grace period for upgrades (old PCRs remain valid during transition)
- Auto-expiry after grace period

### 3. TEE Type Management
- Dynamic types (admin can add new types)
- Initial type: `0 = LLMProxy`
- Types can be deactivated (no new registrations)

### 4. Registration Flow (Nitriding Support)

The precompile supports the [Nitriding framework](https://github.com/brave/nitriding) which enables TLS termination inside AWS Nitro Enclaves. With Nitriding, the attestation document contains a SHA256 hash of the public key rather than the full key.

**Why Nitriding?**
- Enables HTTP/TLS for applications inside Nitro Enclaves
- Hides information from the parent EC2 instance
- Avoids dependency on AWS KMS (keys stay decentralized)

**Registration Steps:**

```
1. First caller becomes admin (bootstrap)
2. Admin approves PCR configuration
3. Admin adds TEE type (if new)
4. TEE operator generates RSA keypair inside enclave
5. TEE requests attestation with SHA256(publicKey) in public_key field
6. TEE operator provides attestation + actual public key to admin
7. Admin calls registerTEEWithAttestation(attestation, publicKey, ...)
8. Precompile verifies:
   - AWS signature chain (attestation authenticity)
   - SHA256(providedPublicKey) == attestation.public_key (key binding)
   - PCR matches approved list (enclave integrity)
   - TEE type is valid
9. TEE registered with teeId = keccak256(publicKey)
```

**Public Key Binding Modes:**

| Mode | attestation.public_key | Verification |
|------|------------------------|--------------|
| Nitriding | SHA256(pubKey) - 32 bytes | SHA256(provided) == attestation |
| Standard | Full DER key | provided == attestation |
| Empty | Not present | Accept provided key |

### 5. Verification Flow

```
1. TEE signs inference: RSA-PSS(SHA256(keccak256(inputHash || outputHash || timestamp)))
2. Facilitator calls verifySettlement()
3. Precompile verifies signature against stored public key
4. Returns validity (event emission planned)
```

## Quick Start

### Genesis Configuration

To set an initial admin at genesis, configure in your genesis file:

```json
{
  "alloc": {
    "0x0000000000000000000000000000000000000900": {
      "storage": {
        "0x<admin_flag_slot>": "0x01",
        "0x<admin_count_slot>": "0x01",
        "0x<admin_list_slot_0>": "0x<initial_admin_address>"
      }
    }
  }
}
```

Alternatively, the first account to call `addAdmin()` becomes the initial admin.

### Basic Setup

```solidity
ITEERegistry tee = ITEERegistry(0x0000000000000000000000000000000000000900);

// 1. Bootstrap first admin (only works if no admins exist)
tee.addAdmin(msg.sender);

// 2. Add TEE type
tee.addTEEType(0, "LLMProxy");

// 3. Approve PCR configuration
PCRMeasurements memory pcrs = PCRMeasurements(pcr0, pcr1, pcr2);
tee.approvePCR(pcrs, "v1.0.0", bytes32(0), 0);

// 4. Register TEE (requires valid attestation + public key)
bytes32 teeId = tee.registerTEEWithAttestation(
    attestationDocument,
    publicKeyDER,           // Actual RSA public key (DER-encoded)
    paymentAddress,
    "https://tee.example.com",
    0 // LLMProxy
);
```

### Signature Verification

```solidity
// Verify a settlement
bool valid = tee.verifySettlement(
    teeId,
    inputHash,
    outputHash,
    timestamp,
    signature
);
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

### Registration Functions

| Function | Description |
|----------|-------------|
| `registerTEEWithAttestation(attestation, publicKey, ...)` | Register TEE with attestation and public key |
| `deactivateTEE(bytes32)` | Deactivate TEE (owner or admin) |
| `activateTEE(bytes32)` | Reactivate TEE (owner or admin) |

### Verification Functions

| Function | Description |
|----------|-------------|
| `verifySignature(request) → bool` | Verify signature (view) |
| `verifySettlement(...) → bool` | Verify and record settlement |

### Query Functions

| Function | Description |
|----------|-------------|
| `getTEE(bytes32) → TEEInfo` | Get TEE details |
| `getActiveTEEs() → bytes32[]` | Get all active TEE IDs |
| `getTEEsByType(uint8) → bytes32[]` | Get TEEs by type |
| `getTEEsByOwner(address) → bytes32[]` | Get TEEs by owner |
| `getPublicKey(bytes32) → bytes` | Get TEE public key |
| `isActive(bytes32) → bool` | Check if TEE is active |

## Data Structures

```solidity
struct PCRMeasurements {
    bytes pcr0;  // 48 bytes - Enclave image hash
    bytes pcr1;  // 48 bytes - Kernel hash  
    bytes pcr2;  // 48 bytes - Application hash
}

struct ApprovedPCR {
    bytes32 pcrHash;
    bool active;
    uint256 approvedAt;
    uint256 expiresAt;    // 0 = no expiry
    string version;
}

struct TEETypeInfo {
    uint8 typeId;
    string name;
    bool active;
    uint256 addedAt;
}

struct TEEInfo {
    bytes32 teeId;
    address owner;
    address paymentAddress;
    string endpoint;
    bytes publicKey;      // Full DER-encoded RSA public key
    bytes32 pcrHash;
    uint8 teeType;
    bool active;
    uint256 registeredAt;
    uint256 lastUpdatedAt;
}

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
message = keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
signature = RSA-PSS-Sign(SHA256(message), privateKey)
```

Parameters:
- Hash algorithm: SHA-256
- Salt length: Hash length (32 bytes)
- Key size: Minimum 2048 bits

## Nitriding Integration Guide

### TEE Side (Inside Enclave)

```go
// 1. Generate RSA keypair
privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
publicKeyDER, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)

// 2. Compute hash for attestation
pubKeyHash := sha256.Sum256(publicKeyDER)

// 3. Request attestation with hash in public_key field
// (Nitriding handles this automatically)
attestation := nitriding.GetAttestation(pubKeyHash[:])

// 4. Export attestation + public key for registration
// Send both to admin for on-chain registration
```

### Admin Side (Registration)

```go
// Receive from TEE operator
attestationDoc := /* raw CBOR attestation */
publicKeyDER := /* DER-encoded RSA public key */

// Call precompile
teeId, err := teeRegistry.RegisterTEEWithAttestation(
    attestationDoc,
    publicKeyDER,
    paymentAddress,
    endpoint,
    teeType,
)
```

### Verification Flow

The precompile automatically detects Nitriding mode:
1. If `attestation.public_key` is 32 bytes → Nitriding mode
2. Computes `SHA256(providedPublicKey)`
3. Verifies hash matches attestation binding
4. Stores full public key for signature verification

## PCR Upgrade Flow

```
Day 0:  Admin approves PCR v1.0 (no expiry)
        approvePCR(pcrs_v1, "v1.0", 0x0, 0)

Day 30: New code released
        approvePCR(pcrs_v2, "v1.1", pcrHashV1, 7 days)
        - PCR v1.0: active, expiresAt = Day 37
        - PCR v1.1: active, expiresAt = 0

Day 30-37: Both PCRs valid, operators upgrade TEEs

Day 37: PCR v1.0 auto-expires
        - Old TEEs still work (already registered)
        - New registrations require v1.1
```

## Error Codes

| Error | Description |
|-------|-------------|
| `tee: not found` | TEE ID does not exist |
| `tee: already exists` | TEE with this public key exists |
| `tee: not active` | TEE is deactivated |
| `tee: caller is not owner` | Only owner/admin can modify |
| `tee: caller is not admin` | Admin required |
| `tee: admin already exists` | Admin already registered |
| `tee: admin not found` | Admin does not exist |
| `tee: cannot remove last admin` | At least one admin required |
| `tee: PCR not in approved list` | PCR not approved |
| `tee: PCR has expired` | PCR grace period ended |
| `tee: PCR not found` | PCR hash not found |
| `tee: invalid or inactive TEE type` | TEE type invalid |
| `tee: TEE type already exists` | Duplicate type ID |
| `tee: TEE type not found` | Type does not exist |
| `tee: invalid attestation` | Attestation verification failed |
| `tee: public key does not match attestation binding` | **Nitriding hash mismatch** |
| `tee: invalid signature` | Signature verification failed |
| `tee: invalid public key` | Public key format invalid |
| `tee: invalid input` | Malformed input data |
| `tee: method not found` | Unknown method selector |
| `tee: write protection` | Write in read-only context |

## Gas Costs

| Operation | Gas |
|-----------|-----|
| Admin operations | 50,000 |
| Registration with attestation | 600,000 |
| Signature verification | 20,000 |
| Settlement verification | 25,000 |
| Activate/Deactivate | 10,000 |
| PCR management | 50,000 |
| TEE type management | 30,000 |
| Single queries | 1,000 |
| List queries | 5,000 |

## Files

```
precompiles/tee/
├── precompile.go       # Main entry point, method routing
├── types.go            # Data structures
├── storage.go          # State management
├── attestation.go      # AWS Nitro verification
├── errors.go           # Error definitions
├── abi.go              # ABI definition
└── README.md           # This file

scripts/
└── test_tee_workflow.go  # Integration tests
```

## Testing

```bash
# Start local node
make start-node

# Run integration tests
cd scripts
go run test_tee_workflow.go
```

### Expected Output

```
==========================================
  TEE Registry Integration Test
==========================================
📍 Using account: 0x...

Test 1: First Admin Bootstrap
   ✅ First admin successfully bootstrapped

Test 2: Admin Management
   ✅ Add Second Admin
   ✅ Unauthorized Add Admin (correctly rejected)
   ✅ Cannot Remove Last Admin

Test 3: TEE Type Management
   ✅ Add TEE Type
   ✅ Duplicate TEE Type Rejected

Test 4: PCR Management
   ✅ Approve PCR
   ✅ Get Active PCRs
   ✅ Unapproved PCR Returns False

Test 5: Nitriding Public Key Binding
   ✅ SHA256 hash binding verified
   ✅ Wrong public key rejected

Test 6: Signature Verification (Local)
   ✅ Local Signature Verification
   ✅ Wrong Key Rejected
   ✅ Tampered Message Rejected

==========================================
Results: 14 passed, 0 failed
✅ All tests passed!
```

## Security Considerations

1. **Admin Bootstrap**: First caller becomes admin - ensure controlled deployment
2. **Attestation**: Verify against stored or default AWS Nitro root certificate
3. **Public Key Binding**: Nitriding uses SHA256 hash binding - cryptographically secure
4. **PCR Management**: Use grace periods for smooth upgrades
5. **Replay Protection**: Settlements are tracked to prevent double-verification
6. **Timestamp Bounds**: Settlements must be within 1 hour age and 5 minutes future tolerance
7. **Key Security**: TEE private keys must never leave the enclave

## Certificate Management

The precompile supports dynamic AWS root certificate updates:

```solidity
// Set custom root certificate (admin only)
tee.setAWSRootCertificate(customCertPEM);

// Get hash of current certificate
bytes32 certHash = tee.getAWSRootCertificateHash();
```

**Behavior:**
- If no certificate is set on-chain, the default AWS Nitro root certificate is used
- Certificates must be valid PEM format
- Only admins can update the certificate

## Settlement Verification

### Replay Protection

Each settlement can only be verified once:

```solidity
// First call - succeeds
bool valid1 = tee.verifySettlement(teeId, input, output, ts, sig); // true

// Second call with same parameters - reverts
bool valid2 = tee.verifySettlement(teeId, input, output, ts, sig); // reverts
```

### Timestamp Validation

Settlements are validated against timestamp bounds:
- **Max Age**: 3600 seconds (1 hour)
- **Future Tolerance**: 300 seconds (5 minutes)

### verifySignature vs verifySettlement

| Function | State | Replay Protection | Use Case |
|----------|-------|-------------------|----------|
| `verifySignature` | view (read-only) | No | Pre-check before committing |
| `verifySettlement` | nonpayable (writes) | Yes | Actual settlement recording |

## Completed Features

- ✅ First admin bootstrap
- ✅ Settlement replay protection
- ✅ Timestamp validation
- ✅ Dynamic certificate management
- ✅ Storage slot collision fixes
- ✅ **Nitriding framework support (v1.2.0)**

## TODO

### Blockchain
- [ ] Event emission for all state changes
- [ ] Genesis config for initial admin
- [ ] Gas optimization review

### LLM Server
- [ ] Implement `/admin/register` endpoint
- [ ] Store TEE ID after registration
- [ ] Export public key for registration

### Facilitator (x402)
- [ ] Call `verifySettlement()` before payment
- [ ] Handle verification failures
- [ ] Index settlement events

### Documentation
- [ ] Security audit checklist
- [ ] Deployment runbook
- [ ] Monitoring guide