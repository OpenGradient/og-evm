// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../IBridgeMint.sol";

/// @title MockBridgeMint
/// @notice Test-only mock for the bridge precompile surface.
contract MockBridgeMint is IBridgeMint {
    bool public enabled;
    bool public mintResult;
    bool public burnResult;
    bool public initialized;

    address public lastMintRecipient;
    uint256 public lastMintAmount;
    address public lastMintCaller;
    uint256 public mintCalls;

    uint256 public lastBurnAmount;
    address public lastBurnCaller;
    uint256 public burnCalls;

    function initialize() external {
        enabled = true;
        mintResult = true;
        burnResult = true;
        initialized = true;
    }

    function setEnabled(bool _enabled) external {
        enabled = _enabled;
    }

    function setMintResult(bool _mintResult) external {
        mintResult = _mintResult;
    }

    function setBurnResult(bool _burnResult) external {
        burnResult = _burnResult;
    }

    function mintNative(address recipient, uint256 amount) external returns (bool success) {
        lastMintRecipient = recipient;
        lastMintAmount = amount;
        lastMintCaller = msg.sender;
        mintCalls += 1;

        emit MintNative(recipient, amount);

        return mintResult;
    }

    function burnNative(uint256 amount) external returns (bool success) {
        lastBurnAmount = amount;
        lastBurnCaller = msg.sender;
        burnCalls += 1;

        emit BurnNative(msg.sender, amount);

        return burnResult;
    }

    function isEnabled() external view returns (bool) {
        return enabled;
    }
}
