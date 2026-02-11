// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ITEERegistry - Interface for TEE Registry
/// @notice Manages TEE registration and signature verification for X402 settlements
/// @dev Supports Nitriding framework with dual-key verification (TLS + Signing)
///
/// ## High-Level Flow
///
/// ### Roles & Permissions
/// - **TEE_ADMIN_ROLE**: Protocol administrators who manage the registry
///   - Add/remove TEE types and approve PCR measurements
///   - Set AWS root certificates for attestation verification
///   - Register TEEs (become owners) and can deactivate/activate ANY TEE
/// - **TEE_OPERATOR_ROLE**: TEE operators who run enclaves
///   - Register their own TEEs (become owners)
///   - Can only deactivate/activate TEEs they own
///
/// ### TEE Lifecycle
/// 1. **Setup** (Admin):
///    - Admin calls `addTEEType()` to create allowed TEE types (e.g., AWS Nitro)
///    - Admin calls `approvePCR()` to approve enclave measurements
///    - Admin calls `setAWSRootCertificate()` for attestation verification
///
/// 2. **Registration** (Operator or Admin):
///    - Operator runs a TEE (e.g., AWS Nitro Enclave with Nitriding framework)
///    - TEE generates signing key + TLS certificate, creates attestation document
///    - Operator calls `registerTEEWithAttestation()` with:
///      - Raw attestation document from AWS Nitro
///      - RSA signing public key (for settlement signatures)
///      - TLS certificate (for HTTPS connections)
///      - Payment address and endpoint URL
///    - Registry verifies:
///      - Attestation signature against AWS root certificate
///      - PCR measurements match approved values
///      - Both keys are bound in attestation.user_data (Nitriding framework)
///    - TEE is registered with `teeId = keccak256(signingPublicKey)`
///
/// 3. **Settlement Verification**:
///    - Client sends request to TEE endpoint (HTTPS with TLS cert verification)
///    - TEE processes request and signs response with signing key
///    - Client calls `verifySettlement()` with signature
///    - Registry verifies signature using TEE's public key
///    - Settlement is recorded on-chain with replay protection
///
/// 4. **Deactivation**:
///    - Owner or admin calls `deactivateTEE()` to pause a TEE
///    - TEE can be reactivated with `activateTEE()`
///    - For metadata changes (endpoint, keys), deactivate + re-register with new attestation
///
/// ### PCR Management
/// - PCRs (Platform Configuration Registers) are cryptographic measurements of enclave code
/// - Admins approve specific PCR values representing trusted enclave builds
/// - When approving new PCR, can set grace period on previous PCR for smooth upgrades
/// - Expired or revoked PCRs prevent new registrations but don't affect existing TEEs
///
interface ITEERegistry {

    // ============ Errors ============

    error TEENotFound(bytes32 teeId);
    error TEENotActive(bytes32 teeId);
    error TEEAlreadyExists(bytes32 teeId);
    error NotTEEOwner(bytes32 teeId, address caller, address owner);
    error InvalidSignature();
    error InvalidPublicKey();
    error InvalidAttestation();
    error InvalidAWSSignature();
    error PCRNotApproved();
    error PCRExpired(bytes32 pcrHash);
    error InvalidTEEType(uint8 teeType);
    error TEETypeExists(uint8 teeType);
    error TimestampTooOld(uint256 timestamp, uint256 maxAge);
    error TimestampInFuture(uint256 timestamp);
    error RootCertificateNotSet();
    error PublicKeyBindingFailed();
    
    // ============ Structs ============
    
    /// @notice PCR measurements from AWS Nitro Enclave
    /// @dev AWS Nitro produces SHA-384 (48 bytes)
    struct PCRMeasurements {
        bytes pcr0;  // Enclave image file hash (48 bytes)
        bytes pcr1;  // Linux kernel and bootstrap hash (48 bytes)
        bytes pcr2;  // Application hash (48 bytes)
    }
    
    /// @notice Approved PCR record
    struct ApprovedPCR {
        bytes32 pcrHash;
        bool active;
        uint256 approvedAt;
        uint256 expiresAt;
        string version;
    }
    
    /// @notice TEE Type definition
    struct TEETypeInfo {
        uint8 typeId;
        string name;
        bool active;
        uint256 addedAt;
    }
    
    /// @notice Complete TEE registration record
    struct TEEInfo {
        bytes32 teeId;
        address owner;
        address paymentAddress;
        string endpoint;
        bytes publicKey;          // RSA signing key for settlements
        bytes tlsCertificate;     // TLS certificate for HTTPS 
        bytes32 pcrHash;          // Reference to approved PCR
        uint8 teeType;
        bool active;
        uint256 registeredAt;
        uint256 lastUpdatedAt;
    }
    
