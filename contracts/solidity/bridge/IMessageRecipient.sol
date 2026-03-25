// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IMessageRecipient {
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _body
    ) external payable;
}
