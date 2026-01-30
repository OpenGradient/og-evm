// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ITEERegistry - Interface for TEE Registry Precompile
/// @notice Manages TEE registration and signature verification for X402 settlements
/// @dev Precompile deployed at 0x0000000000000000000000000000000000000900
interface ITEERegistry {
    
    // ============ Enums ============
    
    /// @notice Type of TEE service (admin can add new types)
    /// @dev Stored as uint8, new types added via addTEEType()
    /// Initial type: 0 = LLMProxy
*********************** where is the typeeee    
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
    
    // ============ Structs ============
    
    /// @notice PCR measurements from AWS Nitro Enclave
    /// @dev AWS Nitro produces SHA-384 (48 bytes). We store full 48 bytes.
    struct PCRMeasurements {
        bytes pcr0;  // Enclave image file hash (48 bytes)
        bytes pcr1;  // Linux kernel and bootstrap hash (48 bytes)
        bytes pcr2;  // Application hash (48 bytes)
    }
    
    /// @notice Approved PCR record
    struct ApprovedPCR {
        bytes32 pcrHash;      // keccak256(abi.encode(pcrs))
        bool active;
        uint256 approvedAt;
        uint256 expiresAt;    // 0 = no expiry (current version)
        string version;       // e.g., "v1.0.0"
    }
    
    /// @notice TEE Type definition
    struct TEETypeInfo {
        uint8 typeId;
        string name;          // e.g., "LLMProxy", "ModelServer"
        bool active;
        uint256 addedAt;
    }
    
    /// @notice Complete TEE registration record
    struct TEEInfo {
        bytes32 teeId;
        address owner;
        address paymentAddress;   // Address to receive payments
        string endpoint;          // URL (e.g., "https://3.54.4.64.5/v1/api/chat")
        bytes publicKey;          // DER-encoded RSA-2048 public key
        PCRMeasurements pcrs;
        uint8 teeType;            // Reference to TEETypeInfo
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
    
    // --- Admin Events ---
    event AdminAdded(address indexed admin, address indexed addedBy, uint256 addedAt);
    event AdminRemoved(address indexed admin, address indexed removedBy, uint256 removedAt);
    
    // --- TEE Type Events ---
    event TEETypeAdded(uint8 indexed typeId, string name, uint256 addedAt);
    event TEETypeDeactivated(uint8 indexed typeId, uint256 deactivatedAt);
    
    // --- PCR Events ---
    event PCRApproved(
        bytes32 indexed pcrHash,
        string version,
        uint256 approvedAt,
        uint256 expiresAt
    );
    event PCRRevoked(bytes32 indexed pcrHash, uint256 revokedAt);
    
    // --- TEE Events ---
    event TEERegistered(
        bytes32 indexed teeId,
        address indexed owner,
        address paymentAddress,
        string endpoint,
        uint8 teeType,
        uint256 registeredAt
    );
    event TEEDeactivated(bytes32 indexed teeId, uint256 deactivatedAt);
    event TEEActivated(bytes32 indexed teeId, uint256 activatedAt);
    
    // --- Settlement Events ---
    event SettlementVerified(
        bytes32 indexed teeId,
        bytes32 indexed settlementHash,
        address indexed caller,
        uint256 timestamp
    );
    
    // --- Certificate Events ---
    event AWSRootCertificateUpdated(
        bytes32 indexed certificateHash,
        address indexed updatedBy,
        uint256 updatedAt
    );

    // ============ Admin Management ============
    
    /// @notice Add a new admin
    /// @dev Only callable by existing admin
    /// @param newAdmin Address to add as admin
    function addAdmin(address newAdmin) external;
    
    /// @notice Remove an admin
    /// @dev Only callable by existing admin. Cannot remove last admin.
    /// @param admin Address to remove
    function removeAdmin(address admin) external;
    
    /// @notice Check if address is admin
    /// @param account Address to check
    /// @return True if address is admin
    function isAdmin(address account) external view returns (bool);
    
    /// @notice Get all admin addresses
    /// @return Array of admin addresses
    function getAdmins() external view returns (address[] memory);

    // ============ TEE Type Management ============
    
    /// @notice Add a new TEE type
    /// @dev Only callable by admin
    /// @param typeId Unique type ID
    /// @param name Human-readable name
    function addTEEType(uint8 typeId, string calldata name) external;
    
    /// @notice Deactivate a TEE type (no new registrations)
    /// @dev Only callable by admin. Existing TEEs of this type remain active.
    /// @param typeId Type to deactivate
    function deactivateTEEType(uint8 typeId) external;
    
    /// @notice Check if TEE type is valid and active
    /// @param typeId Type to check
    /// @return True if type exists and is active
    function isValidTEEType(uint8 typeId) external view returns (bool);
    
    /// @notice Get all TEE types
    /// @return Array of TEETypeInfo
    function getTEETypes() external view returns (TEETypeInfo[] memory);

    // ============ PCR Management ============
    
