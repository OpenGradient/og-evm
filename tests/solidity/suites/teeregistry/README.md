# TEERegistry Test Suite

Comprehensive test suite for the TEERegistry smart contract.

## Overview

This test suite covers all functionality of the TEERegistry contract including:
- Initialization and deployment
- TEE type management
- PCR (Platform Configuration Register) management
- AWS root certificate management
- TEE registration with attestation
- TEE lifecycle management (activation/deactivation)
- Signature and settlement verification
- Access control and permissions

## Test Files

### 1_initialization.js
Tests contract deployment, role setup, and constants verification.

**Coverage:**
- Contract deployment
- DEFAULT_ADMIN_ROLE assignment
- TEE_OPERATOR role assignment
- Role admin configuration
- Constants validation (MAX_SETTLEMENT_AGE, FUTURE_TOLERANCE, VERIFIER address)

### 2_tee_types.js
Tests TEE type management functionality.

**Coverage:**
- Adding TEE types
- Deactivating TEE types
- Validating TEE types
- Querying all TEE types
- Error handling (duplicate types, non-existent types)
- Access control

### 3_pcr_management.js
Tests PCR (Platform Configuration Register) management.

**Coverage:**
- Computing PCR hashes
- Approving PCRs with versioning
- PCR expiry and grace periods during upgrades
- Revoking PCRs
- Checking PCR approval status
- Querying active PCRs
- Access control

### 4_certificate_management.js
Tests AWS root certificate management.

**Coverage:**
- Setting AWS root certificate
- Updating AWS root certificate
- Event emission with certificate hash
- Access control

### 5_tee_registration.js
Tests TEE registration functionality.

**Coverage:**
- Computing TEE IDs from public keys
- Validating TEE type requirements
- Access control for registration
- Error handling (invalid types)

**Note:** Full attestation verification tests require the TEE precompile at address 0x900 to be available (cosmos network).

### 6_tee_lifecycle.js
Tests TEE activation/deactivation and query functions.

**Coverage:**
- TEE deactivation
- TEE activation
- Query functions (getTEE, getPublicKey, getTLSCertificate, etc.)
- Error handling for non-existent TEEs
- Active TEE tracking

**Note:** Full lifecycle tests require registered TEEs, which depend on the precompile.

### 7_verification.js
Tests signature verification and settlement functionality.

**Coverage:**
- Computing message hashes
- Signature verification
- Settlement verification with timestamp validation
- Replay protection
- Timestamp validation (too old, future tolerance)

**Note:** Full verification tests require the RSA-PSS precompile functionality.

### 8_access_control.js
Tests role-based access control.

**Coverage:**
- Role management (grant, revoke)
- Admin-only function protection
- Operator-only function protection
- Public view function access
- Role hierarchy (DEFAULT_ADMIN_ROLE → TEE_OPERATOR)

## Running Tests

### Against Ganache (local)
```bash
cd tests/solidity/suites/teeregistry
yarn install
yarn test-ganache
```

### Against Cosmos Network (with precompiles)
```bash
cd tests/solidity/suites/teeregistry
yarn install
yarn test-cosmos
```

**Note:** Running against cosmos network requires:
1. Local cosmos node running at http://127.0.0.1:8545
2. TEE precompile deployed at address 0x900
3. Accounts with sufficient balance for gas

### From Root Test Directory
```bash
cd tests/solidity
yarn install
node test-helper.js --network ganache --allowTests=teeregistry
```

Or for cosmos network:
```bash
node test-helper.js --network cosmos --allowTests=teeregistry
```

## Test Coverage

| Category | Coverage |
|----------|----------|
| Initialization | ✓ Full |
| TEE Types | ✓ Full |
| PCR Management | ✓ Full |
| Certificate Management | ✓ Full |
| Access Control | ✓ Full |
| TEE Registration | ⚠️ Partial (requires precompile) |
| TEE Lifecycle | ⚠️ Partial (requires precompile) |
| Verification | ⚠️ Partial (requires precompile) |

## Dependencies

- Hardhat: Ethereum development environment
- Ethers.js v6: Ethereum library
- Chai: Assertion library
- OpenZeppelin Contracts: For AccessControl and utilities

## Architecture Notes

### Precompile Integration
The TEERegistry contract integrates with a precompile at address `0x900` for cryptographic operations:
- `verifyAttestation`: Verifies AWS Nitro attestation documents
- `verifyRSAPSS`: Verifies RSA-PSS signatures

Tests that rely on these precompile functions will only fully pass when running against a cosmos network with the precompile implemented.

### Role System
- **DEFAULT_ADMIN_ROLE**: Can manage all aspects (TEE types, PCRs, certificates, roles)
- **TEE_OPERATOR**: Can register TEEs (requires attestation verification)

### Test Data
Tests use sample data for:
- PCR measurements (48-byte hex strings)
- Public keys (256-byte DER-encoded RSA keys)
- Certificates (DER-encoded X.509)
- Attestation documents (CBOR-encoded)

## Future Enhancements

1. **Integration Tests**: Full end-to-end tests with real attestation documents
2. **Mock Precompile**: Mock contract for local testing without cosmos network
3. **Gas Profiling**: Measure gas costs for all operations
4. **Stress Tests**: Test with large numbers of TEEs, PCRs, and types
5. **Upgrade Tests**: Test contract upgradability scenarios
