// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity >=0.8.18;

/// @dev The IBridgeMint contract's address.
address constant IBRIDGE_MINT_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000000A00;

/// @dev The IBridgeMint contract's instance.
IBridgeMint constant IBRIDGE_MINT_CONTRACT = IBridgeMint(IBRIDGE_MINT_PRECOMPILE_ADDRESS);

/// @title IBridgeMint
/// @dev Interface for the bridge precompile that mints and burns the chain-native token.
/// @custom:address 0x0000000000000000000000000000000000000A00
interface IBridgeMint {
    /// @dev Emitted when native tokens are minted to a recipient.
    event MintNative(address indexed recipient, uint256 amount);

    /// @dev Emitted when native tokens are burned from a sender.
    event BurnNative(address indexed sender, uint256 amount);

    /// @notice Mint native tokens to a recipient.
    /// @dev Only callable by the authorized bridge contract.
    /// @param recipient The recipient of the minted native tokens.
    /// @param amount The amount of native tokens to mint.
    /// @return success Whether the mint succeeded.
    function mintNative(address recipient, uint256 amount) external returns (bool success);

    /// @notice Burn native tokens held by the authorized bridge contract.
    /// @dev Only callable by the authorized bridge contract.
    /// @param amount The amount of native tokens to burn.
    /// @return success Whether the burn succeeded.
    function burnNative(uint256 amount) external returns (bool success);

    /// @notice Returns whether bridge operations are enabled.
    function isEnabled() external view returns (bool enabled);
}
