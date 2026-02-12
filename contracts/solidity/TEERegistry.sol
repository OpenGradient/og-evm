// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./precompiles/tee/ITEEVerifier.sol";

/// @title TEERegistry - TEE Registration and Management
/// @notice Manages TEE lifecycle, calls precompile only for crypto
/// @dev All storage in Solidity, crypto in precompile at 0x900
contract TEERegistry {
    
    // ============ Constants ============
    
    ITEEVerifier public constant VERIFIER = ITEEVerifier(0x0000000000000000000000000000000000000900);
    uint256 public constant MAX_SETTLEMENT_AGE = 1 hours;
    uint256 public constant FUTURE_TOLERANCE = 5 minutes;

    // ============ Structs ============
    
    struct PCRMeasurements {
        bytes pcr0;
        bytes pcr1;
        bytes pcr2;
    }

    struct ApprovedPCR {
        bool active;
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
    
    // Admin management
    mapping(address => bool) public isAdmin;
    address[] private _adminList;
    
    // TEE Types
    mapping(uint8 => TEETypeInfo) public teeTypes;
    uint8[] private _teeTypeList;
    
    // PCR Registry
    mapping(bytes32 => ApprovedPCR) public approvedPCRs;
    bytes32[] private _pcrList;
    
    // AWS Root Certificate
    bytes public awsRootCertificate;
    
    // TEE Storage
    mapping(bytes32 => TEEInfo) public tees;
    bytes32[] private _activeTEEList;
    mapping(bytes32 => uint256) private _activeTEEIndex;
    mapping(address => bytes32[]) private _teesByOwner;
    mapping(uint8 => bytes32[]) private _teesByType;
    
    // Settlement replay protection
    mapping(bytes32 => bool) public settlementUsed;

    // ============ Events ============
    
    event AdminAdded(address indexed admin);
    event AdminRemoved(address indexed admin);
    event TEETypeAdded(uint8 indexed typeId, string name);
    event TEETypeDeactivated(uint8 indexed typeId);
    event PCRApproved(bytes32 indexed pcrHash, string version);
    event PCRRevoked(bytes32 indexed pcrHash);
    event TEERegistered(bytes32 indexed teeId, address indexed owner, uint8 teeType);
    event TEEDeactivated(bytes32 indexed teeId);
    event TEEActivated(bytes32 indexed teeId);
    event SettlementVerified(bytes32 indexed teeId, bytes32 indexed settlementHash);
    event AWSCertificateUpdated(bytes32 indexed certHash);

    // ============ Errors ============
    
    error NotAdmin();
    error AdminAlreadyExists();
    error AdminNotFound();
    error CannotRemoveLastAdmin();
    error TEETypeExists();
    error TEETypeNotFound();
    error InvalidTEEType();
    error PCRNotApproved();
    error PCRExpired();
    error TEEAlreadyExists();
    error TEENotFound();
    error TEENotActive();
    error NotTEEOwner();
    error AttestationInvalid(string reason);
    error KeyBindingFailed(string reason);
    error InvalidSignature();
    error SettlementAlreadyUsed();
    error TimestampTooOld();
    error TimestampInFuture();

    // ============ Modifiers ============
    
    modifier onlyAdmin() {
        if (!isAdmin[msg.sender]) revert NotAdmin();
        _;
    }

    // ============ Constructor ============
    
    constructor() {
        // Bootstrap first admin
        isAdmin[msg.sender] = true;
        _adminList.push(msg.sender);
        emit AdminAdded(msg.sender);
    }

    // ============ Admin Management ============
    
    function addAdmin(address admin) external onlyAdmin {
        if (isAdmin[admin]) revert AdminAlreadyExists();
        isAdmin[admin] = true;
        _adminList.push(admin);
        emit AdminAdded(admin);
    }

    function removeAdmin(address admin) external onlyAdmin {
        if (!isAdmin[admin]) revert AdminNotFound();
        if (_adminList.length <= 1) revert CannotRemoveLastAdmin();
        isAdmin[admin] = false;
        emit AdminRemoved(admin);
    }

    function getAdmins() external view returns (address[] memory) {
        uint256 count = 0;
        for (uint256 i = 0; i < _adminList.length; i++) {
            if (isAdmin[_adminList[i]]) count++;
        }
        address[] memory result = new address[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < _adminList.length; i++) {
            if (isAdmin[_adminList[i]]) {
                result[j++] = _adminList[i];
            }
        }
        return result;
    }

    // ============ TEE Type Management ============
    
    function addTEEType(uint8 typeId, string calldata name) external onlyAdmin {
        if (teeTypes[typeId].addedAt != 0) revert TEETypeExists();
        teeTypes[typeId] = TEETypeInfo({
            name: name,
            active: true,
            addedAt: block.timestamp
        });
        _teeTypeList.push(typeId);
        emit TEETypeAdded(typeId, name);
    }

    function deactivateTEEType(uint8 typeId) external onlyAdmin {
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
    
    function approvePCR(
        PCRMeasurements calldata pcrs,
        string calldata version,
        bytes32 previousPcrHash,
        uint256 gracePeriod
    ) external onlyAdmin {
        bytes32 pcrHash = computePCRHash(pcrs);
        
        // Set expiry on previous PCR if provided
        if (previousPcrHash != bytes32(0) && approvedPCRs[previousPcrHash].active) {
            approvedPCRs[previousPcrHash].expiresAt = block.timestamp + gracePeriod;
        }

        approvedPCRs[pcrHash] = ApprovedPCR({
            active: true,
            approvedAt: block.timestamp,
            expiresAt: 0,
            version: version
        });
        _pcrList.push(pcrHash);
        emit PCRApproved(pcrHash, version);
    }

    function revokePCR(bytes32 pcrHash) external onlyAdmin {
        approvedPCRs[pcrHash].active = false;
        emit PCRRevoked(pcrHash);
    }

    function isPCRApproved(bytes32 pcrHash) public view returns (bool) {
        ApprovedPCR storage pcr = approvedPCRs[pcrHash];
        if (!pcr.active) return false;
        if (pcr.expiresAt != 0 && block.timestamp > pcr.expiresAt) return false;
        return true;
    }

    function computePCRHash(PCRMeasurements calldata pcrs) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));
    }

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
    
    function setAWSRootCertificate(bytes calldata certificate) external onlyAdmin {
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
    ) external onlyAdmin returns (bytes32 teeId) {
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

        // Verify PCR is approved
        if (!isPCRApproved(pcrHash)) revert PCRNotApproved();

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
        if (tee.owner != msg.sender && !isAdmin[msg.sender]) revert NotTEEOwner();

        tee.active = false;
        tee.lastUpdatedAt = block.timestamp;
        _removeFromActiveList(teeId);
        emit TEEDeactivated(teeId);
    }

    function activateTEE(bytes32 teeId) external {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();
        if (tee.owner != msg.sender && !isAdmin[msg.sender]) revert NotTEEOwner();

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

    // ============ Verification ============
    
    function verifySignature(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) public view returns (bool) {
        TEEInfo storage tee = tees[teeId];
        if (!tee.active) return false;

        bytes32 messageHash = computeMessageHash(inputHash, outputHash, timestamp);
        return VERIFIER.verifyRSAPSS(tee.publicKey, messageHash, signature);
    }

    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external returns (bool) {
        // Timestamp validation
        if (timestamp < block.timestamp - MAX_SETTLEMENT_AGE) revert TimestampTooOld();
        if (timestamp > block.timestamp + FUTURE_TOLERANCE) revert TimestampInFuture();

        // Replay protection
        bytes32 settlementHash = keccak256(abi.encodePacked(teeId, inputHash, outputHash, timestamp));
        if (settlementUsed[settlementHash]) revert SettlementAlreadyUsed();

        // Verify signature
        if (!verifySignature(teeId, inputHash, outputHash, timestamp, signature)) {
            revert InvalidSignature();
        }

        // Mark as used
        settlementUsed[settlementHash] = true;
        emit SettlementVerified(teeId, settlementHash);
        
        return true;
    }

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