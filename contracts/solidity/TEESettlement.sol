// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./TEERegistry.sol";
import "./precompiles/tee/ITEEVerifier.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";

/// @title TEESettlement - Settlement verification for TEE-signed outputs
/// @notice Reads TEE public keys from TEERegistry, verifies RSA-PSS signatures,
contract TEESettlement is AccessControl {

    // ============ Constants ============

    ITEEVerifier public constant VERIFIER =
        ITEEVerifier(0x0000000000000000000000000000000000000900);

    uint256 public constant MAX_SETTLEMENT_AGE = 1 hours;
    uint256 public constant FUTURE_TOLERANCE   = 5 minutes;

    // ============ State ============

    /// @notice The registry this settlement contract reads TEE info from
    TEERegistry public registry;

    /// @notice Replay protection: settlementHash => used
    mapping(bytes32 => bool) public settlementUsed;
    // ============ Events ============

    event SettlementVerified(
        bytes32 indexed teeId,
        bytes32 indexed settlementHash,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    );

    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);

    // ============ Errors ============

    error TEENotActive(bytes32 teeId);
    error TimestampTooOld(uint256 provided, uint256 minAllowed);
    error TimestampInFuture(uint256 provided, uint256 maxAllowed);
    error SettlementAlreadyUsed(bytes32 settlementHash);
    error InvalidSignature();

    // ============ Constructor ============

    /// @param _registry Address of the deployed TEERegistry
    constructor(address _registry) {
        registry = TEERegistry(_registry);
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
    }

    // ============ Admin ============

    /// @notice Point this contract at a new registry (e.g. after a registry migration)
    function setRegistry(address _registry) external onlyRole(DEFAULT_ADMIN_ROLE) {
        emit RegistryUpdated(address(registry), _registry);
        registry = TEERegistry(_registry);
    }

    // ============ Core Logic ============

    /// @notice Compute the canonical message hash the TEE must sign
    /// @dev keccak256(inputHash || outputHash || timestamp)
    ///      teeId is NOT included — each TEE has a unique key pair so the
    ///      signature is already implicitly bound to a single TEE identity.
    ///      teeId is still included in settlementHash for replay protection.
    function computeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
    }

    /// @notice Stateless signature check — no replay protection, no timestamp validation
    /// @dev Useful for off-chain pre-validation or other contracts calling in
    function verifySignature(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) public view returns (bool) {
        if (!registry.isActive(teeId)) return false;

        bytes memory pubKey = registry.getPublicKey(teeId);
        bytes32 msgHash = computeMessageHash(inputHash, outputHash, timestamp);
        return VERIFIER.verifyRSAPSS(pubKey, msgHash, signature);
    }

    /// @notice Full settlement verification with replay protection and timestamp bounds
    /// @param teeId      Registered TEE identifier
    /// @param inputHash  Hash of the inference input
    /// @param outputHash Hash of the inference output
    /// @param timestamp  Unix timestamp the TEE embedded when signing (seconds)
    /// @param signature  RSA-PSS signature from the TEE's signing key
    /// @return settlementHash The unique hash recorded for this settlement
    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external returns (bytes32 settlementHash) {
        // 1. TEE must be active in the registry
        if (!registry.isActive(teeId)) revert TEENotActive(teeId);

        // 2. Timestamp bounds
        uint256 minTs = block.timestamp - MAX_SETTLEMENT_AGE;
        uint256 maxTs = block.timestamp + FUTURE_TOLERANCE;
        if (timestamp < minTs) revert TimestampTooOld(timestamp, minTs);
        if (timestamp > maxTs) revert TimestampInFuture(timestamp, maxTs);

        // 3. Replay protection
        settlementHash = keccak256(
            abi.encodePacked(teeId, inputHash, outputHash, timestamp)
        );
        if (settlementUsed[settlementHash]) revert SettlementAlreadyUsed(settlementHash);

        // 4. Cryptographic verification
        bytes memory pubKey = registry.getPublicKey(teeId);
        bytes32 msgHash = computeMessageHash(inputHash, outputHash, timestamp);
        if (!VERIFIER.verifyRSAPSS(pubKey, msgHash, signature)) revert InvalidSignature();

        // 5. Commit
        settlementUsed[settlementHash] = true;
        emit SettlementVerified(teeId, settlementHash, inputHash, outputHash, timestamp);
    }
}
