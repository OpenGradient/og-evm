// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./TEERegistry.sol";

/// @title TEERegistryV2 - TEE registry with on-chain OHTTP config
/// @notice Extends TEERegistry with an attested-signing-key-backed OHTTP config
///         record. This avoids changing the TEE verifier precompile: the Nitro
///         attestation still binds the TEE signing key, and that signing key
///         signs the OHTTP/HPKE config before it is accepted on-chain.
contract TEERegistryV2 is TEERegistry {
    bytes32 public constant OHTTP_CONFIG_DOMAIN_SEPARATOR =
        keccak256("OPENGRADIENT_TEE_OHTTP_CONFIG_V1");
    uint16 public constant KEM_ID_X25519_HKDF_SHA256 = 32;
    uint256 public constant X25519_PUBLIC_KEY_SIZE = 32;

    struct OHTTPConfig {
        uint8 keyId;
        uint16 kemId;
        uint16 kdfId;
        uint16 aeadId;
        bytes publicKey;
        bytes keyConfig;
        uint256 registeredAt;
        uint256 updatedAt;
    }

    struct TEEOHTTPRecord {
        bytes32 teeId;
        TEEInfo tee;
        OHTTPConfig ohttpConfig;
    }

    mapping(bytes32 => OHTTPConfig) private _ohttpConfigs;

    event OHTTPConfigUpdated(
        bytes32 indexed teeId,
        uint8 keyId,
        uint16 kemId,
        uint16 kdfId,
        uint16 aeadId,
        bytes32 publicKeyHash,
        bytes32 keyConfigHash
    );
    event OHTTPConfigCleared(bytes32 indexed teeId);

    error OHTTPConfigNotFound();
    error OHTTPConfigInvalid();
    error OHTTPConfigSignatureInvalid();

    /// @notice Register a TEE using the original attestation flow, then attach
    ///         a signed OHTTP config in the same transaction.
    /// @dev This keeps the precompile unchanged. The OHTTP config is verified
    ///      using verifyRSAPSS against the signing public key that was bound by
    ///      the Nitro attestation.
    function registerTEEWithAttestationAndOHTTPConfig(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType,
        uint8 keyId,
        uint16 kemId,
        uint16 kdfId,
        uint16 aeadId,
        bytes calldata ohttpPublicKey,
        bytes calldata ohttpKeyConfig,
        bytes calldata ohttpConfigSignature
    ) external onlyRole(TEE_OPERATOR) returns (bytes32 teeId) {
        teeId = registerTEEWithAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            paymentAddress,
            endpoint,
            teeType
        );

        _setOHTTPConfig(
            teeId,
            keyId,
            kemId,
            kdfId,
            aeadId,
            ohttpPublicKey,
            ohttpKeyConfig,
            ohttpConfigSignature
        );
    }

    /// @notice Set or rotate the OHTTP config for a registered TEE.
    /// @dev The caller must own the TEE or be an admin, and the config must be
    ///      signed by the TEE signing key stored in the base registry.
    function setOHTTPConfig(
        bytes32 teeId,
        uint8 keyId,
        uint16 kemId,
        uint16 kdfId,
        uint16 aeadId,
        bytes calldata ohttpPublicKey,
        bytes calldata ohttpKeyConfig,
        bytes calldata ohttpConfigSignature
    ) external onlyTEEOwnerOrAdmin(teeId) {
        _setOHTTPConfig(
            teeId,
            keyId,
            kemId,
            kdfId,
            aeadId,
            ohttpPublicKey,
            ohttpKeyConfig,
            ohttpConfigSignature
        );
    }

    function clearOHTTPConfig(bytes32 teeId) external onlyTEEOwnerOrAdmin(teeId) {
        if (_ohttpConfigs[teeId].registeredAt == 0) revert OHTTPConfigNotFound();
        delete _ohttpConfigs[teeId];
        emit OHTTPConfigCleared(teeId);
    }

    function getOHTTPConfig(bytes32 teeId) external view returns (OHTTPConfig memory) {
        OHTTPConfig memory config = _ohttpConfigs[teeId];
        if (config.registeredAt == 0) revert OHTTPConfigNotFound();
        return config;
    }

    function hasOHTTPConfig(bytes32 teeId) external view returns (bool) {
        return _ohttpConfigs[teeId].registeredAt != 0;
    }

    function getTEEWithOHTTPConfig(
        bytes32 teeId
    ) external view returns (TEEInfo memory tee, OHTTPConfig memory ohttpConfig) {
        tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();

        ohttpConfig = _ohttpConfigs[teeId];
        if (ohttpConfig.registeredAt == 0) revert OHTTPConfigNotFound();
    }

    function getActiveTEERecordsWithOHTTPConfig(
        uint8 teeType
    ) external view returns (TEEOHTTPRecord[] memory) {
        bytes32[] memory teeIds = this.getEnabledTEEs(teeType);
        uint256 count = 0;

        for (uint256 i = 0; i < teeIds.length; i++) {
            if (_isActiveWithOHTTPConfig(teeIds[i])) count++;
        }

        TEEOHTTPRecord[] memory result = new TEEOHTTPRecord[](count);
        uint256 j = 0;
        for (uint256 i = 0; i < teeIds.length; i++) {
            bytes32 teeId = teeIds[i];
            if (_isActiveWithOHTTPConfig(teeId)) {
                result[j++] = TEEOHTTPRecord({
                    teeId: teeId,
                    tee: tees[teeId],
                    ohttpConfig: _ohttpConfigs[teeId]
                });
            }
        }

        return result;
    }

    function computeOHTTPConfigHash(
        bytes32 teeId,
        uint8 keyId,
        uint16 kemId,
        uint16 kdfId,
        uint16 aeadId,
        bytes calldata ohttpPublicKey,
        bytes calldata ohttpKeyConfig
    ) public pure returns (bytes32) {
        return keccak256(
            abi.encode(
                OHTTP_CONFIG_DOMAIN_SEPARATOR,
                teeId,
                keyId,
                kemId,
                kdfId,
                aeadId,
                keccak256(ohttpPublicKey),
                keccak256(ohttpKeyConfig)
            )
        );
    }

    function _setOHTTPConfig(
        bytes32 teeId,
        uint8 keyId,
        uint16 kemId,
        uint16 kdfId,
        uint16 aeadId,
        bytes calldata ohttpPublicKey,
        bytes calldata ohttpKeyConfig,
        bytes calldata ohttpConfigSignature
    ) private {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) revert TEENotFound();
        if (ohttpPublicKey.length == 0 || ohttpKeyConfig.length == 0) {
            revert OHTTPConfigInvalid();
        }
        if (
            kemId == KEM_ID_X25519_HKDF_SHA256 &&
            ohttpPublicKey.length != X25519_PUBLIC_KEY_SIZE
        ) {
            revert OHTTPConfigInvalid();
        }

        bytes32 configHash = computeOHTTPConfigHash(
            teeId,
            keyId,
            kemId,
            kdfId,
            aeadId,
            ohttpPublicKey,
            ohttpKeyConfig
        );
        bool valid = VERIFIER.verifyRSAPSS(tee.publicKey, configHash, ohttpConfigSignature);
        if (!valid) revert OHTTPConfigSignatureInvalid();

        OHTTPConfig storage config = _ohttpConfigs[teeId];
        uint256 registeredAt = config.registeredAt == 0 ? block.timestamp : config.registeredAt;

        _ohttpConfigs[teeId] = OHTTPConfig({
            keyId: keyId,
            kemId: kemId,
            kdfId: kdfId,
            aeadId: aeadId,
            publicKey: ohttpPublicKey,
            keyConfig: ohttpKeyConfig,
            registeredAt: registeredAt,
            updatedAt: block.timestamp
        });

        emit OHTTPConfigUpdated(
            teeId,
            keyId,
            kemId,
            kdfId,
            aeadId,
            keccak256(ohttpPublicKey),
            keccak256(ohttpKeyConfig)
        );
    }

    function _isActiveWithOHTTPConfig(bytes32 teeId) private view returns (bool) {
        TEEInfo storage tee = tees[teeId];
        if (tee.registeredAt == 0) return false;
        if (!tee.enabled) return false;
        if (block.timestamp < tee.lastHeartbeatAt) return false;
        if (block.timestamp - tee.lastHeartbeatAt > heartbeatMaxAge) return false;
        if (!isPCRApproved(tee.teeType, tee.pcrHash)) return false;
        if (_ohttpConfigs[teeId].registeredAt == 0) return false;
        return true;
    }
}
