// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../../TEESettlement.sol";
import "../../TEERegistry.sol";
import "../../precompiles/tee/ITEEVerifier.sol";

/// @title Mock TEE Verifier for testing
/// @notice Simulates the precompile behavior for unit tests
contract MockTEEVerifier is ITEEVerifier {
    bool public shouldVerify = true;
    
    function setShouldVerify(bool _shouldVerify) external {
        shouldVerify = _shouldVerify;
    }
    
    function verifyRSAPSS(
        bytes calldata, /* publicKeyDER */
        bytes32, /* messageHash */
        bytes calldata /* signature */
    ) external view override returns (bool valid) {
        return shouldVerify;
    }
    
    function verifyAttestation(
        bytes calldata, /* attestationDocument */
        bytes calldata, /* signingPublicKey */
        bytes calldata, /* tlsCertificate */
        bytes calldata  /* rootCertificate */
    ) external pure override returns (bool valid, bytes32 pcrHash) {
        return (true, bytes32(0));
    }
}

/// @title Mock TEE Registry for testing
contract MockTEERegistry {
    mapping(bytes32 => bool) public activeStatus;
    mapping(bytes32 => bytes) public publicKeys;
    
    function setActive(bytes32 teeId, bool status) external {
        activeStatus[teeId] = status;
    }
    
    function setPublicKey(bytes32 teeId, bytes memory pubKey) external {
        publicKeys[teeId] = pubKey;
    }
    
    function isActive(bytes32 teeId) external view returns (bool) {
        return activeStatus[teeId];
    }
    
    function getPublicKey(bytes32 teeId) external view returns (bytes memory) {
        return publicKeys[teeId];
    }
}

