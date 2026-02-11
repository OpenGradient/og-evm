// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/TEERegistry.sol";
import "../src/ITEERegistry.sol";
import "precompiles/attestation/IAttestationVerifier.sol";
import "precompiles/rsa/IRSAVerifier.sol";

/// @title Mock Attestation Verifier
/// @notice Mock implementation for testing
contract MockAttestationVerifier is IAttestationVerifier {
    bool public shouldReturnValid = true;
    bytes32 public pcrHashToReturn;

    function setShouldReturnValid(bool valid) external {
        shouldReturnValid = valid;
    }

    function setPCRHash(bytes32 pcrHash) external {
        pcrHashToReturn = pcrHash;
    }

    function verifyAttestation(
        bytes calldata,
        bytes calldata,
        bytes calldata,
        bytes calldata
    ) external view override returns (bool valid, bytes32 pcrHash) {
        return (shouldReturnValid, pcrHashToReturn);
    }
}

/// @title Mock RSA Verifier
/// @notice Mock implementation for testing
contract MockRSAVerifier is IRSAVerifier {
    bool public shouldReturnValid = true;

    function setShouldReturnValid(bool valid) external {
        shouldReturnValid = valid;
    }

    function verifyRSAPSS(
        bytes calldata,
        bytes32,
        bytes calldata
    ) external view override returns (bool valid) {
        return shouldReturnValid;
    }
}

