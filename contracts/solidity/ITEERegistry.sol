// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ITEERegistry - Interface for TEE Registry Precompile
/// @notice Manages TEE registration and signature verification for X402 settlements
/// @dev Precompile deployed at 0x0000000000000000000000000000000000000900
/// @dev Supports Nitriding framework with dual-key verification (TLS + Signing)
interface ITEERegistry {
    
    // ============ Errors ============
    
    error TEENotFound(bytes32 teeId);
    error TEENotActive(bytes32 teeId);
    error TEEAlreadyExists(bytes32 teeId);
    error NotTEEOwner(bytes32 teeId, address caller, address owner);
    error NotAdmin(address caller);
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
    error AdminAlreadyExists(address admin);
    error AdminNotFound(address admin);
    error CannotRemoveLastAdmin();
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
    
    event AdminAdded(address indexed admin, address indexed addedBy, uint256 addedAt);
    event AdminRemoved(address indexed admin, address indexed removedBy, uint256 removedAt);
    
    event TEETypeAdded(uint8 indexed typeId, string name, uint256 addedAt);
    event TEETypeDeactivated(uint8 indexed typeId, uint256 deactivatedAt);
    
    event PCRApproved(bytes32 indexed pcrHash, string version, uint256 approvedAt, uint256 expiresAt);
    event PCRRevoked(bytes32 indexed pcrHash, uint256 revokedAt);
    
    event TEERegistered(bytes32 indexed teeId, address indexed owner, address paymentAddress, string endpoint, uint8 teeType, uint256 registeredAt);
    event TEEDeactivated(bytes32 indexed teeId, uint256 deactivatedAt);
    event TEEActivated(bytes32 indexed teeId, uint256 activatedAt);
    
    event SettlementVerified(bytes32 indexed teeId, bytes32 indexed settlementHash, address indexed caller, uint256 timestamp);
    
    event AWSRootCertificateUpdated(bytes32 indexed certificateHash, address indexed updatedBy, uint256 updatedAt);

    // ============ Admin Management ============
    
    function addAdmin(address newAdmin) external;
    function removeAdmin(address admin) external;
    function isAdmin(address account) external view returns (bool);
    function getAdmins() external view returns (address[] memory);

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
    function getTEEsByOwner(address owner) external view returns (bytes32[] memory);
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