/// @title TEESettlement Test Suite
contract TEESettlementTest is Test {
    TEESettlement public settlement;
    MockTEERegistry public mockRegistry;
    MockTEEVerifier public mockVerifier;
    
    address public admin = address(0x1);
    address public user = address(0x2);
    
    bytes32 public constant TEE_ID = keccak256("test-tee-1");
    bytes32 public constant INPUT_HASH = keccak256("input-data");
    bytes32 public constant OUTPUT_HASH = keccak256("output-data");
    bytes public constant MOCK_PUB_KEY = hex"deadbeef";
    bytes public constant MOCK_SIGNATURE = hex"cafebabe";
    
    // Events to test
    event SettlementVerified(
        bytes32 indexed teeId,
        bytes32 indexed settlementHash,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    );
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);

    function setUp() public {
        // Warp to a realistic timestamp to avoid underflow in timestamp checks
        vm.warp(1700000000); // Nov 2023
        
        // Deploy mock contracts
        mockRegistry = new MockTEERegistry();
        mockVerifier = new MockTEEVerifier();
        
        // Deploy settlement contract
        vm.prank(admin);
        settlement = new TEESettlement(address(mockRegistry));
        
        // Setup default TEE state
        mockRegistry.setActive(TEE_ID, true);
        mockRegistry.setPublicKey(TEE_ID, MOCK_PUB_KEY);
        
        // Mock the precompile address
        vm.etch(address(0x0000000000000000000000000000000000000900), address(mockVerifier).code);
        
        // Copy storage from mockVerifier to precompile address
        bytes32 slot0 = vm.load(address(mockVerifier), bytes32(uint256(0)));
        vm.store(address(0x0000000000000000000000000000000000000900), bytes32(uint256(0)), slot0);
    }

    // ============ Constructor Tests ============

    function test_Constructor_SetsRegistry() public view {
        assertEq(address(settlement.registry()), address(mockRegistry));
    }
    
    function test_Constructor_GrantsAdminRole() public view {
        assertTrue(settlement.hasRole(settlement.DEFAULT_ADMIN_ROLE(), admin));
    }

    // ============ Constants Tests ============

    function test_Constants_MaxSettlementAge() public view {
        assertEq(settlement.MAX_SETTLEMENT_AGE(), 1 hours);
    }
    
    function test_Constants_FutureTolerance() public view {
        assertEq(settlement.FUTURE_TOLERANCE(), 5 minutes);
    }
    
    function test_Constants_VerifierAddress() public view {
        assertEq(
            address(settlement.VERIFIER()),
            address(0x0000000000000000000000000000000000000900)
        );
    }

    // ============ setRegistry Tests ============

    function test_SetRegistry_Success() public {
        address newRegistry = address(0x999);
        
        vm.expectEmit(true, true, false, false);
        emit RegistryUpdated(address(mockRegistry), newRegistry);
        
        vm.prank(admin);
        settlement.setRegistry(newRegistry);
        
        assertEq(address(settlement.registry()), newRegistry);
    }
    
    function test_SetRegistry_RevertIfNotAdmin() public {
        vm.prank(user);
        vm.expectRevert();
        settlement.setRegistry(address(0x999));
    }

    function test_SetRegistry_RevertIfZeroAddress() public {
        vm.prank(admin);
        vm.expectRevert();
        settlement.setRegistry(address(0));
    }
    // ============ computeMessageHash Tests ============

    function test_ComputeMessageHash_Deterministic() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        
        assertEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentInputs() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = settlement.computeMessageHash(keccak256("different"), OUTPUT_HASH, timestamp);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentOutputs() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = settlement.computeMessageHash(INPUT_HASH, keccak256("different"), timestamp);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentTimestamps() public view {
        bytes32 hash1 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, 1000);
        bytes32 hash2 = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, 2000);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_MatchesExpected() public view {
        uint256 timestamp = 1234567890;
        
        bytes32 expected = keccak256(abi.encodePacked(INPUT_HASH, OUTPUT_HASH, timestamp));
        bytes32 actual = settlement.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        
        assertEq(actual, expected);
    }

    // ============ verifySignature Tests ============

    function test_VerifySignature_Success() public view {
        uint256 timestamp = block.timestamp;
        
        bool result = settlement.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }
    
    function test_VerifySignature_FailsIfTEENotActive() public {
        mockRegistry.setActive(TEE_ID, false);
        
        bool result = settlement.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }
    
    function test_VerifySignature_FailsIfInvalidSignature() public {
        // Set the mock verifier to return false
        MockTEEVerifier verifierAtPrecompile = MockTEEVerifier(
            address(0x0000000000000000000000000000000000000900)
        );
        
        // We need to update the storage at the precompile address
        vm.store(
            address(0x0000000000000000000000000000000000000900),
            bytes32(uint256(0)),
            bytes32(uint256(0)) // shouldVerify = false
        );
        
        bool result = settlement.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }

    // ============ verifySettlement Tests ============

    function test_VerifySettlement_Success() public {
        uint256 timestamp = block.timestamp;
        
        bytes32 expectedSettlementHash = keccak256(
            abi.encodePacked(TEE_ID, INPUT_HASH, OUTPUT_HASH, timestamp)
        );
        
        vm.expectEmit(true, true, false, true);
        emit SettlementVerified(
            TEE_ID,
            expectedSettlementHash,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp
        );
        
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertEq(settlementHash, expectedSettlementHash);
        assertTrue(settlement.settlementUsed(settlementHash));
    }
    
    function test_VerifySettlement_RevertIfTEENotActive() public {
        mockRegistry.setActive(TEE_ID, false);
        
        vm.expectRevert(abi.encodeWithSelector(
            TEESettlement.TEENotActive.selector,
            TEE_ID
        ));
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
    }
    
    function test_VerifySettlement_RevertIfTimestampTooOld() public {
        uint256 oldTimestamp = block.timestamp - 2 hours;
        uint256 minAllowed = block.timestamp - 1 hours;
        
        vm.expectRevert(abi.encodeWithSelector(
            TEESettlement.TimestampTooOld.selector,
            oldTimestamp,
            minAllowed
        ));
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            oldTimestamp,
            MOCK_SIGNATURE
        );
    }
    
    function test_VerifySettlement_RevertIfTimestampInFuture() public {
        uint256 futureTimestamp = block.timestamp + 10 minutes;
        uint256 maxAllowed = block.timestamp + 5 minutes;
        
        vm.expectRevert(abi.encodeWithSelector(
            TEESettlement.TimestampInFuture.selector,
            futureTimestamp,
            maxAllowed
        ));
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            futureTimestamp,
            MOCK_SIGNATURE
        );
    }
    
    function test_VerifySettlement_RevertIfAlreadyUsed() public {
        uint256 timestamp = block.timestamp;
        
        // First settlement succeeds
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        // Second settlement with same params reverts
        vm.expectRevert(abi.encodeWithSelector(
            TEESettlement.SettlementAlreadyUsed.selector,
            settlementHash
        ));
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
    }
    
    function test_VerifySettlement_RevertIfInvalidSignature() public {
        // Set verifier to fail
        vm.store(
            address(0x0000000000000000000000000000000000000900),
            bytes32(uint256(0)),
            bytes32(uint256(0)) // shouldVerify = false
        );
        
        vm.expectRevert(TEESettlement.InvalidSignature.selector);
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
    }
    
    function test_VerifySettlement_AcceptsTimestampAtMinBoundary() public {
        uint256 timestamp = block.timestamp - 1 hours;
        
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(settlement.settlementUsed(settlementHash));
    }
    
    function test_VerifySettlement_AcceptsTimestampAtMaxBoundary() public {
        uint256 timestamp = block.timestamp + 5 minutes;
        
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(settlement.settlementUsed(settlementHash));
    }
    
    function test_VerifySettlement_DifferentTEEsDifferentHashes() public {
        bytes32 teeId2 = keccak256("test-tee-2");
        mockRegistry.setActive(teeId2, true);
        mockRegistry.setPublicKey(teeId2, MOCK_PUB_KEY);
        
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        bytes32 hash2 = settlement.verifySettlement(
            teeId2,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertNotEq(hash1, hash2);
    }

    // ============ Fuzz Tests ============

    function testFuzz_ComputeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public view {
        bytes32 expected = keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
        bytes32 actual = settlement.computeMessageHash(inputHash, outputHash, timestamp);
        
        assertEq(actual, expected);
    }
    
    function testFuzz_VerifySettlement_TimestampBounds(uint256 offset) public {
        // Bound offset to valid range (0 to MAX_SETTLEMENT_AGE)
        offset = bound(offset, 0, 1 hours);
        
        uint256 timestamp = block.timestamp - offset;
        
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(settlement.settlementUsed(settlementHash));
    }
    
    function testFuzz_VerifySettlement_FutureTolerance(uint256 offset) public {
        // Bound offset to valid range (0 to FUTURE_TOLERANCE)
        offset = bound(offset, 0, 5 minutes);
        
        uint256 timestamp = block.timestamp + offset;
        
        bytes32 settlementHash = settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(settlement.settlementUsed(settlementHash));
    }

    // ============ Edge Case Tests ============

    function test_VerifySettlement_MultipleSettlementsFromSameTEE() public {
        // Multiple settlements with different data from same TEE should work
        for (uint256 i = 0; i < 5; i++) {
            bytes32 inputHash = keccak256(abi.encodePacked("input", i));
            bytes32 outputHash = keccak256(abi.encodePacked("output", i));
            
            settlement.verifySettlement(
                TEE_ID,
                inputHash,
                outputHash,
                block.timestamp,
                MOCK_SIGNATURE
            );
        }
    }
    
    function test_VerifySettlement_SameDataDifferentTimestamps() public {
        // Same input/output but different timestamps should work
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
        
        // Warp time forward
        vm.warp(block.timestamp + 1);
        
        settlement.verifySettlement(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
    }
}