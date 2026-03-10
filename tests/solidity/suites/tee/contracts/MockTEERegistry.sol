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
            lastHeartbeatAt: block.timestamp
        });

        // Add to indexes (matching registerTEE behavior)
        _teesByType[teeType].push(teeId);
        _teesByOwner[msg.sender].push(teeId);

        emit TEERegistered(teeId, msg.sender, teeType);
    }
}
