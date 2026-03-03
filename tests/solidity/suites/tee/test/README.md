# TEE Registry and Precompile Tests

End-to-end tests for the TEE (Trusted Execution Environment) Registry contract and the TEE Verifier precompile at address `0x900`.

## Test Structure

### 1. Precompile Tests (`precompile.js`)
Tests the TEE Verifier precompile directly at `0x0000000000000000000000000000000000000900`.

**Coverage:**
- RSA-PSS signature verification (`verifyRSAPSS`)
  - Valid signature verification
  - Invalid signature rejection
  - Wrong message hash detection
  - Invalid public key handling
  - Weak key rejection (1024-bit keys)
  - Empty input handling
- AWS Nitro attestation verification (`verifyAttestation`)
  - Empty input validation
  - Invalid attestation format rejection
  - Size limit enforcement (DoS prevention)
- Gas usage measurement

### 2. Registry Tests (`registry.js`)
Tests the TEERegistry contract lifecycle and management functions.

**Coverage:**
- **Initialization**
  - Role setup (DEFAULT_ADMIN_ROLE, TEE_OPERATOR)
  - Precompile address verification
  - Constants validation
- **TEE Type Management**
  - Adding TEE types
  - Duplicate prevention
  - Deactivation
  - Access control
- **PCR Management**
  - PCR approval with versioning
  - Grace period handling for PCR updates
  - PCR revocation
  - Active PCR listing
  - Access control
- **Certificate Management**
  - AWS root certificate setup
  - Access control
- **TEE Lifecycle**
  - Registration validation (role enforcement, TEE type validation)
  - Invalid attestation handling
- **Query Functions**
  - TEE ID computation
  - PCR hash computation
  - Message hash computation
  - Non-existent TEE handling
  - Empty arrays for new entities
- **Access Control**
  - Role enforcement across all admin functions
  - Role management (grant/revoke)

### 3. Inference Verifier Tests (`inferenceVerifier.js`)
Tests the TEEInferenceVerifier contract for signature verification with timestamp validation.

**Coverage:**
- **Initialization**
  - Registry address setup
  - Admin role assignment
  - Precompile address and time constants
- **Registry Management**
  - Admin registry update
  - Non-admin rejection
- **Hash Computation**
  - Message hash format (keccak256(inputHash || outputHash || timestamp))
  - Consistency checks across different inputs/timestamps
- **Signature Verification**
  - Inactive TEE returns false
  - Old timestamp rejection (> 1 hour)
  - Future timestamp rejection (> 5 minutes)
- **Access Control**
  - Role enforcement for setRegistry
  - Role grant/revoke management

## Test Helper Contract

`TEETestHelper.sol` provides convenient wrappers and utilities for testing:
- Direct precompile access
- Registry function wrappers
- Gas estimation helpers
- Hash computation utilities

## Running Tests

### All TEE Tests
```bash
cd tests/solidity/suites/tee
npx hardhat test test/*.js --network cosmos
```

### Individual Test Suites
```bash
# Precompile tests only
npx hardhat test test/precompile.js --network cosmos

# Registry tests only
npx hardhat test test/registry.js --network cosmos

# Inference verifier tests only
npx hardhat test test/inferenceVerifier.js --network cosmos
```

## Requirements

- Node.js and npm
- Hardhat
- A running node with the TEE precompile enabled
- The following npm packages:
  - `@nomicfoundation/hardhat-toolbox`
  - `chai`
  - `crypto` (Node.js built-in)

## Test Data

The tests use:
- Dynamically generated RSA key pairs (2048-bit)
- Mock attestation documents for negative testing
- Hardcoded PCR values for testing PCR management
- Test TEE types (AWS Nitro, Custom)

## Important Notes

### Attestation Verification
The full attestation verification requires:
1. Valid AWS Nitro attestation document (CBOR format)
2. Properly formatted signing public key (DER-encoded RSA)
3. Valid TLS certificate (DER-encoded X.509)
4. AWS root certificate for chain verification

Since generating valid attestation documents requires actual AWS Nitro hardware, the tests focus on:
- Input validation and error handling
- The integration points with the precompile
- The cryptographic signature verification (RSA-PSS)

### Signature Flow
The correct flow for TEE inference verification:
1. TEE computes: `messageHash = keccak256(inputHash || outputHash || timestamp)`
2. TEE signs: `signature = RSA-PSS(SHA256(messageHash), privateKey)`
3. TEEInferenceVerifier checks: TEE active, timestamp in bounds, then calls `verifyRSAPSS(publicKey, messageHash, signature)`
4. FacilitatorSettlementRelay calls `verifySignature()` and emits settlement events

### Gas Costs
- `verifyAttestation`: ~500,000 gas (expensive due to crypto operations)
- `verifyRSAPSS`: ~20,000 gas

### Time Windows
- Maximum settlement age: 1 hour (3600 seconds)
- Future tolerance: 5 minutes (300 seconds)
- These prevent both replay attacks and timestamp manipulation

## Test Coverage Summary

| Component | Test Coverage |
|-----------|---------------|
| Precompile RSA-PSS | ✅ Full |
| Precompile Attestation | ✅ Input validation (partial - requires hardware for full test) |
| Registry Roles | ✅ Full |
| TEE Types | ✅ Full |
| PCR Management | ✅ Full |
| Inference Verification | ✅ Full (logic and validation) |
| Timestamp Validation | ✅ Full |
| Query Functions | ✅ Full |
| Access Control | ✅ Full |

## Future Enhancements

Potential additions for more comprehensive testing:
1. Integration with real AWS Nitro attestation documents
2. Performance/stress testing with many TEEs
3. Concurrent settlement verification testing
4. TEE key rotation scenarios
5. Multi-TEE coordination tests
