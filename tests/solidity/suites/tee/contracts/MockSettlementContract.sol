// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./cosmos/FacilitatorSettlementRelay.sol";

/// @title MockSettlementContract for testing FacilitatorSettlementRelay
/// @notice Allows toggling verifySignature result for test scenarios
contract MockSettlementContract is ISettlementContract {
    bool public shouldVerify = true;

    function setShouldVerify(bool _shouldVerify) external {
        shouldVerify = _shouldVerify;
    }

    function verifySignature(
        bytes32, /* teeId */
        bytes32, /* inputHash */
        bytes32, /* outputHash */
        uint256, /* timestamp */
        bytes calldata /* signature */
    ) external view override returns (bool) {
        return shouldVerify;
    }
}