    /// @notice Approve new PCR and optionally set expiry on previous
    /// @dev Only callable by admin
    /// @param pcrs The PCR measurements to approve
    /// @param version Version string (e.g., "v1.2.0")
    /// @param previousPcrHash Previous PCR to set expiry on (bytes32(0) if none)
    /// @param gracePeriod Grace period for previous PCR in seconds (e.g., 7 days = 604800)
    function approvePCR(
        PCRMeasurements calldata pcrs,
        string calldata version,
        bytes32 previousPcrHash,
        uint256 gracePeriod
    ) external;
    
    /// @notice Manually revoke a PCR (immediate)
    /// @dev Only callable by admin
    /// @param pcrHash Hash of PCR to revoke
    function revokePCR(bytes32 pcrHash) external;
    
    
    /// @notice Check if PCR is currently approved (active and not expired)
    /// @param pcrs PCR measurements to check
    /// @return True if PCR is approved
    function isPCRApproved(PCRMeasurements calldata pcrs) external view returns (bool);
    
    /// @notice Get all active PCR hashes
    /// @return Array of active PCR hashes
    function getActivePCRs() external view returns (bytes32[] memory);
    
    /// @notice Get PCR details
    /// @param pcrHash Hash of PCR
    /// @return PCR details
    function getPCRDetails(bytes32 pcrHash) external view returns (ApprovedPCR memory);
    
    /// @notice Compute PCR hash
    /// @param pcrs PCR measurements
    /// @return keccak256(abi.encode(pcrs))
    function computePCRHash(PCRMeasurements calldata pcrs) external pure returns (bytes32);

    // ============ Certificate Management ============
    
    /// @notice Set AWS Nitro root certificate
    /// @dev Only callable by admin
    /// @param certificate DER-encoded AWS Nitro root certificate
    function setAWSRootCertificate(bytes calldata certificate) external;
    
    /// @notice Get AWS root certificate hash
    /// @return Hash of current root certificate
    function getAWSRootCertificateHash() external view returns (bytes32);

    // ============ TEE Registration ============
    
    /// @notice Register TEE with attestation document
    /// @dev Only callable by admin (for now). 
    ///      Verifies attestation and checks PCR is in approved list.
    /// @param attestationDocument Raw CBOR-encoded attestation from AWS Nitro
    /// @param paymentAddress Address to receive payments
    /// @param endpoint TEE endpoint URL
    /// @param teeType Type of TEE service (must be valid)
    /// @return teeId The registered TEE identifier (keccak256 of public key)
    function registerTEEWithAttestation(
        bytes calldata attestationDocument,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType
    ) external returns (bytes32 teeId);
    
    /// @notice Deactivate a TEE
    /// @dev Only callable by TEE owner or admin
    /// @param teeId TEE to deactivate
    function deactivateTEE(bytes32 teeId) external;
    
    /// @notice Reactivate a TEE
    /// @dev Only callable by TEE owner or admin
    /// @param teeId TEE to activate
    function activateTEE(bytes32 teeId) external;
    

    // ============ Verification ============
    
    /// @notice Verify TEE signature (view function, no state change)
    /// @dev Use for off-chain verification or pre-checks
    /// @param request The verification request
    /// @return valid True if signature is valid and TEE is active
    function verifySignature(
        VerificationRequest calldata request
    ) external view returns (bool valid);
    
    /// @notice Verify TEE signature and emit settlement event
    /// @dev Main function for X402 payment settlement
    /// @param teeId TEE that produced the inference
    /// @param inputHash keccak256 of inference input
    /// @param outputHash keccak256 of inference output
    /// @param timestamp Unix timestamp when inference occurred
    /// @param signature RSA-PSS signature over messageHash
    /// @return valid True if verification succeeded
    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external returns (bool valid);

    // ============ TEE Queries ============
    
    /// @notice Get complete TEE information
    /// @param teeId TEE identifier
    /// @return TEE info struct
    function getTEE(bytes32 teeId) external view returns (TEEInfo memory);
    
    /// @notice Get all active TEE IDs
    /// @return Array of active TEE IDs
    function getActiveTEEs() external view returns (bytes32[] memory);
    
    /// @notice Get all TEEs by type
    /// @param teeType Type to filter by
    /// @return Array of TEE IDs
    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory);
    
    /// @notice Get TEEs owned by an address
    /// @param owner Owner address
    /// @return Array of TEE IDs
    function getTEEsByOwner(address owner) external view returns (bytes32[] memory);
    
    /// @notice Get the public key for a TEE
    /// @param teeId TEE identifier
    /// @return DER-encoded public key
    function getPublicKey(bytes32 teeId) external view returns (bytes memory);
    
    /// @notice Check if a TEE is active
    /// @param teeId TEE identifier
    /// @return True if TEE exists and is active
    function isActive(bytes32 teeId) external view returns (bool);

    // ============ Utilities ============
    
    /// @notice Compute TEE ID from public key
    /// @param publicKey DER-encoded RSA public key
    /// @return teeId keccak256(publicKey)
    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32);
    
    /// @notice Compute message hash for signature verification
    /// @param inputHash Hash of inference input
    /// @param outputHash Hash of inference output
    /// @param timestamp Inference timestamp
    /// @return messageHash keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) external pure returns (bytes32);
}

/// @notice Precompile address constant
address constant TEE_REGISTRY = 0x0000000000000000000000000000000000000900;
