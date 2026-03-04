// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../../TEEInferenceVerifier.sol";
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

/// @title TEEInferenceVerifier Test Suite
contract TEEInferenceVerifierTest is Test {
    TEEInferenceVerifier public verifier;
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
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);

    function setUp() public {
        // Warp to a realistic timestamp to avoid underflow in timestamp checks
        vm.warp(1700000000); // Nov 2023
        
        // Deploy mock contracts
        mockRegistry = new MockTEERegistry();
        mockVerifier = new MockTEEVerifier();
        
        // Deploy verifier contract
        vm.prank(admin);
        verifier = new TEEInferenceVerifier(address(mockRegistry));
        
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
        assertEq(address(verifier.registry()), address(mockRegistry));
    }
    
    function test_Constructor_GrantsAdminRole() public view {
        assertTrue(verifier.hasRole(verifier.DEFAULT_ADMIN_ROLE(), admin));
    }

    function test_Constructor_RevertIfZeroAddress() public {
        vm.expectRevert(TEEInferenceVerifier.InvalidRegistryAddress.selector);
        new TEEInferenceVerifier(address(0));
    }

    // ============ Constants Tests ============

    function test_Constants_MaxInferenceAge() public view {
        assertEq(verifier.MAX_INFERENCE_AGE(), 1 hours);
    }
    
    function test_Constants_FutureTolerance() public view {
        assertEq(verifier.FUTURE_TOLERANCE(), 5 minutes);
    }
    
    function test_Constants_VerifierAddress() public view {
        assertEq(
            address(verifier.VERIFIER()),
            address(0x0000000000000000000000000000000000000900)
        );
    }

    // ============ setRegistry Tests ============

    function test_SetRegistry_Success() public {
        address newRegistry = address(0x999);
        
        vm.expectEmit(true, true, false, false);
        emit RegistryUpdated(address(mockRegistry), newRegistry);
        
        vm.prank(admin);
        verifier.setRegistry(newRegistry);
        
        assertEq(address(verifier.registry()), newRegistry);
    }
    
    function test_SetRegistry_RevertIfNotAdmin() public {
        vm.prank(user);
        vm.expectRevert();
        verifier.setRegistry(address(0x999));
    }

    function test_SetRegistry_RevertIfZeroAddress() public {
        vm.prank(admin);
        vm.expectRevert(TEEInferenceVerifier.InvalidRegistryAddress.selector);
        verifier.setRegistry(address(0));
    }

    // ============ computeMessageHash Tests ============

    function test_ComputeMessageHash_Deterministic() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        
        assertEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentInputs() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = verifier.computeMessageHash(keccak256("different"), OUTPUT_HASH, timestamp);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentOutputs() public view {
        uint256 timestamp = block.timestamp;
        
        bytes32 hash1 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        bytes32 hash2 = verifier.computeMessageHash(INPUT_HASH, keccak256("different"), timestamp);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_DifferentTimestamps() public view {
        bytes32 hash1 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, 1000);
        bytes32 hash2 = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, 2000);
        
        assertNotEq(hash1, hash2);
    }
    
    function test_ComputeMessageHash_MatchesExpected() public view {
        uint256 timestamp = 1234567890;
        
        bytes32 expected = keccak256(abi.encodePacked(INPUT_HASH, OUTPUT_HASH, timestamp));
        bytes32 actual = verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp);
        
        assertEq(actual, expected);
    }

    // ============ verifySignature Tests ============

    function test_VerifySignature_Success() public view {
        uint256 timestamp = block.timestamp;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }
    
    function test_VerifySignature_ReturnsFalseIfTEENotActive() public {
        mockRegistry.setActive(TEE_ID, false);
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }
    
    function test_VerifySignature_ReturnsFalseIfInvalidSignature() public {
        // Set the mock verifier to return false
        vm.store(
            address(0x0000000000000000000000000000000000000900),
            bytes32(uint256(0)),
            bytes32(uint256(0)) // shouldVerify = false
        );
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            block.timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }

    function test_VerifySignature_ReturnsFalseIfTimestampTooOld() public view {
        uint256 oldTimestamp = block.timestamp - 2 hours;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            oldTimestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }
    
    function test_VerifySignature_ReturnsFalseIfTimestampInFuture() public view {
        uint256 futureTimestamp = block.timestamp + 10 minutes;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            futureTimestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }
    
    function test_VerifySignature_AcceptsTimestampAtMinBoundary() public view {
        uint256 timestamp = block.timestamp - 1 hours;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }
    
    function test_VerifySignature_AcceptsTimestampAtMaxBoundary() public view {
        uint256 timestamp = block.timestamp + 5 minutes;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }

    // ============ Fuzz Tests ============

    function testFuzz_ComputeMessageHash(
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp
    ) public view {
        bytes32 expected = keccak256(abi.encodePacked(inputHash, outputHash, timestamp));
        bytes32 actual = verifier.computeMessageHash(inputHash, outputHash, timestamp);
        
        assertEq(actual, expected);
    }
    
    function testFuzz_VerifySignature_ValidTimestampRange(uint256 offset) public view {
        // Bound offset to valid range (0 to MAX_INFERENCE_AGE)
        offset = bound(offset, 0, 1 hours);
        
        uint256 timestamp = block.timestamp - offset;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }
    
    function testFuzz_VerifySignature_ValidFutureTolerance(uint256 offset) public view {
        // Bound offset to valid range (0 to FUTURE_TOLERANCE)
        offset = bound(offset, 0, 5 minutes);
        
        uint256 timestamp = block.timestamp + offset;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertTrue(result);
    }

    function testFuzz_VerifySignature_InvalidOldTimestamp(uint256 offset) public view {
        // Bound offset to invalid range (more than MAX_INFERENCE_AGE)
        offset = bound(offset, 1 hours + 1, 10 hours);
        
        uint256 timestamp = block.timestamp - offset;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }

    function testFuzz_VerifySignature_InvalidFutureTimestamp(uint256 offset) public view {
        // Bound offset to invalid range (more than FUTURE_TOLERANCE)
        offset = bound(offset, 5 minutes + 1, 10 hours);
        
        uint256 timestamp = block.timestamp + offset;
        
        bool result = verifier.verifySignature(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            timestamp,
            MOCK_SIGNATURE
        );
        
        assertFalse(result);
    }
}