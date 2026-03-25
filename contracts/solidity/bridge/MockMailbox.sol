// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./IMailbox.sol";
import "./IMessageRecipient.sol";

/// @title MockMailbox
/// @notice Local test helper for bridge integration tests.
contract MockMailbox is IMailbox {
    uint32 public immutable localDomain;

    uint256 public quoteFee;
    uint256 public nonce;

    bytes32 public latestDispatchedId;

    address public defaultIsm;
    address public defaultHook;
    address public requiredHook;

    address public lastSender;
    uint32 public lastDestination;
    bytes32 public lastRecipient;
    bytes public lastBody;
    uint256 public lastValue;

    mapping(bytes32 => bool) public delivered;

    error Unsupported();

    constructor(uint32 _localDomain, uint256 _quoteFee) {
        localDomain = _localDomain;
        quoteFee = _quoteFee;
    }

    function setQuoteFee(uint256 _quoteFee) external {
        quoteFee = _quoteFee;
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody
    ) external payable override returns (bytes32 messageId) {
        return _dispatch(destinationDomain, recipientAddress, messageBody);
    }

    function _dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes memory messageBody
    ) internal returns (bytes32 messageId) {
        lastSender = msg.sender;
        lastDestination = destinationDomain;
        lastRecipient = recipientAddress;
        lastBody = messageBody;
        lastValue = msg.value;

        messageId = keccak256(
            abi.encode(msg.sender, destinationDomain, recipientAddress, messageBody, nonce)
        );
        nonce += 1;
        latestDispatchedId = messageId;
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata
    ) external view override returns (uint256 fee) {
        return quoteFee;
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata
    ) external payable returns (bytes32 messageId) {
        return _dispatch(destinationDomain, recipientAddress, messageBody);
    }

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata
    ) external view returns (uint256 fee) {
        return this.quoteDispatch(destinationDomain, recipientAddress, messageBody);
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata,
        address
    ) external payable returns (bytes32 messageId) {
        return _dispatch(destinationDomain, recipientAddress, messageBody);
    }

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata,
        address
    ) external view returns (uint256 fee) {
        return this.quoteDispatch(destinationDomain, recipientAddress, messageBody);
    }

    function process(bytes calldata, bytes calldata) external payable override {
        revert Unsupported();
    }

    function recipientIsm(address) external view returns (address) {
        return defaultIsm;
    }

    function deliver(
        address recipient,
        uint32 origin,
        bytes32 sender,
        bytes calldata body
    ) external payable returns (bytes32 messageId) {
        messageId = keccak256(abi.encode(origin, sender, recipient, body, nonce));
        nonce += 1;
        delivered[messageId] = true;
        IMessageRecipient(recipient).handle{value: msg.value}(origin, sender, body);
    }
}
