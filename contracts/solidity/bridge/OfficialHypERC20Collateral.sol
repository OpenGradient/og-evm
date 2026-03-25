// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import "../hyperlane/token/HypERC20Collateral.sol";

/// @notice Thin wrapper that compiles Hyperlane's official HypERC20Collateral
/// source inside this repo's Hardhat workspace.
contract OfficialHypERC20Collateral is HypERC20Collateral {
    constructor(
        address erc20,
        uint256 scaleNumerator,
        uint256 scaleDenominator,
        address mailbox
    ) HypERC20Collateral(erc20, scaleNumerator, scaleDenominator, mailbox) {}
}
