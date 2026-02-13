// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./cosmos/TEERegistry.sol";
import "./cosmos/tee/ITEEVerifier.sol";

/// @title TEETestHelper
/// @notice Helper contract for testing TEERegistry and TEE precompile
/// @dev Provides convenient wrappers and test utilities
contract TEETestHelper {
    TEERegistry public registry;
    ITEEVerifier public constant VERIFIER = ITEEVerifier(0x0000000000000000000000000000000000000900);

    event PrecompileCallResult(bool success, bytes32 result);
    event SignatureVerificationResult(bool valid);

    constructor(address registryAddress) {
        registry = TEERegistry(registryAddress);
    }

    // ============ TEE Type Management Wrappers ============

    function addTEEType(uint8 typeId, string calldata name) external {
        registry.addTEEType(typeId, name);
    }

    function deactivateTEEType(uint8 typeId) external {
        registry.deactivateTEEType(typeId);
    }

    function isValidTEEType(uint8 typeId) external view returns (bool) {
        return registry.isValidTEEType(typeId);
    }

    // ============ PCR Management Wrappers ============

    function approvePCR(
        TEERegistry.PCRMeasurements calldata pcrs,
        string calldata version,
        bytes32 previousPcrHash,
        uint256 gracePeriod
    ) external {
        registry.approvePCR(pcrs, version, previousPcrHash, gracePeriod);
    }

    function revokePCR(bytes32 pcrHash) external {
        registry.revokePCR(pcrHash);
    }

    function isPCRApproved(bytes32 pcrHash) external view returns (bool) {
        return registry.isPCRApproved(pcrHash);
    }

    function computePCRHash(TEERegistry.PCRMeasurements calldata pcrs) external pure returns (bytes32) {
        return keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));
    }

    // ============ Direct Precompile Testing ============

    function testVerifyAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        bytes calldata rootCertificate
    ) external returns (bool valid, bytes32 pcrHash) {
        (valid, pcrHash) = VERIFIER.verifyAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            rootCertificate
        );
        emit PrecompileCallResult(valid, pcrHash);
    }

    function testVerifyRSAPSS(
        bytes calldata publicKeyDER,
        bytes32 messageHash,
        bytes calldata signature
    ) external returns (bool valid) {
        valid = VERIFIER.verifyRSAPSS(publicKeyDER, messageHash, signature);
        emit SignatureVerificationResult(valid);
    }

    // ============ TEE Registration Wrappers ============

    function registerTEE(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        address paymentAddress,
        string calldata endpoint,
        uint8 teeType
    ) external returns (bytes32 teeId) {
        return registry.registerTEEWithAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            paymentAddress,
            endpoint,
            teeType
        );
    }

    // ============ TEE Management Wrappers ============

    function deactivateTEE(bytes32 teeId) external {
        registry.deactivateTEE(teeId);
    }

    function activateTEE(bytes32 teeId) external {
        registry.activateTEE(teeId);
    }

    // ============ Verification Wrappers ============

    function verifySignature(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external view returns (bool) {
        return registry.verifySignature(teeId, inputHash, outputHash, timestamp, signature);
    }

    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) external pure returns (bytes32) {
        return keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
    }

    // ============ Query Wrappers ============

    function getTEE(bytes32 teeId) external view returns (TEERegistry.TEEInfo memory) {
        return registry.getTEE(teeId);
    }

    function getActiveTEEs() external view returns (bytes32[] memory) {
        return registry.getActiveTEEs();
    }

    function getTEEsByType(uint8 teeType) external view returns (bytes32[] memory) {
        return registry.getTEEsByType(teeType);
    }

    function getTEEsByOwner(address owner) external view returns (bytes32[] memory) {
        return registry.getTEEsByOwner(owner);
    }

    function getPublicKey(bytes32 teeId) external view returns (bytes memory) {
        return registry.getPublicKey(teeId);
    }

    function isActive(bytes32 teeId) external view returns (bool) {
        return registry.isActive(teeId);
    }

    function computeTEEId(bytes calldata publicKey) external pure returns (bytes32) {
        return keccak256(publicKey);
    }

    // ============ Test Utilities ============

    function setAWSRootCertificate(bytes calldata certificate) external {
        registry.setAWSRootCertificate(certificate);
    }

    // Helper to test gas usage
    function estimateAttestationGas(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        bytes calldata rootCertificate
    ) external returns (uint256 gasUsed) {
        uint256 gasBefore = gasleft();
        VERIFIER.verifyAttestation(
            attestationDocument,
            signingPublicKey,
            tlsCertificate,
            rootCertificate
        );
        gasUsed = gasBefore - gasleft();
    }

    function estimateRSAPSSGas(
        bytes calldata publicKeyDER,
        bytes32 messageHash,
        bytes calldata signature
    ) external returns (uint256 gasUsed) {
        uint256 gasBefore = gasleft();
        VERIFIER.verifyRSAPSS(publicKeyDER, messageHash, signature);
        gasUsed = gasBefore - gasleft();
    }

    // Helper to test timestamp validation
    function getCurrentTimestamp() external view returns (uint256) {
        return block.timestamp;
    }
}
