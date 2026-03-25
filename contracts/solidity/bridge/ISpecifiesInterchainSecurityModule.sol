// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./IInterchainSecurityModule.sol";

interface ISpecifiesInterchainSecurityModule {
    function interchainSecurityModule()
        external
        view
        returns (IInterchainSecurityModule);
}
