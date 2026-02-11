// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/TEERegistry.sol";
import "../src/ITEERegistry.sol";

/// @title TEERegistryTest
/// @notice Foundry tests for TEERegistry contract
contract TEERegistryTest is Test {
    TEERegistry public registry;

    address deployer = address(this);
    address admin = address(0x1);
    address user = address(0x2);
    address teeOwner = address(0x3);

    bytes32 public constant TEE_ADMIN_ROLE = keccak256("TEE_ADMIN_ROLE");

    function setUp() public {
        registry = new TEERegistry();
        // Deployer (this contract) automatically gets DEFAULT_ADMIN_ROLE and TEE_ADMIN_ROLE
    }

    // ============ Access Control Tests ============

    function test_DeployerHasAdminRoles() public {
        assertTrue(registry.hasRole(registry.DEFAULT_ADMIN_ROLE(), deployer));
        assertTrue(registry.hasRole(TEE_ADMIN_ROLE, deployer));
    }

    function test_GrantTEEAdminRole() public {
        // Deployer grants TEE_ADMIN_ROLE to admin
        registry.grantRole(TEE_ADMIN_ROLE, admin);

        assertTrue(registry.hasRole(TEE_ADMIN_ROLE, admin));
    }

    function test_RevokeTEEAdminRole() public {
        // Grant then revoke
        registry.grantRole(TEE_ADMIN_ROLE, admin);
        registry.revokeRole(TEE_ADMIN_ROLE, admin);

        assertFalse(registry.hasRole(TEE_ADMIN_ROLE, admin));
    }

    function test_RevertWhen_NonAdminGrantsRole() public {
        // User tries to grant role without permission
        vm.prank(user);
        vm.expectRevert();  // AccessControl will revert
        registry.grantRole(TEE_ADMIN_ROLE, admin);
    }

    // ============ TEE Type Management Tests ============

    function test_AddTEEType() public {
        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEETypeAdded(0, "LLMProxy", block.timestamp);
        registry.addTEEType(0, "LLMProxy");

        assertTrue(registry.isValidTEEType(0));
    }

    function test_RevertWhen_DuplicateTEEType() public {
        registry.addTEEType(0, "LLMProxy");

vm.expectRevert(abi.encodeWithSelector(ITEERegistry.TEETypeExists.selector, 0));
        registry.addTEEType(0, "LLMProxy");
    }

    function test_DeactivateTEEType() public {
        registry.addTEEType(0, "LLMProxy");

vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEETypeDeactivated(0, block.timestamp);
        registry.deactivateTEEType(0);

        assertFalse(registry.isValidTEEType(0));
    }

    function test_GetTEETypes() public {
        registry.addTEEType(0, "LLMProxy");

registry.addTEEType(1, "Validator");

        ITEERegistry.TEETypeInfo[] memory types = registry.getTEETypes();
        assertEq(types.length, 2);
        assertEq(types[0].typeId, 0);
        assertEq(types[0].name, "LLMProxy");
        assertEq(types[1].typeId, 1);
        assertEq(types[1].name, "Validator");
    }

    // ============ PCR Management Tests ============

    function test_ApprovePCR() public {

        ITEERegistry.PCRMeasurements memory pcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
            pcr1: hex"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
            pcr2: hex"567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12"
        });

        bytes32 expectedHash = registry.computePCRHash(pcrs);

vm.expectEmit(true, false, false, true);
        emit ITEERegistry.PCRApproved(expectedHash, "v1.0.0", block.timestamp, 0);
        registry.approvePCR(pcrs, "v1.0.0", bytes32(0), 0);

        assertTrue(registry.isPCRApproved(pcrs));
    }

    function test_RevokePCR() public {

        ITEERegistry.PCRMeasurements memory pcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
            pcr1: hex"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
            pcr2: hex"567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12"
        });

registry.approvePCR(pcrs, "v1.0.0", bytes32(0), 0);

        bytes32 pcrHash = registry.computePCRHash(pcrs);

vm.expectEmit(true, false, false, true);
        emit ITEERegistry.PCRRevoked(pcrHash, block.timestamp);
        registry.revokePCR(pcrHash);

        assertFalse(registry.isPCRApproved(pcrs));
    }

    function test_PCRGracePeriod() public {

        ITEERegistry.PCRMeasurements memory oldPcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"1111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111",
            pcr1: hex"2222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222222",
            pcr2: hex"3333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333"
        });

        ITEERegistry.PCRMeasurements memory newPcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"4444444444444444444444444444444444444444444444444444444444444444444444444444444444444444444444",
            pcr1: hex"5555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555555",
            pcr2: hex"6666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666"
        });

        // Approve old PCR
registry.approvePCR(oldPcrs, "v1.0.0", bytes32(0), 0);

        bytes32 oldPcrHash = registry.computePCRHash(oldPcrs);

        // Approve new PCR with grace period for old one
registry.approvePCR(newPcrs, "v2.0.0", oldPcrHash, 3600); // 1 hour grace

        // Both should be valid during grace period
        assertTrue(registry.isPCRApproved(oldPcrs));
        assertTrue(registry.isPCRApproved(newPcrs));

        // Warp past grace period
        vm.warp(block.timestamp + 3601);

        // Old should be expired, new still valid
        assertFalse(registry.isPCRApproved(oldPcrs));
        assertTrue(registry.isPCRApproved(newPcrs));
    }

    // ============ Utility Tests ============

    function test_ComputePCRHash() public {
        ITEERegistry.PCRMeasurements memory pcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"1234",
            pcr1: hex"5678",
            pcr2: hex"90ab"
        });

        bytes32 hash = registry.computePCRHash(pcrs);
        bytes32 expected = keccak256(abi.encodePacked(pcrs.pcr0, pcrs.pcr1, pcrs.pcr2));

        assertEq(hash, expected);
    }

    function test_ComputeTEEId() public {
        bytes memory publicKey = hex"1234567890abcdef";
        bytes32 teeId = registry.computeTEEId(publicKey);
        bytes32 expected = keccak256(publicKey);

        assertEq(teeId, expected);
    }

    function test_ComputeMessageHash() public {
        bytes32 inputHash = bytes32(uint256(1));
        bytes32 outputHash = bytes32(uint256(2));
        uint256 timestamp = 12345;

        bytes32 hash = registry.computeMessageHash(inputHash, outputHash, timestamp);
        bytes32 expected = keccak256(abi.encodePacked(inputHash, outputHash, timestamp));

        assertEq(hash, expected);
    }

    // ============ Certificate Management Tests ============

    function test_SetAWSRootCertificate() public {
        bytes memory cert = hex"1234567890abcdef";

        vm.expectEmit(true, true, false, true);
        emit ITEERegistry.AWSRootCertificateUpdated(keccak256(cert), deployer, block.timestamp);
        registry.setAWSRootCertificate(cert);

        bytes32 certHash = registry.getAWSRootCertificateHash();
        assertEq(certHash, keccak256(cert));
    }

    function test_RevertWhen_SettingEmptyCertificate() public {
        vm.expectRevert(ITEERegistry.RootCertificateNotSet.selector);
        registry.setAWSRootCertificate("");
    }
}
