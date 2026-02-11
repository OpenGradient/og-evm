// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title IRSAVerifier
/// @notice Interface for RSA-PSS signature verification precompile
/// @dev Precompile address: 0x0000000000000000000000000000000000000902
interface IRSAVerifier {
    /// @notice Verify an RSA-PSS signature using SHA-256
    /// @param publicKeyDER DER-encoded RSA public key
    /// @param messageHash SHA-256 hash of the message (32 bytes)
    /// @param signature RSA-PSS signature bytes
    /// @return valid True if signature is valid for the given message hash and public key
    function verifyRSAPSS(
        bytes calldata publicKeyDER,
        bytes32 messageHash,
        bytes calldata signature
    ) external view returns (bool valid);
}