    /// @notice Bundled verification request
    struct VerificationRequest {
        bytes32 teeId;
        bytes32 requestHash;
        bytes32 responseHash;
        uint256 timestamp;
        bytes signature;
    }
    
    // ============ Events ============

    event TEETypeAdded(uint8 indexed typeId, string name, uint256 addedAt);
    event TEETypeDeactivated(uint8 indexed typeId, uint256 deactivatedAt);
    
    event PCRApproved(bytes32 indexed pcrHash, string version, uint256 approvedAt, uint256 expiresAt);
    event PCRRevoked(bytes32 indexed pcrHash, uint256 revokedAt);
    
    event TEERegistered(bytes32 indexed teeId, address indexed owner, address paymentAddress, string endpoint, uint8 teeType, uint256 registeredAt);
    event TEEDeactivated(bytes32 indexed teeId, uint256 deactivatedAt);
    event TEEActivated(bytes32 indexed teeId, uint256 activatedAt);
    
    event SettlementVerified(bytes32 indexed teeId, bytes32 indexed settlementHash, address indexed caller, uint256 timestamp);
    
    event AWSRootCertificateUpdated(bytes32 indexed certificateHash, address indexed updatedBy, uint256 updatedAt);

    // ============ TEE Type Management ============
    
    function addTEEType(uint8 typeId, string calldata name) external;
    function deactivateTEEType(uint8 typeId) external;
    function isValidTEEType(uint8 typeId) external view returns (bool);
    function getTEETypes() external view returns (TEETypeInfo[] memory);

    // ============ PCR Management ============
    
    function approvePCR(PCRMeasurements calldata pcrs, string calldata version, bytes32 previousPcrHash, uint256 gracePeriod) external;
    function revokePCR(bytes32 pcrHash) external;
    function isPCRApproved(PCRMeasurements calldata pcrs) external view returns (bool);
    function getActivePCRs() external view returns (bytes32[] memory);
    function getPCRDetails(bytes32 pcrHash) external view returns (ApprovedPCR memory);
    function computePCRHash(PCRMeasurements calldata pcrs) external pure returns (bytes32);

    // ============ Certificate Management ============
    
    function setAWSRootCertificate(bytes calldata certificate) external;
    function getAWSRootCertificateHash() external view returns (bytes32);

    // ============ TEE Registration ============
    
    /// @notice Register a TEE with AWS Nitro attestation and dual-key verification
    /// @dev Supports Nitriding framework where attestation.user_data contains SHA256 hashes of both keys
    /// @param attestationDocument Raw CBOR-encoded AWS Nitro attestation document (base64 decoded)
    /// @param signingPublicKey DER-encoded RSA public key for settlement signatures
    /// @param tlsCertificate DER-encoded TLS certificate for HTTPS (NEW in v1.3)
    /// @param paymentAddress Address to receive payments for this TEE
    /// @param endpoint HTTPS endpoint for the TEE service
    /// @param teeType Type identifier (must be pre-approved)
    /// @return teeId Unique identifier for the registered TEE (keccak256 of signingPublicKey)
    function registerTEEWithAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType
    ) external returns (bytes32 teeId);
    
    function deactivateTEE(bytes32 teeId) external;
    function activateTEE(bytes32 teeId) external;

    // ============ Verification ============
    
    function verifySignature(VerificationRequest calldata request) external view returns (bool valid);
    function verifySettlement(bytes32 teeId, bytes32 inputHash, bytes32 outputHash, uint256 timestamp, bytes calldata signature) external returns (bool valid);

    // ============ TEE Queries ============
    
    function getTEE(bytes32 teeId) external view returns (TEEInfo memory);
    function getActiveTEEs() external view returns (bytes32[] memory);
    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory);
    function getPublicKey(bytes32 teeId) external view returns (bytes memory);
    
    /// @notice Get TLS certificate for a registered TEE 
    /// @dev Users download this to verify HTTPS connections to the enclave
    /// @param teeId The TEE identifier
    /// @return TLS certificate in DER format
    function getTLSCertificate(bytes32 teeId) external view returns (bytes memory);
    
    function isActive(bytes32 teeId) external view returns (bool);

    // ============ Utilities ============
    
    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32);
    function computeMessageHash(bytes32 inputHash, bytes32 outputHash, uint256 timestamp) external pure returns (bytes32);
}

address constant TEE_REGISTRY = 0x0000000000000000000000000000000000000900;