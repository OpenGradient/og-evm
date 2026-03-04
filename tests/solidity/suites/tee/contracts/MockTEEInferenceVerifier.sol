// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title MockTEEInferenceVerifier
/// @notice Mock that always returns true from verifySignature for testing settlement success paths
contract MockTEEInferenceVerifier {
    bool public verifyResult;

    constructor() {
        verifyResult = true;
    }

    function setVerifyResult(bool _result) external {
        verifyResult = _result;
    }

    function verifySignature(
        bytes32,
        bytes32,
        bytes32,
        uint256,
        bytes calldata
    ) external view returns (bool) {
        return verifyResult;
    }
}
