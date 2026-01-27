// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ITEERegistry - Interface for TEE Registry Precompile
/// @notice Manages TEE registration and signature verification for X402 settlements
/// @dev Precompile deployed at 0x0000000000000000000000000000000000000900
interface ITEERegistry {
    
    // ============ Errors ============
    
    error TEENotFound(bytes32 teeId);
    error TEENotActive(bytes32 teeId);
    error TEEAlreadyExists(bytes32 teeId);
    error NotTEEOwner(bytes32 teeId, address caller, address owner);
    error NotAuthorized(address caller);
    error InvalidSignature();
    error InvalidPublicKey();
    error InvalidAttestation();
    error InvalidAWSSignature();
    error PCRMismatch();
    error TimestampTooOld(uint256 timestamp, uint256 maxAge);
    error TimestampInFuture(uint256 timestamp);
    error RootCertificateNotSet();
    
    // ============ Structs ============
    
    /// @notice PCR measurements from AWS Nitro Enclave
    /// @dev AWS Nitro produces SHA-384 (48 bytes). We store first 32 bytes only.
    ///      Facilitator must truncate PCR values before registration/comparison.
    ///      Security note: 256 bits provides sufficient collision resistance.
    /// TODO: Confirm with Kyle if truncation is acceptable
    struct PCRMeasurements {
        bytes32 pcr0;  // Enclave image file hash
        bytes32 pcr1;  // Linux kernel and bootstrap hash
        bytes32 pcr2;  // Application hash
    }
    
    /// @notice Complete TEE registration record
    struct TEEInfo {
        bytes32 teeId;
        address owner;
        bytes publicKey;        // DER-encoded RSA-2048 public key
        PCRMeasurements pcrs;
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
        bytes signature;        // RSA-PSS signature with SHA-256
    }
    
    // ============ Events ============
    
    event TEERegistered(
        bytes32 indexed teeId,
        address indexed owner,
        bytes publicKey,
        uint256 registeredAt
    );
    
    event TEERegisteredWithAttestation(
        bytes32 indexed teeId,
        address indexed owner,
        bytes32 attestationHash,
        uint256 registeredAt
    );
    
    event TEEUpdated(
        bytes32 indexed teeId,
        PCRMeasurements oldPcrs,
        PCRMeasurements newPcrs,
        uint256 updatedAt
    );
    
    event TEEStatusChanged(
        bytes32 indexed teeId,
        bool active,
        uint256 changedAt
    );
    
    event SettlementVerified(
        bytes32 indexed teeId,
        bytes32 indexed settlementHash,
        address indexed caller,
        uint256 timestamp
    );
    
    event AWSRootCertificateUpdated(
        bytes32 indexed certificateHash,
        address indexed updatedBy,
        uint256 updatedAt
    );

    // ============ Attestation Verification ============

    /// @notice Set AWS Nitro root certificate for attestation verification
    /// @dev CRITICAL: Only callable by governance/admin. This is the trust anchor.
    /// @param certificate DER-encoded AWS Nitro root certificate
    function setAWSRootCertificate(bytes calldata certificate) external;

    /// @notice Register TEE with full attestation document verification
    /// @dev Verifies AWS signature and PCR values before registration
    /// @param attestationDocument Raw CBOR-encoded attestation from AWS Nitro
    /// @param expectedPcrs PCR values we expect (for the correct enclave image)
    /// @return teeId The registered TEE identifier
    function registerTEEWithAttestation(
        bytes calldata attestationDocument,
        PCRMeasurements calldata expectedPcrs
    ) external returns (bytes32 teeId);

    /// @notice Verify an attestation document without registering
    /// @param attestationDocument Raw CBOR-encoded attestation from AWS Nitro
    /// @param expectedPcrs PCR values we expect
    /// @return valid True if attestation is valid and PCRs match
    /// @return publicKey The public key from the attestation (if valid)
    function verifyAttestation(
        bytes calldata attestationDocument,
        PCRMeasurements calldata expectedPcrs
    ) external view returns (bool valid, bytes memory publicKey);

    // ============ Registration ============
    
    /// @notice Register a new TEE (trusted registration without attestation)
    /// @dev Use registerTEEWithAttestation() for trustless registration
    /// @param publicKey DER-encoded RSA-2048 public key
    /// @param pcrs Initial PCR measurements from attestation document
    /// @return teeId Unique identifier (keccak256 of publicKey)
    function registerTEE(
        bytes calldata publicKey,
        PCRMeasurements calldata pcrs
    ) external returns (bytes32 teeId);
    
    /// @notice Update PCR measurements after enclave rebuild
    /// @dev Only callable by TEE owner
    /// @param teeId The TEE to update
    /// @param newPcrs New PCR measurements
    function updatePCRs(
        bytes32 teeId,
        PCRMeasurements calldata newPcrs
    ) external;
    
    /// @notice Deactivate a TEE (emergency kill switch)
    /// @dev Only callable by TEE owner
    /// @param teeId The TEE to deactivate
    function deactivateTEE(bytes32 teeId) external;
    
    /// @notice Reactivate a previously deactivated TEE
    /// @dev Only callable by TEE owner
    /// @param teeId The TEE to reactivate
    function activateTEE(bytes32 teeId) external;
    
    // ============ Verification ============
    
    /// @notice Verify TEE signature without emitting event
    /// @dev Use for off-chain verification or checks before settlement
    /// @param request The verification request containing all parameters
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
    
    // ============ Queries ============
    
    /// @notice Get complete TEE information
    function getTEE(bytes32 teeId) external view returns (TEEInfo memory);
    
    /// @notice Get the public key for a TEE
    function getPublicKey(bytes32 teeId) external view returns (bytes memory);
    
    /// @notice Check if a TEE is active
    function isActive(bytes32 teeId) external view returns (bool);
    
    /// @notice Get PCR measurements for a TEE
    function getPCRs(bytes32 teeId) external view returns (PCRMeasurements memory);
    
    /// @notice Get all TEEs owned by an address
    function getTEEsByOwner(address owner) external view returns (bytes32[] memory);
    
    /// @notice Get the current AWS root certificate hash
    function getAWSRootCertificateHash() external view returns (bytes32);
    
    // ============ Utilities ============
    
    /// @notice Compute TEE ID from public key
    /// @param publicKey The RSA public key
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