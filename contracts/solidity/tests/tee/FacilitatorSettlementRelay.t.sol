// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../../FacilitatorSettlementRelay.sol";

/// @title Mock Settlement Contract for relay tests
contract MockSettlementContract is ISettlementContract {
    bool public shouldVerify = true;

    function setShouldVerify(bool _shouldVerify) external {
        shouldVerify = _shouldVerify;
    }

    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata /* signature */
    ) external pure override returns (bytes32 settlementHash) {
        return keccak256(abi.encodePacked(teeId, inputHash, outputHash, timestamp));
    }

    function verifySignature(
        bytes32, /* teeId */
        bytes32, /* inputHash */
        bytes32, /* outputHash */
        uint256, /* timestamp */
        bytes calldata /* signature */
    ) external view override returns (bool) {
        return shouldVerify;
    }
}

/// @title FacilitatorSettlementRelay Test Suite
contract FacilitatorSettlementRelayTest is Test {
    FacilitatorSettlementRelay public relay;
    MockSettlementContract public mockSettlement;

    address public admin = address(0x1);
    address public user = address(0x2);

    bytes32 public constant TEE_ID = keccak256("test-tee");
    bytes32 public constant INPUT_HASH = keccak256("input");
    bytes32 public constant OUTPUT_HASH = keccak256("output");
    uint256 public constant SETTLEMENT_TIMESTAMP = 1700000000;
    bytes public constant WALRUS_BLOB_ID = bytes("test_blob_id");
    bytes public constant SIGNATURE = bytes("test_signature");

    event BatchSettlement(bytes32 indexed merkleRoot, uint256 batchSize, bytes walrusBlobId);

    event IndividualSettlement(
        bytes32 indexed teeId,
        address indexed ethAddress,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes walrusBlobId,
        bytes signature
    );

    function setUp() public {
        mockSettlement = new MockSettlementContract();

        vm.prank(admin);
        relay = new FacilitatorSettlementRelay(address(mockSettlement));
    }

    // ============ Constructor Tests ============

    function test_Constructor_SetsSettlementContract() public view {
        assertEq(address(relay.SETTLEMENT_CONTRACT()), address(mockSettlement));
    }

    function test_Constructor_GrantsRoles() public view {
        assertTrue(relay.hasRole(relay.DEFAULT_ADMIN_ROLE(), admin));
        assertTrue(relay.hasRole(relay.SETTLEMENT_RELAY_ROLE(), admin));
    }

    function test_Constructor_RevertIfSettlementContractIsZeroAddress() public {
        vm.expectRevert(bytes("Invalid settlement contract"));
        new FacilitatorSettlementRelay(address(0));
    }

    // ============ batchSettle Tests ============

    function test_BatchSettle_EmitsBatchSettlement() public {
        bytes32 merkleRoot = keccak256("batch-root");
        uint256 batchSize = 2;

        vm.expectEmit(true, false, false, true);
        emit BatchSettlement(merkleRoot, batchSize, WALRUS_BLOB_ID);

        vm.prank(admin);
        relay.batchSettle(merkleRoot, batchSize, WALRUS_BLOB_ID);
    }

    function test_BatchSettle_RevertIfCallerDoesNotHaveRelayRole() public {
        bytes32 merkleRoot = keccak256("batch-root");
        uint256 batchSize = 2;

        vm.startPrank(user);
        vm.expectRevert(
            abi.encodeWithSelector(
                AccessControl.AccessControlUnauthorizedAccount.selector,
                user,
                relay.SETTLEMENT_RELAY_ROLE()
            )
        );
        relay.batchSettle(merkleRoot, batchSize, WALRUS_BLOB_ID);
        vm.stopPrank();
    }

    // ============ settleIndividual Tests ============

    function test_SettleIndividual_EmitsIndividualSettlement() public {
        vm.expectEmit(true, true, false, true);
        emit IndividualSettlement(
            TEE_ID,
            user,
            INPUT_HASH,
            OUTPUT_HASH,
            SETTLEMENT_TIMESTAMP,
            WALRUS_BLOB_ID,
            SIGNATURE
        );

        vm.prank(admin);
        relay.settleIndividual(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            SETTLEMENT_TIMESTAMP,
            user,
            WALRUS_BLOB_ID,
            SIGNATURE
        );
    }

    function test_SettleIndividual_RevertIfInvalidSignature() public {
        mockSettlement.setShouldVerify(false);

        vm.startPrank(admin);
        vm.expectRevert(bytes("Invalid signature"));
        relay.settleIndividual(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            SETTLEMENT_TIMESTAMP,
            user,
            WALRUS_BLOB_ID,
            SIGNATURE
        );
        vm.stopPrank();
    }

    function test_SettleIndividual_RevertIfCallerDoesNotHaveRelayRole() public {
        vm.startPrank(user);
        vm.expectRevert(
            abi.encodeWithSelector(
                AccessControl.AccessControlUnauthorizedAccount.selector,
                user,
                relay.SETTLEMENT_RELAY_ROLE()
            )
        );
        relay.settleIndividual(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            SETTLEMENT_TIMESTAMP,
            user,
            WALRUS_BLOB_ID,
            SIGNATURE
        );
        vm.stopPrank();
    }

    function test_SettleIndividual_SucceedsForGrantedRelayRole() public {
        vm.prank(admin);
        relay.grantRole(relay.SETTLEMENT_RELAY_ROLE(), user);

        vm.prank(user);
        relay.settleIndividual(
            TEE_ID,
            INPUT_HASH,
            OUTPUT_HASH,
            SETTLEMENT_TIMESTAMP,
            user,
            WALRUS_BLOB_ID,
            SIGNATURE
        );
    }

    // ============ verifyProof Tests ============

    function test_VerifyProof_ReturnsTrueForValidProof() public view {
        bytes32 leafA = keccak256("leaf-a");
        bytes32 leafB = keccak256("leaf-b");
        bytes32 root = _commutativeKeccak256(leafA, leafB);
        bytes32[] memory proof = new bytes32[](1);
        proof[0] = leafB;

        bool valid = relay.verifyProof(proof, root, leafA);
        assertTrue(valid);
    }

    function test_VerifyProof_ReturnsFalseForInvalidLeaf() public view {
        bytes32 leafA = keccak256("leaf-a");
        bytes32 leafB = keccak256("leaf-b");
        bytes32 invalidLeaf = keccak256("leaf-c");
        bytes32 root = _commutativeKeccak256(leafA, leafB);
        bytes32[] memory proof = new bytes32[](1);
        proof[0] = leafB;

        bool valid = relay.verifyProof(proof, root, invalidLeaf);
        assertFalse(valid);
    }

    function _commutativeKeccak256(bytes32 a, bytes32 b) internal pure returns (bytes32) {
        return a < b ? keccak256(abi.encodePacked(a, b)) : keccak256(abi.encodePacked(b, a));
    }
}
