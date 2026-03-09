// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./precompiles/tee/ITEEVerifier.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";

/// @title TEERegistry - TEE Registration and Management
/// @notice Manages TEE lifecycle, calls precompile only for crypto
/// @dev All storage in Solidity, crypto in precompile at 0x900
contract TEERegistry is AccessControl {
    
    // ============ Constants ============

    bytes32 public constant TEE_OPERATOR = keccak256("TEE_OPERATOR");
    ITEEVerifier public constant VERIFIER = ITEEVerifier(0x0000000000000000000000000000000000000900);

    // ============ Structs ============
    
    struct PCRMeasurements {
        bytes pcr0;
        bytes pcr1;
        bytes pcr2;
    }

    struct ApprovedPCR {
        bool active;
        uint8 teeType;
        uint256 approvedAt;
        uint256 expiresAt;
        string version;
    }

    struct TEETypeInfo {
        string name;
        bool active;
        uint256 addedAt;
    }

    struct TEEInfo {
        address owner;
        address paymentAddress;
        string endpoint;
        bytes publicKey;
        bytes tlsCertificate;
        bytes32 pcrHash;
        uint8 teeType;
        bool active;
        uint256 registeredAt;
        uint256 lastUpdatedAt;
    }

    // ============ Storage ============

    // TEE Types
    mapping(uint8 => TEETypeInfo) public teeTypes;
    uint8[] private _teeTypeList;
    
    // PCR Registry
    mapping(bytes32 => ApprovedPCR) public approvedPCRs;
    bytes32[] private _pcrList;
    
    // AWS Root Certificate
    bytes public awsRootCertificate;

    // Heartbeat: max allowed age of the signed timestamp vs block.timestamp.
    uint256 public heartbeatMaxAge = 1800; // 30 minutes default

    // TEE Storage
    mapping(bytes32 => TEEInfo) public tees;
    bytes32[] private _activeTEEList;
    mapping(bytes32 => uint256) private _activeTEEIndex;
    mapping(address => bytes32[]) private _teesByOwner;
    mapping(uint8 => bytes32[]) private _teesByType;

    // ============ Events ============

    event TEETypeAdded(uint8 indexed typeId, string name);
    event TEETypeDeactivated(uint8 indexed typeId);
    event PCRApproved(bytes32 indexed pcrHash, uint8 indexed teeType, string version);
    event PCRRevoked(bytes32 indexed pcrHash, uint256 gracePeriod);
    event TEERegistered(bytes32 indexed teeId, address indexed owner, uint8 teeType);
    event TEEDeactivated(bytes32 indexed teeId);
    event TEEActivated(bytes32 indexed teeId);
    event AWSCertificateUpdated(bytes32 indexed certHash);
    event HeartbeatReceived(bytes32 indexed teeId, uint256 timestamp);

    // ============ Errors ============

    error TEETypeExists();
    error TEETypeNotFound();
    error InvalidTEEType();
    error PCRNotApproved();
    error PCRExpired();
    error PCRAlreadyExists();
    error PCRTypeMismatch();
    error TEEAlreadyExists();
    error TEENotFound();
    error TEENotActive();
    error NotTEEOwner();
    error AttestationInvalid(string reason);
    error KeyBindingFailed(string reason);
    error HeartbeatSignatureInvalid();
    error HeartbeatTimestampTooOld();
    error HeartbeatTimestampInFuture();

    // ============ Constructor ============

    constructor() {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(TEE_OPERATOR, msg.sender);
        _setRoleAdmin(TEE_OPERATOR, DEFAULT_ADMIN_ROLE);
    }

    // ============ TEE Type Management ============
    
    function addTEEType(uint8 typeId, string calldata name) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (teeTypes[typeId].addedAt != 0) revert TEETypeExists();
        teeTypes[typeId] = TEETypeInfo({
            name: name,
            active: true,
            addedAt: block.timestamp
        });
        _teeTypeList.push(typeId);
        emit TEETypeAdded(typeId, name);
    }

    function deactivateTEEType(uint8 typeId) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (teeTypes[typeId].addedAt == 0) revert TEETypeNotFound();
        teeTypes[typeId].active = false;
        emit TEETypeDeactivated(typeId);
    }

    function isValidTEEType(uint8 typeId) public view returns (bool) {
        return teeTypes[typeId].active;
    }

    function getTEETypes() external view returns (uint8[] memory typeIds, TEETypeInfo[] memory infos) {
        typeIds = _teeTypeList;
        infos = new TEETypeInfo[](_teeTypeList.length);
        for (uint256 i = 0; i < _teeTypeList.length; i++) {
            infos[i] = teeTypes[_teeTypeList[i]];
        }
    }

    // ============ PCR Management ============
    
    /// @notice Approve a new PCR measurement for a specific TEE type
    /// @param pcrs The PCR measurements (pcr0, pcr1, pcr2)
    /// @param version Human-readable version string (e.g., "v1.2.0")
    /// @param teeType The TEE type this PCR is valid for
    function approvePCR(
        PCRMeasurements calldata pcrs,
        string calldata version,
        uint8 teeType
    ) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!isValidTEEType(teeType)) revert InvalidTEEType();

        bytes32 pcrHash = computePCRHash(pcrs);

        // Allow re-approval of revoked/expired PCRs, but not currently active ones
        if (isPCRApproved(pcrHash)) revert PCRAlreadyExists();

        bool isNew = approvedPCRs[pcrHash].approvedAt == 0;

        approvedPCRs[pcrHash] = ApprovedPCR({
            active: true,
            teeType: teeType,
            approvedAt: block.timestamp,
            expiresAt: 0,
            version: version
        });

        if (isNew) {
            _pcrList.push(pcrHash);
        }

        emit PCRApproved(pcrHash, teeType, version);
    }

    /// @notice Revoke a PCR, either immediately or with a grace period
    /// @dev TEEs using this PCR are caught lazily at activateTEE() and heartbeat()
    /// @param pcrHash The PCR hash to revoke
    /// @param gracePeriod Seconds until revocation takes effect (0 = immediate)
    function revokePCR(bytes32 pcrHash, uint256 gracePeriod) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (gracePeriod == 0) {
            approvedPCRs[pcrHash].active = false;
        } else {
            approvedPCRs[pcrHash].expiresAt = block.timestamp + gracePeriod;
        }
        emit PCRRevoked(pcrHash, gracePeriod);
    }

    /// @notice Check if a PCR is currently approved and not expired
    /// @param pcrHash The PCR hash to check
    /// @return bool True if approved and not expired
    function isPCRApproved(bytes32 pcrHash) public view returns (bool) {
        ApprovedPCR storage pcr = approvedPCRs[pcrHash];
        if (!pcr.active) return false;
        if (pcr.expiresAt != 0 && block.timestamp >= pcr.expiresAt) return false;
        return true;
    }

    /// @dev Reverts with specific error for expired vs revoked/unknown PCRs
    function _requirePCRApproved(bytes32 pcrHash) private view {
        ApprovedPCR storage pcr = approvedPCRs[pcrHash];
        if (!pcr.active) revert PCRNotApproved();
        if (pcr.expiresAt != 0 && block.timestamp >= pcr.expiresAt) revert PCRExpired();
    }

    /// @notice Compute PCR hash from measurements
    /// @param pcrs The PCR measurements
    /// @return bytes32 Hash of the concatenated PCRs
    function computePCRHash(PCRMeasurements calldata pcrs) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));
    }

    /// @notice Get all currently active (approved and not expired) PCRs
    /// @return bytes32[] Array of active PCR hashes
    function getActivePCRs() external view returns (bytes32[] memory) {
        uint256 count = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (isPCRApproved(_pcrList[i])) count++;
        }
        
        bytes32[] memory result = new bytes32[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (isPCRApproved(_pcrList[i])) {
                result[j++] = _pcrList[i];
            }
        }
        return result;
    }

    // ============ Certificate Management ============
    
    function setAWSRootCertificate(bytes calldata certificate) external onlyRole(DEFAULT_ADMIN_ROLE) {
        awsRootCertificate = certificate;
        emit AWSCertificateUpdated(keccak256(certificate));
    }

    // ============ TEE Registration ============
    
    function registerTEEWithAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType
    ) external onlyRole(TEE_OPERATOR) returns (bytes32 teeId) {
        // Validate TEE type
        if (!isValidTEEType(teeType)) revert InvalidTEEType();

        // Compute TEE ID
        teeId = keccak256(signingPublicKey);
        if (tees[teeId].registeredAt != 0) revert TEEAlreadyExists();

        // Verify attestation via precompile (v2 API with dual-key binding)
        (bool valid, bytes32 pcrHash) = VERIFIER.verifyAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            awsRootCertificate
        );
        if (!valid) revert AttestationInvalid("Attestation verification failed");

        // Verify PCR is approved and matches the TEE type
        if (!isPCRApproved(pcrHash)) revert PCRNotApproved();
        if (approvedPCRs[pcrHash].teeType != teeType) revert PCRTypeMismatch();

        // Store TEE
        tees[teeId] = TEEInfo({
            owner: msg.sender,
            paymentAddress: paymentAddress,
            endpoint: endpoint,
            publicKey: signingPublicKey,
            tlsCertificate: tlsCertificate,
            pcrHash: pcrHash,
            teeType: teeType,
            active: true,
            registeredAt: block.timestamp,
            lastUpdatedAt: block.timestamp
        });

        // Add to indexes
        _activeTEEIndex[teeId] = _activeTEEList.length;
        _activeTEEList.push(teeId);
        _teesByOwner[msg.sender].push(teeId);
        _teesByType[teeType].push(teeId);

        emit TEERegistered(teeId, msg.sender, teeType);
    }

    // ============ TEE Management ============
    
    function deactivateTEE(bytes32 teeId) external {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();
        if (tee.owner != msg.sender && !hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) revert NotTEEOwner();
        if (!tee.active) return;

        tee.active = false;
        tee.lastUpdatedAt = block.timestamp;
        _removeFromActiveList(teeId);
        emit TEEDeactivated(teeId);
    }

    function activateTEE(bytes32 teeId) external {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();
        if (tee.owner != msg.sender && !hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) revert NotTEEOwner();
        if (tee.active) return;

        _requirePCRApproved(tee.pcrHash);

        tee.active = true;
        tee.lastUpdatedAt = block.timestamp;
        _addToActiveList(teeId);
        emit TEEActivated(teeId);
    }

    function _addToActiveList(bytes32 teeId) private {
        _activeTEEIndex[teeId] = _activeTEEList.length;
        _activeTEEList.push(teeId);
    }

    function _removeFromActiveList(bytes32 teeId) private {
        uint256 index = _activeTEEIndex[teeId];
        uint256 lastIndex = _activeTEEList.length - 1;
        
        if (index != lastIndex) {
            bytes32 lastTeeId = _activeTEEList[lastIndex];
            _activeTEEList[index] = lastTeeId;
            _activeTEEIndex[lastTeeId] = index;
        }
        
        _activeTEEList.pop();
        delete _activeTEEIndex[teeId];
    }

    // ============ Heartbeat ============

    /// @notice Submit a signed heartbeat for a registered TEE.
    /// @dev Signature is RSA-PSS-SHA256 over keccak256(abi.encodePacked(teeId, timestamp)).
    ///      Anyone can relay the tx, but only the TEE holding the RSA private key
    ///      can produce a valid signature.
    /// @param teeId     - The registered TEE identifier (keccak256 of its public key).
    /// @param timestamp  - Unix timestamp included in the signed payload.
    /// @param signature  - RSA-PSS signature bytes.
    function heartbeat(
        bytes32 teeId,
        uint256 timestamp,
        bytes calldata signature
    ) external {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();
        if (!tee.active) revert TEENotActive();

        // Lazy PCR enforcement
        _requirePCRApproved(tee.pcrHash);

        // Reject stale or future signed timestamps
        if (timestamp > block.timestamp) revert HeartbeatTimestampInFuture();
        if (block.timestamp - timestamp > heartbeatMaxAge) revert HeartbeatTimestampTooOld();

        // Verify RSA-PSS signature using the TEE's stored public key
        bytes32 messageHash = keccak256(abi.encodePacked(teeId, timestamp));
        bool valid = VERIFIER.verifyRSAPSS(tee.publicKey, messageHash, signature);
        if (!valid) revert HeartbeatSignatureInvalid();

        tee.lastUpdatedAt = block.timestamp;
        emit HeartbeatReceived(teeId, timestamp);
    }

    /// @notice Update the max allowed age for heartbeat timestamps
    function setHeartbeatMaxAge(uint256 maxAge) external onlyRole(DEFAULT_ADMIN_ROLE) {
        heartbeatMaxAge = maxAge;
    }

    // ============ Utilities ============

    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
    }

    // ============ Queries ============
    
    function getTEE(bytes32 teeId) external view returns (TEEInfo memory) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        return tees[teeId];
    }

    function getActiveTEEs() external view returns (bytes32[] memory) {
        return _activeTEEList;
    }

    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory) {
        return _teesByType[teeType];
    }

    function getTEEsByOwner(address owner) external view returns (bytes32[] memory) {
        return _teesByOwner[owner];
    }

    function getPublicKey(bytes32 teeId) external view returns (bytes memory) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        return tees[teeId].publicKey;
    }

    function getTLSCertificate(bytes32 teeId) external view returns (bytes memory) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        return tees[teeId].tlsCertificate;
    }

    function isActive(bytes32 teeId) external view returns (bool) {
        return tees[teeId].active;
    }

    function getPaymentAddress(bytes32 teeId) external view returns (address) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        return tees[teeId].paymentAddress;
    }

    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32) {
        return keccak256(publicKey);
    }
}