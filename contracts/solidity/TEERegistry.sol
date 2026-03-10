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
    
    // PCR Registry: teeType => pcrHash => ApprovedPCR
    mapping(uint8 => mapping(bytes32 => ApprovedPCR)) public approvedPCRs;

    struct PCRKey {
        bytes32 pcrHash;
        uint8 teeType;
    }
    PCRKey[] private _pcrList;
    
    // AWS Root Certificate
    bytes public awsRootCertificate;

    // Heartbeat: max allowed age of the signed timestamp vs block.timestamp.
    uint256 public heartbeatMaxAge = 1800; // 30 minutes default

    // All TEEs
    mapping(bytes32 => TEEInfo) public tees;

    // Active TEEs by type: teeType => list of active teeIds
    mapping(uint8 => bytes32[]) private _activeTEEList;
    // teeType => teeId => index in _activeTEEList[teeType]
    mapping(uint8 => mapping(bytes32 => uint256)) private _activeTEEIndex;

    // All TEEs by type (active + inactive)
    mapping(uint8 => bytes32[]) internal _teesByType;

    // TEEs by owner
    mapping(address => bytes32[]) internal _teesByOwner;

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
    event TEERemoved(bytes32 indexed teeId);

    // ============ Errors ============

    error TEETypeExists();
    error TEETypeNotFound();
    error InvalidTEEType();
    error PCRNotApproved();
    error PCRExpired();
    error PCRAlreadyExists();
    error TEEAlreadyExists();
    error TEENotFound();
    error TEENotActive();
    error NotTEEOwner();
    error AttestationInvalid(string reason);
    error KeyBindingFailed(string reason);
    error HeartbeatSignatureInvalid();
    error HeartbeatTimestampTooOld();
    error HeartbeatTimestampInFuture();

    // ============ Modifiers ============

    modifier onlyTEEOwnerOrAdmin(bytes32 teeId) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        if (tees[teeId].owner != msg.sender && !hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) revert NotTEEOwner();
        if (!hasRole(TEE_OPERATOR, msg.sender) && !hasRole(DEFAULT_ADMIN_ROLE, msg.sender)) revert NotTEEOwner();
        _;
    }

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
        bool isNew = approvedPCRs[teeType][pcrHash].approvedAt == 0;

        approvedPCRs[teeType][pcrHash] = ApprovedPCR({
            active: true,
            teeType: teeType,
            approvedAt: block.timestamp,
            expiresAt: 0,
            version: version
        });

        if (isNew) {
            _pcrList.push(PCRKey({pcrHash: pcrHash, teeType: teeType}));
        }

        emit PCRApproved(pcrHash, teeType, version);
    }

    /// @notice Revoke a PCR, either immediately or with a grace period
    /// @dev TEEs using this PCR are caught lazily at activateTEE() and heartbeat()
    /// @param pcrHash The PCR hash to revoke
    /// @param gracePeriod Seconds until revocation takes effect (0 = immediate)
    function revokePCR(bytes32 pcrHash, uint8 teeType, uint256 gracePeriod) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!isPCRApproved(teeType, pcrHash)) revert PCRNotApproved();

        if (gracePeriod == 0) {
            approvedPCRs[teeType][pcrHash].active = false;
        } else {
            approvedPCRs[teeType][pcrHash].expiresAt = block.timestamp + gracePeriod;
        }
        emit PCRRevoked(pcrHash, gracePeriod);
    }

    /// @notice Check if a PCR is currently approved and not expired
    /// @param teeType The TEE type the PCR is valid for
    /// @param pcrHash The PCR hash to check
    /// @return bool True if approved and not expired
    function isPCRApproved(uint8 teeType, bytes32 pcrHash) public view returns (bool) {
        ApprovedPCR storage pcr = approvedPCRs[teeType][pcrHash];
        if (!pcr.active) return false;
        if (pcr.expiresAt != 0 && block.timestamp >= pcr.expiresAt) return false;
        return true;
    }

    /// @dev Reverts if PCR is not approved for the given TEE type
    function _requirePCRValidForTEE(bytes32 pcrHash, uint8 teeType) private view {
        ApprovedPCR storage pcr = approvedPCRs[teeType][pcrHash];
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
    /// @return PCRKey[] Array of active PCR keys (pcrHash + teeType)
    function getActivePCRs() external view returns (PCRKey[] memory) {
        uint256 count = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (isPCRApproved(_pcrList[i].teeType, _pcrList[i].pcrHash)) count++;
        }

        PCRKey[] memory result = new PCRKey[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (isPCRApproved(_pcrList[i].teeType, _pcrList[i].pcrHash)) {
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

    // ============ TEE Management ============
    
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
        if (!valid) revert AttestationInvalid("Attestation document verification failed");

        // Verify PCR is approved and matches the TEE type
        _requirePCRValidForTEE(pcrHash, teeType);

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
        _activeTEEIndex[teeType][teeId] = _activeTEEList[teeType].length;
        _activeTEEList[teeType].push(teeId);
        _teesByType[teeType].push(teeId);
        _teesByOwner[msg.sender].push(teeId);

        emit TEERegistered(teeId, msg.sender, teeType);
    }

    /// @notice Deactivate a TEE, removing it from the active list
    /// @dev Requires caller to be the TEE owner with TEE_OPERATOR role, or an admin
    /// @param teeId The TEE identifier to deactivate
    function deactivateTEE(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        TEEInfo storage tee = tees[teeId];
        if (!tee.active) return;

        tee.active = false;
        _removeFromActiveList(teeId, tee.teeType);
        emit TEEDeactivated(teeId);
    }

    /// @notice Re-activate a previously deactivated TEE
    /// @dev Requires caller to be the TEE owner with TEE_OPERATOR role, or an admin.
    ///      Also re-validates that the TEE's PCR is still approved for its type.
    /// @param teeId The TEE identifier to activate
    function activateTEE(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        TEEInfo storage tee = tees[teeId];
        // Make sure to do an early return here in order to prevent
        // getting around the heartbeat check which relies on lastUpdatedAt.
        if (tee.active) return;

        _requirePCRValidForTEE(tee.pcrHash, tee.teeType);

        tee.active = true;
        _addToActiveList(teeId, tee.teeType);
        emit TEEActivated(teeId);
    }

    /// @notice Permanently remove a TEE from all storage
    /// @dev Callable by TEE owner (with TEE_OPERATOR role) or admin.
    ///      Use to clean up decommissioned or upgraded TEEs and reclaim storage.
    /// @param teeId The TEE identifier to remove
    function removeTEE(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        TEEInfo storage tee = tees[teeId];

        uint8 teeType = tee.teeType;
        address owner = tee.owner;

        // Remove from active list if active
        if (tee.active) {
            _removeFromActiveList(teeId, teeType);
        }

        // Remove from _teesByType
        _removeFromArray(_teesByType[teeType], teeId);

        // Remove from _teesByOwner
        _removeFromArray(_teesByOwner[owner], teeId);

        // Delete TEE data
        delete tees[teeId];

        emit TEERemoved(teeId);
    }

    function _addToActiveList(bytes32 teeId, uint8 teeType) private {
        _activeTEEIndex[teeType][teeId] = _activeTEEList[teeType].length;
        _activeTEEList[teeType].push(teeId);
    }

    function _removeFromActiveList(bytes32 teeId, uint8 teeType) private {
        uint256 index = _activeTEEIndex[teeType][teeId];
        uint256 lastIndex = _activeTEEList[teeType].length - 1;

        if (index != lastIndex) {
            bytes32 lastTeeId = _activeTEEList[teeType][lastIndex];
            _activeTEEList[teeType][index] = lastTeeId;
            _activeTEEIndex[teeType][lastTeeId] = index;
        }

        _activeTEEList[teeType].pop();
        delete _activeTEEIndex[teeType][teeId];
    }

    /// @dev Swap-and-pop removal from an unordered bytes32 array
    function _removeFromArray(bytes32[] storage arr, bytes32 value) private {
        for (uint256 i = 0; i < arr.length; i++) {
            if (arr[i] == value) {
                arr[i] = arr[arr.length - 1];
                arr.pop();
                return;
            }
        }
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

        // Lazy PCR enforcement (validity + type match)
        _requirePCRValidForTEE(tee.pcrHash, tee.teeType);

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

    // ============ Queries ============

    /// @notice Get full TEE info by ID
    /// @param teeId The TEE identifier
    /// @return The TEE info struct
    function getTEE(bytes32 teeId) external view returns (TEEInfo memory) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        return tees[teeId];
    }

    /// @notice Get TEE IDs that have been activated for a given type
    /// @dev Does NOT filter by heartbeat freshness or PCR validity.
    ///      Use getLiveTEEs() for fully verified results.
    /// @param teeType The TEE type to query
    /// @return Array of TEE IDs
    function getActivatedTEEs(uint8 teeType) external view returns (bytes32[] memory) {
        return _activeTEEList[teeType];
    }

    /// @notice Get TEEs that are activated, have a valid PCR, and a fresh heartbeat
    /// @dev More expensive than getActivatedTEEs() due to on-chain filtering.
    ///      Use this when you need guaranteed-healthy TEEs without client-side checks.
    /// @param teeType The TEE type to query
    /// @return Array of TEEInfo structs for live TEEs
    function getLiveTEEs(uint8 teeType) external view returns (TEEInfo[] memory) {
        bytes32[] storage list = _activeTEEList[teeType];
        uint256 count = 0;
        for (uint256 i = 0; i < list.length; i++) {
            if (_isLive(tees[list[i]])) count++;
        }

        TEEInfo[] memory result = new TEEInfo[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < list.length; i++) {
            if (_isLive(tees[list[i]])) {
                result[j++] = tees[list[i]];
            }
        }
        return result;
    }

    /// @notice Get all TEE IDs (active and inactive) for a given type
    /// @param teeType The TEE type to query
    /// @return Array of TEE IDs
    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory) {
        return _teesByType[teeType];
    }

    /// @notice Get all TEE IDs owned by an address
    /// @param owner The owner address to query
    /// @return Array of TEE IDs
    function getTEEsByOwner(address owner) external view returns (bytes32[] memory) {
        return _teesByOwner[owner];
    }

    /// @notice Check if a TEE is currently active
    function isActive(bytes32 teeId) external view returns (bool) {
        return tees[teeId].active;
    }

    /// @notice Check if a TEE is live (active + valid PCR + fresh heartbeat)
    function isLive(bytes32 teeId) external view returns (bool) {
        return _isLive(tees[teeId]);
    }

    function _isLive(TEEInfo storage tee) private view returns (bool) {
        if (!tee.active) return false;
        if (block.timestamp - tee.lastUpdatedAt > heartbeatMaxAge) return false;
        if (!isPCRApproved(tee.teeType, tee.pcrHash)) return false;
        return true;
    }

    /// @notice Get a TEE's public key
    function getPublicKey(bytes32 teeId) external view returns (bytes memory) {
        return tees[teeId].publicKey;
    }

    /// @notice Compute TEE ID from its public key
    /// @param publicKey The TEE's public key
    /// @return The TEE identifier (keccak256 hash)
    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32) {
        return keccak256(publicKey);
    }
}