// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ITEEVerifier
/// @notice Interface for TEE verification precompile (AWS Nitro attestation + RSA-PSS signatures)
/// @dev Precompile address: 0x0000000000000000000000000000000000000901
interface ITEEVerifier {
    /// @notice Verify an AWS Nitro attestation document with Nitriding dual-key binding
    /// @param attestationDocument CBOR-encoded AWS Nitro attestation document
    /// @param signingPublicKey DER-encoded RSA public key for settlement signatures
    /// @param tlsCertificate DER-encoded X.509 certificate for TLS connections
    /// @param rootCertificate DER-encoded AWS root certificate for chain verification
    /// @return valid True if attestation is valid and keys are bound correctly
    /// @return pcrHash keccak256(pcr0 || pcr1 || pcr2) - hash of PCR measurements
    function verifyAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        bytes calldata rootCertificate
    ) external view returns (bool valid, bytes32 pcrHash);

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
