// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IMailbox {
    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody
    ) external payable returns (bytes32 messageId);

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody
    ) external view returns (uint256 fee);

    function process(
        bytes calldata metadata,
        bytes calldata message
    ) external payable;

    function localDomain() external view returns (uint32);

    function delivered(bytes32 messageId) external view returns (bool);

    function latestDispatchedId() external view returns (bytes32);

    function defaultIsm() external view returns (address);

    function defaultHook() external view returns (address);

    function requiredHook() external view returns (address);
}
