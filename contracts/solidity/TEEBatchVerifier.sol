// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./TEEInferenceVerifier.sol";

/// @title TEEBatchVerifier - TEE signature verification without timestamp bounds

contract TEEBatchVerifier is TEEInferenceVerifier {

    constructor(address _registry) TEEInferenceVerifier(_registry) {}

    /// @notice Verify a TEE signature without timestamp bounds check
    /// @param teeId Registered TEE identifier
    /// @param inputHash Hash of the inference input
    /// @param outputHash Hash of the inference output
    /// @param timestamp Unix timestamp the TEE embedded when signing (seconds)
    /// @param signature RSA-PSS signature from the TEE's signing key
    /// @return True if TEE is active and signature is cryptographically valid
    function verifySignatureNoTimestamp(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) public view returns (bool) {
        if (!registry.isTEEEnabled(teeId)) return false;
        bytes memory pubKey = registry.getTEEPublicKey(teeId);
        bytes32 msgHash = computeMessageHash(inputHash, outputHash, timestamp);
        return VERIFIER.verifyRSAPSS(pubKey, msgHash, signature);
    }
}
