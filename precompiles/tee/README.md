# TEE Registry Precompile

EVM precompile at `0x0000000000000000000000000000000000000900` for TEE registration and signature verification.

## Version

- Nitriding Dual-Key Support

### What's New
- ✅ **TLS certificate verification** - Cryptographic binding for HTTPS encryption
- ✅ **Signing key verification** - Separate key for settlement signatures  
- ✅ **Dual SHA256 hash binding** - Both keys verified in attestation user_data
- ✅ **Multihash format parsing** - Support for 0x1220 prefix format
- ✅ **getTLSCertificate()** - New query method for TLS certificates


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

The **Nitriding framework** generates TWO separate cryptographic keys inside the AWS Nitro Enclave:

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

**With both (Nitriding):**
- ✅ HTTPS terminates inside enclave (privacy)
- ✅ Settlements cryptographically proven (integrity)
- ✅ Full end-to-end security guarantee

### User Data Format (68 bytes)
To check with Kyle
### Registration Flow
```
┌─────────────────────────────────────────────────────────────────┐
│ 1. Enclave Setup (Inside AWS Nitro Enclave)                     │
├─────────────────────────────────────────────────────────────────┤
│  • Generate TLS keypair (for HTTPS)                             │
│  • Generate Signing keypair (for settlements)                   │
│  • Compute tlsHash = SHA256(TLS cert public key)                │
│  • Compute appHash = SHA256(signing public key)                 │
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
│  ✓ Verify SHA256(tlsCertDER.pubkey) == user_data[2:34]         │
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
| `getTLSCertificate(bytes32) → bytes` | **NEW** - Get TLS certificate (for HTTPS) |
| `isActive(bytes32) → bool` | Check if TEE is active |

## Data Structures

### TEEInfo
```solidity
struct TEEInfo {
    bytes32 teeId;           // keccak256(signingPublicKey)
    address owner;           // TEE owner
    address paymentAddress;  // Payment recipient
    string endpoint;         // HTTPS endpoint
    bytes publicKey;         // RSA signing key (for settlements)
    bytes tlsCertificate;    // TLS certificate (for HTTPS) ⭐ NEW
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

## Signature Format

TEEs must sign settlements using **RSA-PSS with SHA-256**:
```
message = keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
signature = RSA-PSS-Sign(SHA256(message), privateKey)
```

**Parameters:**
- Hash algorithm: SHA-256
- Salt length: Hash length (32 bytes)
- Key size: Minimum 2048 bits

## Testing

### Prerequisites
```bash
# Install Python dependencies for attestation testing
pip install cbor2 cryptography

# Ensure measurements.txt exists with your PCR values
```

### Run Integration Tests
```bash
# Start local blockchain node
make start-node

# Run Nitriding dual-key integration tests
cd scripts
go run test_tee_nitriding.go
```

### Expected Output
```
==========================================
  TEE Registry Nitriding Integration Test
  (Dual-Key Verification)
==========================================
📍 Using account: 0x...

------------------------------------------
Test 1: Download Attestation & Parse
------------------------------------------
🎲 Generated nonce: 0123456789abcdef...
✅ Downloaded attestation (4555 bytes CBOR)
✅ Decoded: 4555 bytes CBOR

[Attestation Details]
  Module ID: i-0340e0cb833504eb6-enc019c12d31f78864d
  PCR0: 9baef83909784e4d2cb84466c02931bb...
  User Data: 68 bytes

------------------------------------------
Test 2: Parse Nitriding User Data
------------------------------------------
✅ User data is 68 bytes (Nitriding format)
✅ Multihash prefixes valid (0x1220)

[Extracted Hashes]
  TLS Key Hash: b735546536e78ee12beea2d384fcda35...
  App Key Hash: abf9d7453422eb419fc1391604b070a8...

------------------------------------------
Test 3: TLS Certificate Verification
------------------------------------------
✅ Downloaded TLS certificate (91 bytes DER)
✅ Extracted public key (91 bytes)

[TLS Key Verification]
  Computed:  5f237d09494db58b77ecfc79452d6759...
  From attestation: b735546536e78ee12beea2d384fcda35...
✅ TLS certificate hash MATCHES!

------------------------------------------
Test 4: Signing Key Verification
------------------------------------------
✅ Generated signing keypair (RSA 2048)

[Signing Key Verification]
  Computed:  abf9d7453422eb419fc1391604b070a8...
  From attestation: abf9d7453422eb419fc1391604b070a8...
✅ Signing key hash MATCHES!

------------------------------------------
Test 5: Blockchain Setup
------------------------------------------
📤 addAdmin tx: 0x...
   ✅ Transaction confirmed
📤 addTEEType tx: 0x...
   ✅ Transaction confirmed
📤 approvePCR tx: 0x...
   ✅ Transaction confirmed

------------------------------------------
Test 6: Register TEE (Dual Keys)
------------------------------------------
✅ TEE registered successfully!
   TEE ID: 0x7a8f2b3c...

------------------------------------------
Test 7: Query TLS Certificate
------------------------------------------
✅ Retrieved TLS certificate from chain
   Length: 91 bytes
✅ Certificate matches original!

==========================================
  Test Summary
==========================================
✅ Attestation download & parsing
✅ Nitriding user_data format (68 bytes)
✅ TLS certificate hash verification
✅ Signing key hash verification
✅ Dual-key TEE registration
✅ TLS certificate retrieval

🎉 All tests passed!
```

## Security Considerations

### Dual-Key Security Model

#### 1. TLS Certificate Security
- ✅ **Proves HTTPS endpoint is inside enclave**
- ✅ **Prevents man-in-the-middle attacks** from parent instance
- ✅ **Users can verify certificate** before connecting
- ⚠️ **Does NOT prove computation authenticity** (use signing key)

#### 2. Signing Key Security
- ✅ **Proves settlement signatures from enclave**
- ✅ **Cryptographically bound to attestation**
- ✅ **Prevents settlement forgery**
- ⚠️ **Does NOT handle TLS encryption** (use TLS cert)

### Attack Scenarios Prevented

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
| `tee: public key does not match attestation binding` | **Nitriding hash mismatch** - TLS or signing key binding failed |
| `tee: invalid signature` | Signature verification failed |
| `tee: settlement already verified` | Replay attack prevented |
| `tee: timestamp too old` | Settlement timestamp expired |
| `tee: timestamp in future` | Settlement timestamp invalid |

## Gas Costs

| Operation | Gas |
|-----------|-----|
| Admin operations | 50,000 |
| **Registration with attestation (dual-key)** | **600,000** |
| Signature verification (view) | 20,000 |
| Settlement verification (writes) | 25,000 |
| Activate/Deactivate | 10,000 |
| PCR management | 50,000 |
| TEE type management | 30,000 |
| Certificate management | 100,000 |
| Single queries | 1,000 |
| List queries | 5,000 |

## Files
```
precompiles/tee/
├── precompile.go       # Main entry point, dual-key registration
├── types.go            # Data structures (added TLSCertificate)
├── storage.go          # State management (TLS cert storage)
├── attestation.go      # AWS Nitro + multihash parsing
├── abi.go              # Updated ABI with tlsCertificate
├── errors.go           # Error definitions
└── README.md           # This file

scripts/
├── test_tee_nitriding.go    # Dual-key integration tests
└── measurements.txt         # PCR values from enclave build
```

## Changelog

### v1.3.0 (Current) - Nitriding Dual-Key Support
- ✅ Added TLS certificate verification and storage
- ✅ Implemented dual-key binding (TLS + Signing)
- ✅ Added multihash format parsing (0x1220 prefix)
- ✅ New `getTLSCertificate()` query function
- ✅ Updated `TEEInfo` structure with `tlsCertificate` field
- ✅ Updated `registerTEEWithAttestation()` to accept 3 parameters (attestation, signingKey, tlsCert)
- ✅ Enhanced `attestation.go` with `ParseNitridingUserData()`
- ✅ Added `VerifyTLSCertificateBinding()` and `VerifySigningKeyBinding()` functions
- ✅ Full Nitriding workflow support

### v1.2.0 - Nitriding Framework Support
- ✅ Basic Nitriding SHA256 binding
- ✅ Single key verification

### v1.1.0 - Certificate Management
- ✅ Dynamic AWS root certificate management
- ✅ Settlement replay protection
- ✅ Timestamp validation

### v1.0.0 - Initial Release
- ✅ Basic TEE registration with attestation
- ✅ PCR verification
- ✅ Admin management

## Migration Guide (v1.2 → v1.3)

If you have existing code using v1.2, update as follows:

### Old (v1.2):
```solidity
tee.registerTEEWithAttestation(
    attestationDocument,
    publicKeyDER,           // Single key
    paymentAddress,
    endpoint,
    teeType
);
```

### New (v1.3):
```solidity
tee.registerTEEWithAttestation(
    attestationDocument,
    signingPublicKeyDER,    // Key for settlements
    tlsCertificateDER,      // Key for HTTPS ⭐ NEW
    paymentAddress,
    endpoint,
    teeType
);
```

### Retrieve TLS Certificate (New):
```solidity
bytes memory tlsCert = tee.getTLSCertificate(teeId);
```

## TODO

### Planned Features
- [ ] Event emission for all state changes
- [ ] TLS certificate expiry tracking
- [ ] Key rotation support
- [ ] Multi-signature admin operations
- [ ] Batch TEE registration
- [ ] TEE health monitoring hooks

### Integration Tasks
- [ ] LLM Server: Implement dual-key generation in enclave
- [ ] Facilitator (x402): Call `verifySettlement()` before payment
- [ ] Frontend: Download and verify TLS certificates
- [ ] Monitoring: Track TEE active status

## Support

For questions or issues:
- GitHub Issues: `<your-repo>/issues`
- Documentation: `<your-docs-url>`
- Discord: `<your-discord>`

A lot of updates are missed @khalifa
