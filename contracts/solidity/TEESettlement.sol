// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./TEERegistry.sol";
import "./precompiles/tee/ITEEVerifier.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";

/// @title TEESettlement - Settlement verification for TEE-signed outputs
/// @notice Reads TEE public keys from TEERegistry and verifies RSA-PSS signatures with timestamp validation.
contract TEESettlement is AccessControl {

    // ============ Constants ============

    ITEEVerifier public constant VERIFIER =
        ITEEVerifier(0x0000000000000000000000000000000000000900);

    uint256 public constant MAX_SETTLEMENT_AGE = 1 hours;
    uint256 public constant FUTURE_TOLERANCE = 5 minutes;

    // ============ State ============

    /// @notice The registry this settlement contract reads TEE info from
    TEERegistry public registry;

    // ============ Events ============

    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);

    // ============ Errors ============

    error InvalidRegistryAddress();

    // ============ Constructor ============

    /// @param _registry Address of the deployed TEERegistry
    constructor(address _registry) {
        if (_registry == address(0)) revert InvalidRegistryAddress();
        registry = TEERegistry(_registry);
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
    }

    // ============ Admin ============

    /// @notice Point this contract at a new registry (e.g. after a registry migration)
    function setRegistry(address _registry) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (_registry == address(0)) revert InvalidRegistryAddress();
        emit RegistryUpdated(address(registry), _registry);
        registry = TEERegistry(_registry);
    }

    // ============ Core Logic ============

    /// @notice Compute the canonical message hash the TEE must sign
    /// @dev keccak256(inputHash || outputHash || timestamp)
    /// @param inputHash Hash of the inference input
    /// @param outputHash Hash of the inference output
    /// @param timestamp Unix timestamp the TEE embedded when signing
    /// @return The message hash that should be signed
    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
    }

    /// @notice Verify a TEE signature with timestamp validation
    /// @dev Returns false for invalid signatures, inactive TEEs, or out-of-bounds timestamps.
    /// @param teeId Registered TEE identifier
    /// @param inputHash Hash of the inference input
    /// @param outputHash Hash of the inference output
    /// @param timestamp Unix timestamp the TEE embedded when signing (seconds)
    /// @param signature RSA-PSS signature from the TEE's signing key
    /// @return True if TEE is active, timestamp is valid, and signature is correct
    function verifySignature(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) public view returns (bool) {
        // 1. TEE must be active in the registry
        if (!registry.isActive(teeId)) return false;

        // 2. Timestamp bounds
        uint256 minTs = block.timestamp > MAX_SETTLEMENT_AGE 
            ? block.timestamp - MAX_SETTLEMENT_AGE 
            : 0;
        uint256 maxTs = block.timestamp + FUTURE_TOLERANCE;
        if (timestamp < minTs || timestamp > maxTs) return false;

        // 3. Cryptographic verification
        bytes memory pubKey = registry.getPublicKey(teeId);
        bytes32 msgHash = computeMessageHash(inputHash, outputHash, timestamp);
        return VERIFIER.verifyRSAPSS(pubKey, msgHash, signature);
    }
}