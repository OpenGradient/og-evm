// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./precompiles/tee/ITEEVerifier.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";

/// @title TEERegistry - TEE Registration and Management
/// @notice On-chain registry for Trusted Execution Environment (TEE) nodes that provide
///         verifiable AI inference. Manages the full TEE lifecycle: registration, enabling,
///         heartbeat liveness, and decommissioning.
///
/// @dev ## Chain of Trust
///
///  The registry establishes a hardware-rooted chain of trust from AWS Nitro hardware
///  all the way to the client connection:
///
///    AWS Nitro Hardware (root of trust)
///      → signs attestation document
///        → precompile verifies against stored AWS root certificate
///          → extracts PCR (enclave code identity) + binds signing key & TLS cert
///            → contract checks PCR is admin-approved for this TEE type
///              → heartbeats prove ongoing liveness via the bound signing key
///                → clients pin TLS cert to verify they're talking to the real enclave
///
///  **Key binding** is the critical property: the TEE's signing key and TLS certificate
///  are included in the attestation document at enclave boot time, so the on-chain record
///  is cryptographically tied to a specific enclave instance running approved code.
///
/// ## Overall Flow
///
///  1. **Admin setup** — An admin adds TEE types (e.g. LLM inference, agent execution) via `addTEEType`, then
///     approves known-good enclave measurements via `approvePCR`. The AWS root certificate
///     used for attestation verification is stored via `setAWSRootCertificate`.
///
///  2. **Registration** — A TEE operator calls `registerTEEWithAttestation`, which:
///       a. Verifies the attestation document against the AWS root cert via the 0x900
///          precompile (`ITEEVerifier`).
///       b. Extracts PCR measurements and checks they match an admin-approved set.
///       c. Binds the TEE's signing key and TLS certificate to the verified enclave identity.
///       d. Stores the TEE as **enabled** and indexes it by type and owner.
///
///  3. **Heartbeat** — Each TEE periodically proves liveness by submitting a signed
///     timestamp via `heartbeat`. The RSA-PSS signature is verified on-chain against the
///     TEE's stored public key. Stale or future timestamps are rejected.
///
///  4. **Disable / Enable** — TEE owners or admins can toggle a TEE's enabled
///     status. `enableTEE` re-validates the TEE's PCR before re-enabling.
///
///  5. **PCR revocation** — When an enclave image is compromised or outdated, admins revoke
///     its PCR via `revokePCR`. All enabled TEEs running the revoked image are immediately
///     disabled. `enableTEE` also validates the PCR to prevent re-enabling with a revoked image.
///
/// ## Querying TEE Status
///
///  The contract exposes three tiers of TEE queries with increasing strictness:
///    - `getTEEsByType`     — all TEEs ever registered for a type (enabled + disabled).
///    - `getEnabledTEEs`    — only TEEs in the enabled list (no heartbeat/PCR check).
///    - `getActiveTEEs`     — enabled TEEs with a valid PCR **and** a fresh heartbeat.
///
/// ## Client Integration Guide
///
///  **Choosing a query method:**
///
///  - `getActiveTEEs(teeType)` — **Recommended for most clients.** Returns only TEEs
///    that are enabled, running approved (non-revoked) enclave code, and have sent a
///    recent heartbeat. These are fully verified and ready to serve requests.
///
///  - `getEnabledTEEs(teeType)` — Returns TEEs that are in the enabled list but
///    does **not** check heartbeat freshness or PCR validity. Use this if you want
///    to perform your own filtering logic off-chain (e.g. custom staleness
///    thresholds, geographic selection, or load-balancing across TEEs that may have
///    briefly missed a heartbeat). You are responsible for checking liveness and
///    PCR status yourself.
///
///  - `getTEEsByType(teeType)` — Returns all TEEs ever registered for a type,
///    including disabled ones. Useful for dashboards, auditing, or historical views.
///    Not suitable for selecting a TEE to connect to.
///
///  **TLS certificate verification:**
///
///  When connecting to a TEE, clients **must** verify that the TLS certificate
///  presented by the TEE's endpoint matches the `tlsCertificate` stored on-chain.
///  This certificate was bound to the enclave at registration time via attestation
///  verification. Without this check, a compromised or spoofed endpoint could
///  impersonate a registered TEE. The recommended flow is:
///    1. Query the registry for a healthy TEE (e.g. via `getActiveTEEs`).
///    2. Open a TLS connection to the TEE's `endpoint`.
///    3. Compare the server's presented certificate against `TEEInfo.tlsCertificate`.
///    4. Abort the connection if they do not match.
///
/// ## Access Control
///
///  - `DEFAULT_ADMIN_ROLE` — manages TEE types, PCRs, certificates, heartbeat config.
///  - `TEE_OPERATOR`       — registers TEEs, manages owned TEEs (disable/enable/remove).
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
        bool enabled;
        uint256 registeredAt;
        uint256 lastHeartbeatAt;
    }

    // ============ Storage ============

    // AWS Root Certificate
    bytes public awsRootCertificate;

    // Heartbeat: max allowed age of the signed timestamp vs block.timestamp.
    uint256 public heartbeatMaxAge = 1800; // 30 minutes default

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

    // All TEEs
    mapping(bytes32 => TEEInfo) public tees;

    // Enabled TEEs by type: teeType => list of enabled teeIds
    mapping(uint8 => bytes32[]) private _enabledTEEList;
    // teeType => teeId => index in _enabledTEEList[teeType]
    mapping(uint8 => mapping(bytes32 => uint256)) private _enabledTEEIndex;

    // All TEEs by type (enabled + disabled)
    mapping(uint8 => bytes32[]) internal _teesByType;
    // TEEs by owner
    mapping(address => bytes32[]) internal _teesByOwner;

    // ============ Events ============

    event TEETypeAdded(uint8 indexed typeId, string name);
    event TEETypeDeactivated(uint8 indexed typeId);
    event PCRApproved(bytes32 indexed pcrHash, uint8 indexed teeType, string version);
    event PCRRevoked(bytes32 indexed pcrHash);
    event TEERegistered(bytes32 indexed teeId, address indexed owner, uint8 teeType);
    event TEEDisabled(bytes32 indexed teeId);
    event TEEEnabled(bytes32 indexed teeId);
    event AWSCertificateUpdated(bytes32 indexed certHash);
    event HeartbeatReceived(bytes32 indexed teeId, uint256 timestamp);

    // ============ Errors ============

    error TEETypeExists();
    error TEETypeNotFound();
    error InvalidTEEType();
    error PCRNotApproved();
    error PCRAlreadyExists();
    error TEEAlreadyExists();
    error TEENotFound();
    error TEENotEnabled();
    error NotTEEOwner();
    error AttestationInvalid(string reason);
    error KeyBindingFailed(string reason);
    error HeartbeatSignatureInvalid();
    error HeartbeatTimestampTooOld();
    error HeartbeatTimestampInFuture();

    // ============ Modifiers ============

    modifier onlyTEEOwnerOrAdmin(bytes32 teeId) {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        bool isAdmin = hasRole(DEFAULT_ADMIN_ROLE, msg.sender);
        bool isOwnerOperator = tees[teeId].owner == msg.sender && hasRole(TEE_OPERATOR, msg.sender);
        if (!isAdmin && !isOwnerOperator) revert NotTEEOwner();
        _;
    }

    // ============ Constructor ============

    constructor() {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(TEE_OPERATOR, msg.sender);
        _setRoleAdmin(TEE_OPERATOR, DEFAULT_ADMIN_ROLE);
    }

    // ============ Certificate Management ============

    function setAWSRootCertificate(bytes calldata certificate) external onlyRole(DEFAULT_ADMIN_ROLE) {
        awsRootCertificate = certificate;
        emit AWSCertificateUpdated(keccak256(certificate));
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
            version: version
        });

        if (isNew) {
            _pcrList.push(PCRKey({pcrHash: pcrHash, teeType: teeType}));
        }

        emit PCRApproved(pcrHash, teeType, version);
    }

    /// @notice Revoke a PCR and immediately disable all TEEs running it
    /// @param pcrHash The PCR hash to revoke
    /// @param teeType The TEE type this PCR belongs to
    function revokePCR(bytes32 pcrHash, uint8 teeType) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!isPCRApproved(teeType, pcrHash)) revert PCRNotApproved();

        approvedPCRs[teeType][pcrHash].active = false;

        // Actively disable all enabled TEEs running this PCR (iterate backwards for safe swap-and-pop)
        bytes32[] storage list = _enabledTEEList[teeType];
        for (uint256 i = list.length; i > 0; i--) {
            bytes32 teeId = list[i - 1];
            if (tees[teeId].pcrHash == pcrHash) {
                tees[teeId].enabled = false;
                _removeFromEnabledList(teeId, teeType);
                emit TEEDisabled(teeId);
            }
        }

        emit PCRRevoked(pcrHash);
    }

    /// @notice Check if a PCR is currently approved
    /// @param teeType The TEE type the PCR is valid for
    /// @param pcrHash The PCR hash to check
    /// @return bool True if approved
    function isPCRApproved(uint8 teeType, bytes32 pcrHash) public view returns (bool) {
        return approvedPCRs[teeType][pcrHash].active;
    }

    /// @dev Reverts if PCR is not approved for the given TEE type
    function _requirePCRValidForTEE(bytes32 pcrHash, uint8 teeType) private view {
        if (!approvedPCRs[teeType][pcrHash].active) revert PCRNotApproved();
    }

    /// @notice Compute PCR hash from measurements
    /// @param pcrs The PCR measurements
    /// @return bytes32 Hash of the concatenated PCRs
    function computePCRHash(PCRMeasurements calldata pcrs) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));
    }

    /// @notice Get all currently approved PCRs
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
            enabled: true,
            registeredAt: block.timestamp,
            lastHeartbeatAt: block.timestamp
        });

        // Add to indexes
        _enabledTEEIndex[teeType][teeId] = _enabledTEEList[teeType].length;
        _enabledTEEList[teeType].push(teeId);
        _teesByType[teeType].push(teeId);
        _teesByOwner[msg.sender].push(teeId);

        emit TEERegistered(teeId, msg.sender, teeType);
    }

    /// @notice Disable a TEE, removing it from the enabled list
    /// @dev Requires caller to be the TEE owner with TEE_OPERATOR role, or an admin
    /// @param teeId The TEE identifier to disable
    function disableTEE(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        TEEInfo storage tee = tees[teeId];
        if (!tee.enabled) revert TEENotEnabled();

        tee.enabled = false;
        _removeFromEnabledList(teeId, tee.teeType);
        emit TEEDisabled(teeId);
    }

    /// @notice Re-enable a previously disabled TEE
    /// @dev Requires caller to be the TEE owner with TEE_OPERATOR role, or an admin.
    ///      Also re-validates that the TEE's PCR is still approved for its type.
    /// @param teeId The TEE identifier to enable
    function enableTEE(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        TEEInfo storage tee = tees[teeId];
        if (tee.enabled) return;

        _requirePCRValidForTEE(tee.pcrHash, tee.teeType);

        tee.enabled = true;
        _addToEnabledList(teeId, tee.teeType);
        emit TEEEnabled(teeId);
    }

    function _addToEnabledList(bytes32 teeId, uint8 teeType) private {
        _enabledTEEIndex[teeType][teeId] = _enabledTEEList[teeType].length;
        _enabledTEEList[teeType].push(teeId);
    }

    function _removeFromEnabledList(bytes32 teeId, uint8 teeType) private {
        uint256 index = _enabledTEEIndex[teeType][teeId];
        uint256 lastIndex = _enabledTEEList[teeType].length - 1;

        if (index != lastIndex) {
            bytes32 lastTeeId = _enabledTEEList[teeType][lastIndex];
            _enabledTEEList[teeType][index] = lastTeeId;
            _enabledTEEIndex[teeType][lastTeeId] = index;
        }

        _enabledTEEList[teeType].pop();
        delete _enabledTEEIndex[teeType][teeId];
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
        if (!tee.enabled) revert TEENotEnabled();

        // Reject stale or future signed timestamps
        if (timestamp > block.timestamp) revert HeartbeatTimestampInFuture();
        if (block.timestamp - timestamp > heartbeatMaxAge) revert HeartbeatTimestampTooOld();

        // Verify RSA-PSS signature using the TEE's stored public key
        bytes32 messageHash = keccak256(abi.encodePacked(teeId, timestamp));
        bool valid = VERIFIER.verifyRSAPSS(tee.publicKey, messageHash, signature);
        if (!valid) revert HeartbeatSignatureInvalid();

        tee.lastHeartbeatAt = block.timestamp;
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

    /// @notice Get TEE IDs that are currently enabled for a given type
    /// @dev Does NOT filter by heartbeat freshness. 
    ///      Use getActiveTEEs() for fully verified results.
    /// @param teeType The TEE type to query
    /// @return Array of TEE IDs
    function getEnabledTEEs(uint8 teeType) external view returns (bytes32[] memory) {
        return _enabledTEEList[teeType];
    }

    /// @notice Get TEEs that are enabled, have a valid PCR, and a fresh heartbeat
    /// @dev More expensive than getEnabledTEEs() due to on-chain filtering.
    ///      Use this when you need guaranteed-live TEEs without client-side checks.
    /// @param teeType The TEE type to query
    /// @return Array of TEEInfo structs for active TEEs
    function getActiveTEEs(uint8 teeType) external view returns (TEEInfo[] memory) {
        bytes32[] storage list = _enabledTEEList[teeType];
        uint256 count = 0;
        for (uint256 i = 0; i < list.length; i++) {
            if (_isTEEActive(tees[list[i]])) count++;
        }

        TEEInfo[] memory result = new TEEInfo[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < list.length; i++) {
            if (_isTEEActive(tees[list[i]])) {
                result[j++] = tees[list[i]];
            }
        }
        return result;
    }

    /// @notice Get all TEE IDs (enabled and disabled) for a given type
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

    /// @notice Check if a TEE is currently enabled
    function isTEEEnabled(bytes32 teeId) external view returns (bool) {
        return tees[teeId].enabled;
    }

    /// @notice Check if a TEE is active (enabled + valid PCR + fresh heartbeat)
    function isTEEActive(bytes32 teeId) external view returns (bool) {
        return _isTEEActive(tees[teeId]);
    }

    function _isTEEActive(TEEInfo storage tee) private view returns (bool) {
        if (!tee.enabled) return false;
        if (block.timestamp - tee.lastHeartbeatAt > heartbeatMaxAge) return false;
        if (!isPCRApproved(tee.teeType, tee.pcrHash)) return false;
        return true;
    }

    /// @notice Get a TEE's public key
    function getTEEPublicKey(bytes32 teeId) external view returns (bytes memory) {
        return tees[teeId].publicKey;
    }

    /// @notice Compute TEE ID from its public key
    /// @param publicKey The TEE's public key
    /// @return The TEE identifier (keccak256 hash)
    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32) {
        return keccak256(publicKey);
    }
}
