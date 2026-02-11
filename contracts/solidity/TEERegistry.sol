// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "./ITEERegistry.sol";
import "./precompiles/attestation/IAttestationVerifier.sol";
import "./precompiles/rsa/IRSAVerifier.sol";

/// @title TEERegistry - Trusted Execution Environment Registry
/// @notice Manages TEE registration and signature verification for X402 settlements
/// @dev Delegates crypto-heavy operations to precompiles, handles all business logic in Solidity
contract TEERegistry is ITEERegistry, AccessControl {

    // ============ Precompile Addresses ============

    /// @dev Attestation verification precompile
    address constant ATTESTATION_VERIFIER = 0x0000000000000000000000000000000000000901;

    /// @dev RSA-PSS signature verification precompile
    address constant RSA_VERIFIER = 0x0000000000000000000000000000000000000902;

    // ============ Access Control ============

    /// @dev Role for protocol-level TEE management operations
    bytes32 public constant TEE_ADMIN_ROLE = keccak256("TEE_ADMIN_ROLE");

    /// @dev Role for TEE operators who can register and manage their own TEEs
    bytes32 public constant TEE_OPERATOR_ROLE = keccak256("TEE_OPERATOR_ROLE");

    // ============ State Variables ============

    /// @dev TEE type registry
    mapping(uint8 => TEETypeInfo) private _teeTypes;
    uint8[] private _teeTypeList;

    /// @dev PCR approval registry
    mapping(bytes32 => ApprovedPCR) private _approvedPCRs;
    bytes32[] private _pcrList;

    /// @dev TEE registry
    mapping(bytes32 => TEEInfo) private _tees;
    bytes32[] private _activeTEEList;
    mapping(bytes32 => uint256) private _activeTEEIndex; // For O(1) removal

    /// @dev TEE indexes
    mapping(uint8 => bytes32[]) private _teesByType;

    /// @dev Settlement replay protection
    mapping(bytes32 => bool) private _usedSettlements;

    /// @dev AWS root certificate for attestation verification
    bytes private _awsRootCertificate;

    /// @dev Settlement timestamp validation constants
    uint256 constant MAX_SETTLEMENT_AGE = 3600; // 1 hour
    uint256 constant FUTURE_TIME_TOLERANCE = 300; // 5 minutes

    // ============ Constructor ============

    constructor() {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(TEE_ADMIN_ROLE, msg.sender);
    }

    // ============ Modifiers ============

    modifier teeExists(bytes32 teeId) {
        if (_tees[teeId].owner == address(0)) {
            revert TEENotFound(teeId);
        }
        _;
    }

    modifier onlyTEEOwnerOrAdmin(bytes32 teeId) {
        if (_tees[teeId].owner != msg.sender && !hasRole(TEE_ADMIN_ROLE, msg.sender)) {
            revert NotTEEOwner(teeId, msg.sender, _tees[teeId].owner);
        }
        _;
    }

    modifier onlyOperatorOrAdmin() {
        require(
            hasRole(TEE_OPERATOR_ROLE, msg.sender) || hasRole(TEE_ADMIN_ROLE, msg.sender),
            "Caller must be operator or admin"
        );
        _;
    }

    // ============ TEE Type Management ============

    /// @inheritdoc ITEERegistry
    function addTEEType(uint8 typeId, string calldata name) external onlyRole(TEE_ADMIN_ROLE) {
        if (_teeTypes[typeId].addedAt != 0) {
            revert TEETypeExists(typeId);
        }

        _teeTypes[typeId] = TEETypeInfo({
            typeId: typeId,
            name: name,
            active: true,
            addedAt: block.timestamp
        });

        _teeTypeList.push(typeId);

        emit TEETypeAdded(typeId, name, block.timestamp);
    }

    /// @inheritdoc ITEERegistry
    function deactivateTEEType(uint8 typeId) external onlyRole(TEE_ADMIN_ROLE) {
        if (_teeTypes[typeId].addedAt == 0) {
            revert InvalidTEEType(typeId);
        }

        _teeTypes[typeId].active = false;

        emit TEETypeDeactivated(typeId, block.timestamp);
    }

    /// @inheritdoc ITEERegistry
    function isValidTEEType(uint8 typeId) external view returns (bool) {
        return _teeTypes[typeId].active;
    }

    /// @inheritdoc ITEERegistry
    function getTEETypes() external view returns (TEETypeInfo[] memory) {
        TEETypeInfo[] memory types = new TEETypeInfo[](_teeTypeList.length);
        for (uint256 i = 0; i < _teeTypeList.length; i++) {
            types[i] = _teeTypes[_teeTypeList[i]];
        }
        return types;
    }

    // ============ PCR Management ============

    /// @inheritdoc ITEERegistry
    function approvePCR(
        PCRMeasurements calldata pcrs,
        string calldata version,
        bytes32 previousPcrHash,
        uint256 gracePeriod
    ) external onlyRole(TEE_ADMIN_ROLE) {
        bytes32 pcrHash = computePCRHash(pcrs);

        // Set expiry on previous PCR if provided
        if (previousPcrHash != bytes32(0)) {
            if (_approvedPCRs[previousPcrHash].approvedAt == 0) {
                revert TEENotFound(previousPcrHash);
            }
            _approvedPCRs[previousPcrHash].expiresAt = block.timestamp + gracePeriod;
        }

        // Approve new PCR (no expiry by default)
        _approvedPCRs[pcrHash] = ApprovedPCR({
            pcrHash: pcrHash,
            active: true,
            approvedAt: block.timestamp,
            expiresAt: 0,
            version: version
        });

        _pcrList.push(pcrHash);

        emit PCRApproved(pcrHash, version, block.timestamp, 0);
    }

    /// @inheritdoc ITEERegistry
    function revokePCR(bytes32 pcrHash) external onlyRole(TEE_ADMIN_ROLE) {
        if (_approvedPCRs[pcrHash].approvedAt == 0) {
            revert TEENotFound(pcrHash);
        }

        _approvedPCRs[pcrHash].active = false;

        emit PCRRevoked(pcrHash, block.timestamp);
    }

    /// @inheritdoc ITEERegistry
    function isPCRApproved(PCRMeasurements calldata pcrs) external view returns (bool) {
        bytes32 pcrHash = computePCRHash(pcrs);
        return _isPCRApprovedInternal(pcrHash);
    }

    /// @inheritdoc ITEERegistry
    function getActivePCRs() external view returns (bytes32[] memory) {
        uint256 activeCount = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (_isPCRApprovedInternal(_pcrList[i])) {
                activeCount++;
            }
        }

        bytes32[] memory activePCRs = new bytes32[](activeCount);
        uint256 j = 0;
        for (uint256 i = 0; i < _pcrList.length; i++) {
            if (_isPCRApprovedInternal(_pcrList[i])) {
                activePCRs[j] = _pcrList[i];
                j++;
            }
        }

        return activePCRs;
    }

    /// @inheritdoc ITEERegistry
    function getPCRDetails(bytes32 pcrHash) external view returns (ApprovedPCR memory) {
        if (_approvedPCRs[pcrHash].approvedAt == 0) {
            revert TEENotFound(pcrHash);
        }
        return _approvedPCRs[pcrHash];
    }

    /// @inheritdoc ITEERegistry
    function computePCRHash(PCRMeasurements calldata pcrs) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));
    }

    // ============ Certificate Management ============

    /// @inheritdoc ITEERegistry
    function setAWSRootCertificate(bytes calldata certificate) external onlyRole(TEE_ADMIN_ROLE) {
        if (certificate.length == 0) {
            revert RootCertificateNotSet();
        }

        _awsRootCertificate = certificate;

        emit AWSRootCertificateUpdated(keccak256(certificate), msg.sender, block.timestamp);
    }

    /// @inheritdoc ITEERegistry
    function getAWSRootCertificateHash() external view returns (bytes32) {
        if (_awsRootCertificate.length == 0) {
            return bytes32(0);
        }
        return keccak256(_awsRootCertificate);
    }

    // ============ TEE Registration ============

    /// @inheritdoc ITEERegistry
    function registerTEEWithAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType
    ) external onlyOperatorOrAdmin returns (bytes32 teeId) {
        // Validate TEE type
        if (!_teeTypes[teeType].active) {
            revert InvalidTEEType(teeType);
        }

        // Call precompile to verify attestation and extract data
        (bool valid, bytes32 pcrHash) = IAttestationVerifier(ATTESTATION_VERIFIER).verifyAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            _awsRootCertificate
        );

        if (!valid) {
            revert InvalidAttestation();
        }

        // Verify PCR is approved
        if (!_isPCRApprovedInternal(pcrHash)) {
            revert PCRNotApproved();
        }

        // Compute TEE ID from signing public key
        teeId = keccak256(signingPublicKey);

        // Check if already exists
        if (_tees[teeId].owner != address(0)) {
            revert TEEAlreadyExists(teeId);
        }

        // Store TEE info
        _tees[teeId] = TEEInfo({
            teeId: teeId,
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

        // Add to active list
        _activeTEEIndex[teeId] = _activeTEEList.length;
        _activeTEEList.push(teeId);

        // Add to type list
        _teesByType[teeType].push(teeId);

        emit TEERegistered(teeId, msg.sender, paymentAddress, endpoint, teeType, block.timestamp);

        return teeId;
    }

    /// @inheritdoc ITEERegistry
    function deactivateTEE(bytes32 teeId) external teeExists(teeId) onlyTEEOwnerOrAdmin(teeId) {
        if (!_tees[teeId].active) {
            return; // Already inactive
        }

        _tees[teeId].active = false;
        _tees[teeId].lastUpdatedAt = block.timestamp;

        // Remove from active list
        _removeFromActiveList(teeId);

        emit TEEDeactivated(teeId, block.timestamp);
    }

    /// @inheritdoc ITEERegistry
    function activateTEE(bytes32 teeId) external teeExists(teeId) onlyTEEOwnerOrAdmin(teeId) {
        if (_tees[teeId].active) {
            return; // Already active
        }

        _tees[teeId].active = true;
        _tees[teeId].lastUpdatedAt = block.timestamp;

        // Add to active list
        _activeTEEIndex[teeId] = _activeTEEList.length;
        _activeTEEList.push(teeId);

        emit TEEActivated(teeId, block.timestamp);
    }

    // ============ Verification ============

    /// @inheritdoc ITEERegistry
    function verifySignature(VerificationRequest calldata request) external view returns (bool valid) {
        // Validate timestamp
        if (!_isTimestampValid(request.timestamp)) {
            return false;
        }

        // Check TEE is active
        if (!_tees[request.teeId].active) {
            return false;
        }

        // Compute message hash
        bytes32 messageHash = computeMessageHash(request.requestHash, request.responseHash, request.timestamp);

        // Call RSA verifier precompile
        return IRSAVerifier(RSA_VERIFIER).verifyRSAPSS(
            _tees[request.teeId].publicKey,
            messageHash,
            request.signature
        );
    }

    /// @inheritdoc ITEERegistry
    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external returns (bool valid) {
        // Validate timestamp
        if (!_isTimestampValid(timestamp)) {
            revert TimestampTooOld(timestamp, MAX_SETTLEMENT_AGE);
        }

        // Check TEE is active
        if (!_tees[teeId].active) {
            revert TEENotActive(teeId);
        }

        // Compute settlement hash for replay protection
        bytes32 settlementHash = keccak256(abi.encodePacked(teeId, inputHash, outputHash, timestamp));

        if (_usedSettlements[settlementHash]) {
            revert InvalidSignature(); // Settlement already used
        }

        // Compute message hash
        bytes32 messageHash = computeMessageHash(inputHash, outputHash, timestamp);

        // Call RSA verifier precompile
        bool valid = IRSAVerifier(RSA_VERIFIER).verifyRSAPSS(
            _tees[teeId].publicKey,
            messageHash,
            signature
        );

        if (!valid) {
            revert InvalidSignature();
        }

        // Mark settlement as used (replay protection)
        _usedSettlements[settlementHash] = true;

        emit SettlementVerified(teeId, settlementHash, msg.sender, timestamp);

        return true;
    }

    // ============ TEE Queries ============

    /// @inheritdoc ITEERegistry
    function getTEE(bytes32 teeId) external view teeExists(teeId) returns (TEEInfo memory) {
        return _tees[teeId];
    }

    /// @inheritdoc ITEERegistry
    function getActiveTEEs() external view returns (bytes32[] memory) {
        return _activeTEEList;
    }

    /// @inheritdoc ITEERegistry
    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory) {
        return _teesByType[teeType];
    }

    /// @inheritdoc ITEERegistry
    function getPublicKey(bytes32 teeId) external view teeExists(teeId) returns (bytes memory) {
        return _tees[teeId].publicKey;
    }

    /// @inheritdoc ITEERegistry
    function getTLSCertificate(bytes32 teeId) external view teeExists(teeId) returns (bytes memory) {
        return _tees[teeId].tlsCertificate;
    }

    /// @inheritdoc ITEERegistry
    function isActive(bytes32 teeId) external view returns (bool) {
        return _tees[teeId].active;
    }

    // ============ Utilities ============

    /// @inheritdoc ITEERegistry
    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32) {
        return keccak256(publicKey);
    }

    /// @inheritdoc ITEERegistry
    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
    }

    // ============ Internal Helpers ============

    function _isPCRApprovedInternal(bytes32 pcrHash) internal view returns (bool) {
        ApprovedPCR storage pcr = _approvedPCRs[pcrHash];

        if (!pcr.active) {
            return false;
        }

        // Check expiry
        if (pcr.expiresAt > 0 && block.timestamp > pcr.expiresAt) {
            return false;
        }

        return true;
    }

    function _isTimestampValid(uint256 timestamp) internal view returns (bool) {
        // Check not too far in future
        if (timestamp > block.timestamp + FUTURE_TIME_TOLERANCE) {
            return false;
        }

        // Check not too old
        if (timestamp < block.timestamp - MAX_SETTLEMENT_AGE) {
            return false;
        }

        return true;
    }

    function _removeFromActiveList(bytes32 teeId) internal {
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
}