/// @title TEERegistryTest
/// @notice Comprehensive Foundry tests for TEERegistry contract
contract TEERegistryTest is Test {
    TEERegistry public registry;
    MockAttestationVerifier public attestationVerifier;
    MockRSAVerifier public rsaVerifier;

    address deployer = address(this);
    address admin = address(0x1);
    address operator = address(0x2);
    address teeOwner = address(0x3);
    address user = address(0x4);

    bytes32 public constant TEE_ADMIN_ROLE = keccak256("TEE_ADMIN_ROLE");
    bytes32 public constant TEE_OPERATOR_ROLE = keccak256("TEE_OPERATOR_ROLE");

    address constant ATTESTATION_VERIFIER_ADDR = 0x0000000000000000000000000000000000000901;
    address constant RSA_VERIFIER_ADDR = 0x0000000000000000000000000000000000000902;

    // Test data
    bytes testPublicKey = hex"1234567890abcdef1234567890abcdef";
    bytes testTLSCert = hex"fedcba0987654321fedcba0987654321";
    bytes testSignature = hex"abcdef1234567890";
    bytes testAttestation = hex"a1b2c3d4";
    bytes32 testTeeId;

    ITEERegistry.PCRMeasurements testPcrs = ITEERegistry.PCRMeasurements({
        pcr0: hex"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
        pcr1: hex"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
        pcr2: hex"567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef12"
    });

    function setUp() public {
        // Deploy mock precompiles
        attestationVerifier = new MockAttestationVerifier();
        rsaVerifier = new MockRSAVerifier();

        // Deploy them at the expected addresses using vm.etch
        vm.etch(ATTESTATION_VERIFIER_ADDR, address(attestationVerifier).code);
        vm.etch(RSA_VERIFIER_ADDR, address(rsaVerifier).code);

        // Deploy registry
        registry = new TEERegistry();

        // Grant roles
        registry.grantRole(TEE_OPERATOR_ROLE, operator);

        // Setup test data
        testTeeId = keccak256(testPublicKey);

        // Setup basic state for most tests
        registry.addTEEType(0, "AWS Nitro");
        registry.setAWSRootCertificate(hex"726f6f74636572743132");

        bytes32 pcrHash = registry.computePCRHash(testPcrs);
        registry.approvePCR(testPcrs, "v1.0.0", bytes32(0), 0);

        // Configure mocks
        MockAttestationVerifier(ATTESTATION_VERIFIER_ADDR).setPCRHash(pcrHash);
        MockAttestationVerifier(ATTESTATION_VERIFIER_ADDR).setShouldReturnValid(true);
        MockRSAVerifier(RSA_VERIFIER_ADDR).setShouldReturnValid(true);
    }

    // ============ Access Control Tests ============

    function test_DeployerHasAdminRoles() public {
        assertTrue(registry.hasRole(registry.DEFAULT_ADMIN_ROLE(), deployer));
        assertTrue(registry.hasRole(TEE_ADMIN_ROLE, deployer));
    }

    function test_GrantTEEOperatorRole() public {
        address newOperator = address(0x5);
        registry.grantRole(TEE_OPERATOR_ROLE, newOperator);
        assertTrue(registry.hasRole(TEE_OPERATOR_ROLE, newOperator));
    }

    function test_RevertWhen_NonAdminGrantsRole() public {
        vm.prank(user);
        vm.expectRevert();
        registry.grantRole(TEE_ADMIN_ROLE, admin);
    }

    // ============ TEE Type Management Tests ============

    function test_AddTEEType() public {
        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEETypeAdded(1, "LLMProxy", block.timestamp);
        registry.addTEEType(1, "LLMProxy");

        assertTrue(registry.isValidTEEType(1));
    }

    function test_RevertWhen_DuplicateTEEType() public {
        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.TEETypeExists.selector, 0));
        registry.addTEEType(0, "AWS Nitro");
    }

    function test_DeactivateTEEType() public {
        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEETypeDeactivated(0, block.timestamp);
        registry.deactivateTEEType(0);

        assertFalse(registry.isValidTEEType(0));
    }

    function test_GetTEETypes() public {
        registry.addTEEType(1, "Validator");

        ITEERegistry.TEETypeInfo[] memory types = registry.getTEETypes();
        assertEq(types.length, 2);
        assertEq(types[0].typeId, 0);
        assertEq(types[0].name, "AWS Nitro");
        assertEq(types[1].typeId, 1);
        assertEq(types[1].name, "Validator");
    }

    // ============ PCR Management Tests ============

    function test_ApprovePCR() public {
        ITEERegistry.PCRMeasurements memory newPcrs = ITEERegistry.PCRMeasurements({
            pcr0: hex"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            pcr1: hex"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
            pcr2: hex"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
        });

        bytes32 expectedHash = registry.computePCRHash(newPcrs);

        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.PCRApproved(expectedHash, "v2.0.0", block.timestamp, 0);
        registry.approvePCR(newPcrs, "v2.0.0", bytes32(0), 0);

        assertTrue(registry.isPCRApproved(newPcrs));
    }

    function test_RevokePCR() public {
        bytes32 pcrHash = registry.computePCRHash(testPcrs);

        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.PCRRevoked(pcrHash, block.timestamp);
        registry.revokePCR(pcrHash);

        assertFalse(registry.isPCRApproved(testPcrs));
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

        registry.approvePCR(oldPcrs, "v1.0.0", bytes32(0), 0);
        bytes32 oldPcrHash = registry.computePCRHash(oldPcrs);
        registry.approvePCR(newPcrs, "v2.0.0", oldPcrHash, 3600);

        assertTrue(registry.isPCRApproved(oldPcrs));
        assertTrue(registry.isPCRApproved(newPcrs));

        vm.warp(block.timestamp + 3601);

        assertFalse(registry.isPCRApproved(oldPcrs));
        assertTrue(registry.isPCRApproved(newPcrs));
    }

    function test_GetActivePCRs() public {
        bytes32[] memory activePCRs = registry.getActivePCRs();
        assertEq(activePCRs.length, 1);
    }

    function test_GetPCRDetails() public {
        bytes32 pcrHash = registry.computePCRHash(testPcrs);
        ITEERegistry.ApprovedPCR memory details = registry.getPCRDetails(pcrHash);

        assertEq(details.pcrHash, pcrHash);
        assertTrue(details.active);
        assertEq(details.version, "v1.0.0");
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

    // ============ TEE Registration Tests ============

    function test_RegisterTEEWithAttestation() public {
        vm.prank(operator);
        vm.expectEmit(true, true, false, false);
        emit ITEERegistry.TEERegistered(testTeeId, operator, operator, "https://tee.example.com", 0, block.timestamp);

        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        assertEq(teeId, testTeeId);
        assertTrue(registry.isActive(teeId));

        ITEERegistry.TEEInfo memory info = registry.getTEE(teeId);
        assertEq(info.owner, operator);
        assertEq(info.paymentAddress, operator);
        assertEq(info.endpoint, "https://tee.example.com");
        assertEq(info.publicKey, testPublicKey);
        assertEq(info.tlsCertificate, testTLSCert);
        assertTrue(info.active);
    }

    function test_RevertWhen_RegisteringWithInvalidTEEType() public {
        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.InvalidTEEType.selector, 99));

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            99
        );
    }

    function test_RevertWhen_RegisteringWithInvalidAttestation() public {
        MockAttestationVerifier(ATTESTATION_VERIFIER_ADDR).setShouldReturnValid(false);

        vm.prank(operator);
        vm.expectRevert(ITEERegistry.InvalidAttestation.selector);

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );
    }

    function test_RevertWhen_RegisteringWithUnapprovedPCR() public {
        bytes32 unapprovedPCRHash = keccak256("unapproved");
        MockAttestationVerifier(ATTESTATION_VERIFIER_ADDR).setPCRHash(unapprovedPCRHash);

        vm.prank(operator);
        vm.expectRevert(ITEERegistry.PCRNotApproved.selector);

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );
    }

    function test_RevertWhen_RegisteringDuplicateTEE() public {
        vm.startPrank(operator);

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.TEEAlreadyExists.selector, testTeeId));

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee2.example.com",
            0
        );

        vm.stopPrank();
    }

    function test_RevertWhen_NonOperatorRegisters() public {
        vm.prank(user);
        vm.expectRevert();

        registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            user,
            "https://tee.example.com",
            0
        );
    }

    // ============ TEE Activation/Deactivation Tests ============

    function test_DeactivateTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.prank(operator);
        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEEDeactivated(teeId, block.timestamp);
        registry.deactivateTEE(teeId);

        assertFalse(registry.isActive(teeId));
    }

    function test_ActivateTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.prank(operator);
        registry.deactivateTEE(teeId);

        vm.prank(operator);
        vm.expectEmit(true, false, false, true);
        emit ITEERegistry.TEEActivated(teeId, block.timestamp);
        registry.activateTEE(teeId);

        assertTrue(registry.isActive(teeId));
    }

    function test_RevertWhen_NonOwnerDeactivatesTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.prank(user);
        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.NotTEEOwner.selector, teeId, user, operator));
        registry.deactivateTEE(teeId);
    }

    function test_AdminCanDeactivateAnyTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        // Admin (deployer) can deactivate
        registry.deactivateTEE(teeId);
        assertFalse(registry.isActive(teeId));
    }

    // ============ Signature Verification Tests ============

    function test_VerifySignature() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        ITEERegistry.VerificationRequest memory req = ITEERegistry.VerificationRequest({
            teeId: teeId,
            requestHash: bytes32(uint256(1)),
            responseHash: bytes32(uint256(2)),
            timestamp: block.timestamp,
            signature: testSignature
        });

        bool valid = registry.verifySignature(req);
        assertTrue(valid);
    }

    function test_VerifySignature_ReturnsFalseForInactiveTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.prank(operator);
        registry.deactivateTEE(teeId);

        ITEERegistry.VerificationRequest memory req = ITEERegistry.VerificationRequest({
            teeId: teeId,
            requestHash: bytes32(uint256(1)),
            responseHash: bytes32(uint256(2)),
            timestamp: block.timestamp,
            signature: testSignature
        });

        bool valid = registry.verifySignature(req);
        assertFalse(valid);
    }

    function test_VerifySignature_ReturnsFalseForOldTimestamp() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        ITEERegistry.VerificationRequest memory req = ITEERegistry.VerificationRequest({
            teeId: teeId,
            requestHash: bytes32(uint256(1)),
            responseHash: bytes32(uint256(2)),
            timestamp: block.timestamp - 7200, // 2 hours old (exceeds MAX_SETTLEMENT_AGE)
            signature: testSignature
        });

        bool valid = registry.verifySignature(req);
        assertFalse(valid);
    }

    function test_VerifySignature_ReturnsFalseForFutureTimestamp() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        ITEERegistry.VerificationRequest memory req = ITEERegistry.VerificationRequest({
            teeId: teeId,
            requestHash: bytes32(uint256(1)),
            responseHash: bytes32(uint256(2)),
            timestamp: block.timestamp + 1000, // Too far in future
            signature: testSignature
        });

        bool valid = registry.verifySignature(req);
        assertFalse(valid);
    }

    function test_VerifySignature_ReturnsFalseForInvalidSignature() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        MockRSAVerifier(RSA_VERIFIER_ADDR).setShouldReturnValid(false);

        ITEERegistry.VerificationRequest memory req = ITEERegistry.VerificationRequest({
            teeId: teeId,
            requestHash: bytes32(uint256(1)),
            responseHash: bytes32(uint256(2)),
            timestamp: block.timestamp,
            signature: testSignature
        });

        bool valid = registry.verifySignature(req);
        assertFalse(valid);
    }

    // ============ Settlement Verification Tests ============

    function test_VerifySettlement() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        bytes32 inputHash = bytes32(uint256(1));
        bytes32 outputHash = bytes32(uint256(2));
        uint256 timestamp = block.timestamp;

        bytes32 settlementHash = keccak256(abi.encodePacked(teeId, inputHash, outputHash, timestamp));

        vm.expectEmit(true, true, true, true);
        emit ITEERegistry.SettlementVerified(teeId, settlementHash, address(this), timestamp);

        bool valid = registry.verifySettlement(teeId, inputHash, outputHash, timestamp, testSignature);
        assertTrue(valid);
    }

    function test_RevertWhen_VerifyingSettlementWithOldTimestamp() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.expectRevert();
        registry.verifySettlement(
            teeId,
            bytes32(uint256(1)),
            bytes32(uint256(2)),
            block.timestamp - 7200,
            testSignature
        );
    }

    function test_RevertWhen_VerifyingSettlementForInactiveTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        vm.prank(operator);
        registry.deactivateTEE(teeId);

        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.TEENotActive.selector, teeId));
        registry.verifySettlement(
            teeId,
            bytes32(uint256(1)),
            bytes32(uint256(2)),
            block.timestamp,
            testSignature
        );
    }

    function test_RevertWhen_ReplayingSettlement() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        bytes32 inputHash = bytes32(uint256(1));
        bytes32 outputHash = bytes32(uint256(2));
        uint256 timestamp = block.timestamp;

        // First verification succeeds
        registry.verifySettlement(teeId, inputHash, outputHash, timestamp, testSignature);

        // Second verification with same data should fail (replay protection)
        vm.expectRevert(ITEERegistry.InvalidSignature.selector);
        registry.verifySettlement(teeId, inputHash, outputHash, timestamp, testSignature);
    }

    function test_RevertWhen_VerifyingSettlementWithInvalidSignature() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        MockRSAVerifier(RSA_VERIFIER_ADDR).setShouldReturnValid(false);

        vm.expectRevert(ITEERegistry.InvalidSignature.selector);
        registry.verifySettlement(
            teeId,
            bytes32(uint256(1)),
            bytes32(uint256(2)),
            block.timestamp,
            testSignature
        );
    }

    // ============ TEE Query Tests ============

    function test_GetTEE() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        ITEERegistry.TEEInfo memory info = registry.getTEE(teeId);

        assertEq(info.teeId, teeId);
        assertEq(info.owner, operator);
        assertEq(info.paymentAddress, operator);
        assertEq(info.endpoint, "https://tee.example.com");
        assertEq(info.publicKey, testPublicKey);
        assertEq(info.tlsCertificate, testTLSCert);
        assertEq(info.teeType, 0);
        assertTrue(info.active);
    }

    function test_RevertWhen_GettingNonexistentTEE() public {
        vm.expectRevert(abi.encodeWithSelector(ITEERegistry.TEENotFound.selector, bytes32(uint256(999))));
        registry.getTEE(bytes32(uint256(999)));
    }

    function test_GetActiveTEEs() public {
        bytes memory pk1 = hex"1111111111111111";
        bytes memory pk2 = hex"2222222222222222";

        vm.startPrank(operator);

        bytes32 teeId1 = registry.registerTEEWithAttestation(
            testAttestation,
            pk1,
            testTLSCert,
            operator,
            "https://tee1.example.com",
            0
        );

        bytes32 teeId2 = registry.registerTEEWithAttestation(
            testAttestation,
            pk2,
            testTLSCert,
            operator,
            "https://tee2.example.com",
            0
        );

        vm.stopPrank();

        bytes32[] memory activeTEEs = registry.getActiveTEEs();
        assertEq(activeTEEs.length, 2);
        assertEq(activeTEEs[0], teeId1);
        assertEq(activeTEEs[1], teeId2);

        // Deactivate one
        vm.prank(operator);
        registry.deactivateTEE(teeId1);

        activeTEEs = registry.getActiveTEEs();
        assertEq(activeTEEs.length, 1);
        assertEq(activeTEEs[0], teeId2);
    }

    function test_GetTEEsByType() public {
        registry.addTEEType(1, "Intel SGX");

        bytes memory pk1 = hex"1111111111111111";
        bytes memory pk2 = hex"2222222222222222";

        vm.startPrank(operator);

        bytes32 teeId1 = registry.registerTEEWithAttestation(
            testAttestation,
            pk1,
            testTLSCert,
            operator,
            "https://tee1.example.com",
            0  // AWS Nitro
        );

        bytes32 teeId2 = registry.registerTEEWithAttestation(
            testAttestation,
            pk2,
            testTLSCert,
            operator,
            "https://tee2.example.com",
            1  // Intel SGX
        );

        vm.stopPrank();

        bytes32[] memory nitroTEEs = registry.getTEEsByType(0);
        assertEq(nitroTEEs.length, 1);
        assertEq(nitroTEEs[0], teeId1);

        bytes32[] memory sgxTEEs = registry.getTEEsByType(1);
        assertEq(sgxTEEs.length, 1);
        assertEq(sgxTEEs[0], teeId2);
    }

    function test_GetPublicKey() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        bytes memory publicKey = registry.getPublicKey(teeId);
        assertEq(publicKey, testPublicKey);
    }

    function test_GetTLSCertificate() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        bytes memory cert = registry.getTLSCertificate(teeId);
        assertEq(cert, testTLSCert);
    }

    function test_IsActive() public {
        vm.prank(operator);
        bytes32 teeId = registry.registerTEEWithAttestation(
            testAttestation,
            testPublicKey,
            testTLSCert,
            operator,
            "https://tee.example.com",
            0
        );

        assertTrue(registry.isActive(teeId));

        vm.prank(operator);
        registry.deactivateTEE(teeId);

        assertFalse(registry.isActive(teeId));
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
}
