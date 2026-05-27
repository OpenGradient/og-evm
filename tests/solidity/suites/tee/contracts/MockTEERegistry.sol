// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./cosmos/TEERegistry.sol";

/// @title MockTEERegistry
/// @notice Test-only TEERegistry that allows registering TEEs without attestation verification
/// @dev Bypasses the precompile call. Registers TEE as inactive, then call enableTEE() to add to enabled list.
contract MockTEERegistry is TEERegistry {

    /// @notice Register a TEE directly for testing, bypassing attestation verification.
    /// @dev The TEE is registered as INACTIVE. Call enableTEE(teeId) afterward from the
    ///      same account to add it to the enabled list. This two-step approach is needed because
    ///      _enabledTEEList and related indexes are private in the parent contract.
    function registerTEEForTesting(
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType,
        bytes32 pcrHash
    ) external onlyRole(TEE_OPERATOR) returns (bytes32 teeId) {
        if (!isValidTEEType(teeType)) revert InvalidTEEType();

        teeId = keccak256(signingPublicKey);
        if (tees[teeId].registeredAt != 0) revert TEEAlreadyExists();

        // Store TEE as inactive; caller must call enableTEE(teeId) to add to enabled list
        tees[teeId] = TEEInfo({
            owner: msg.sender,
            paymentAddress: paymentAddress,
            endpoint: endpoint,
            publicKey: signingPublicKey,
            tlsCertificate: tlsCertificate,
            pcrHash: pcrHash,
            teeType: teeType,
            enabled: false,
            registeredAt: block.timestamp,
            lastHeartbeatAt: block.timestamp,
            ohttpConfig: OHTTPConfig({
                keyId: 0,
                kemId: 0,
                kdfId: 0,
                aeadId: 0,
                publicKey: "",
                keyConfig: "",
                signature: "",
                registeredAt: 0
            })
        });

        // Add to indexes (matching registerTEE behavior)
        _teesByType[teeType].push(teeId);
        _teesByOwner[msg.sender].push(teeId);

        emit TEERegistered(teeId, msg.sender, teeType);
    }

    /// @notice Exposes the internal OHTTP config validation for testing.
    /// @dev Lets tests exercise the OHTTPConfigInvalid checks (which run before any
    ///      precompile call) without the attestation flow. The RSA-PSS verification
    ///      step still requires the TEE verifier precompile.
    function validateOHTTPConfigForTesting(
        bytes32 teeId,
        bytes calldata signingPublicKey,
        OHTTPConfigInput calldata ohttp
    ) external view returns (OHTTPConfig memory) {
        return _buildOHTTPConfig(teeId, signingPublicKey, ohttp);
    }

    /// @notice Stores an OHTTP config on an existing TEE so getOHTTPConfig() retrieval
    ///         (and the ABI round-trip) can be tested without the precompile.
    function setOHTTPConfigForTesting(bytes32 teeId, OHTTPConfigInput calldata ohttp) external {
        if (tees[teeId].registeredAt == 0) revert TEENotFound();
        tees[teeId].ohttpConfig = OHTTPConfig({
            keyId: ohttp.keyId,
            kemId: ohttp.kemId,
            kdfId: ohttp.kdfId,
            aeadId: ohttp.aeadId,
            publicKey: ohttp.publicKey,
            keyConfig: ohttp.keyConfig,
            signature: ohttp.signature,
            registeredAt: block.timestamp
        });
    }
}
