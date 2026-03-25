// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

library TokenMessage {
    uint8 internal constant RECIPIENT_OFFSET = 0;
    uint8 internal constant AMOUNT_OFFSET = 32;

    function format(
        bytes32 _recipient,
        uint256 _amount
    ) internal pure returns (bytes memory) {
        return abi.encodePacked(_recipient, _amount);
    }

    function recipient(bytes calldata _message) internal pure returns (bytes32) {
        return bytes32(_message[RECIPIENT_OFFSET:RECIPIENT_OFFSET + 32]);
    }

    function amount(bytes calldata _message) internal pure returns (uint256) {
        return uint256(bytes32(_message[AMOUNT_OFFSET:AMOUNT_OFFSET + 32]));
    }
}
