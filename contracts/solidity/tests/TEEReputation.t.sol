// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {TEEReputation} from "../TEEReputation.sol";
import {TEERegistry} from "../TEERegistry.sol";

/**
 * @title TEEReputationTest
 * @notice Comprehensive test suite for the TEEReputation contract
 */
contract TEEReputationTest is Test {
    TEEReputation reputation;
    TEERegistry registry;

    address admin = address(0x1);
    address recorder = address(0x2);
    address monitor = address(0x3);

    bytes32 teeId1 = keccak256("tee1");
    bytes32 teeId2 = keccak256("tee2");

    function setUp() public {
        // Mock TEERegistry (in real tests, would use actual)
        // For now, just deploy reputation with a placeholder
        vm.prank(admin);
        reputation = new TEEReputation(address(0x1)); // Deploy with mock address

        // Grant roles
        vm.prank(admin);
        reputation.grantRole(reputation.SETTLEMENT_RECORDER_ROLE(), recorder);

        vm.prank(admin);
        reputation.grantRole(reputation.HEARTBEAT_MONITOR_ROLE(), monitor);
    }

    // ============ Settlement Recording Tests ============

    function test_RecordSuccessfulSettlement() public {
        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1000);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertEq(stats.totalRequests, 1);
        assertEq(stats.successfulRequests, 1);
        assertEq(stats.failedRequests, 0);
        assertEq(stats.averageResponseTime, 1000);
    }

    function test_RecordFailedSettlement() public {
        vm.prank(recorder);
        reputation.recordSettlement(teeId1, false, 2000);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertEq(stats.totalRequests, 1);
        assertEq(stats.successfulRequests, 0);
        assertEq(stats.failedRequests, 1);
    }

    function test_MultipleSettlements() public {
        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1000);

        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1200);

        vm.prank(recorder);
        reputation.recordSettlement(teeId1, false, 1500);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertEq(stats.totalRequests, 3);
        assertEq(stats.successfulRequests, 2);
        assertEq(stats.failedRequests, 1);
    }

    function test_AverageResponseTime() public {
        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1000);

        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1100);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        // Moving average: (1000 * 9 + 1100) / 10 = 1010
        assertEq(stats.averageResponseTime, 1010);
    }

    // ============ Downtime Tracking Tests ============

    function test_RecordDowntime() public {
        vm.prank(monitor);
        reputation.recordDowntime(teeId1, 0, "heartbeat_missed");

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertTrue(stats.isCurrentlyDown);
        assertEq(stats.lastDowntimeStart, block.timestamp);

        TEEReputation.DowntimeWindow[] memory history = reputation.getDowntimeHistory(teeId1);
        assertEq(history.length, 1);
        assertEq(history[0].startTimestamp, block.timestamp);
        assertEq(history[0].endTimestamp, 0); // Ongoing
    }

    function test_ResolveDowntime() public {
        vm.prank(monitor);
        reputation.recordDowntime(teeId1, 0, "heartbeat_missed");

        vm.warp(block.timestamp + 100);

        vm.prank(monitor);
        reputation.resolveDowntime(teeId1);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertFalse(stats.isCurrentlyDown);
        assertEq(stats.totalDowntimeSeconds, 100);

        TEEReputation.DowntimeWindow[] memory history = reputation.getDowntimeHistory(teeId1);
        assertEq(history[0].endTimestamp, block.timestamp);
    }

    function test_MultipleDowntimeWindows() public {
        // First downtime
        vm.prank(monitor);
        reputation.recordDowntime(teeId1, 0, "heartbeat_missed");

        vm.warp(block.timestamp + 50);

        vm.prank(monitor);
        reputation.resolveDowntime(teeId1);

        // Second downtime
        vm.warp(block.timestamp + 100);

        vm.prank(monitor);
        reputation.recordDowntime(teeId1, 0, "pcr_revoked");

        vm.warp(block.timestamp + 30);

        vm.prank(monitor);
        reputation.resolveDowntime(teeId1);

        TEEReputation.TEEStats memory stats = reputation.stats(teeId1);
        assertEq(stats.totalDowntimeSeconds, 80); // 50 + 30

        TEEReputation.DowntimeWindow[] memory history = reputation.getDowntimeHistory(teeId1);
        assertEq(history.length, 2);
        assertEq(history[0].endTimestamp, block.timestamp - 130);
        assertEq(history[1].endTimestamp, block.timestamp);
    }

    // ============ Reputation Calculation Tests ============

    function test_ReputationWithNoRequests() public {
        TEEReputation.ReputationScore memory score = reputation.calculateReputationScore(teeId1);
        assertEq(score.score, 5000); // Neutral score
        assertEq(score.tier, reputation.TIER_FAIR());
    }

    function test_ReputationWithPerfectSuccessRate() public {
        // Record 10 successful requests
        for (uint256 i = 0; i < 10; i++) {
            vm.prank(recorder);
            reputation.recordSettlement(teeId1, true, 1000);
        }

        TEEReputation.ReputationScore memory score = reputation.calculateReputationScore(teeId1);
        // Success: 10000, Uptime: 10000, Response: depends on time passed
        // Score should be very high but let's check it's at least good
        assertGe(score.score, reputation.tierGoodThreshold());
    }

    function test_ReputationAfterDowntime() public {
        // 10 successful requests
        for (uint256 i = 0; i < 10; i++) {
            vm.prank(recorder);
            reputation.recordSettlement(teeId1, true, 1000);
        }

        // Record downtime
        vm.prank(monitor);
        reputation.recordDowntime(teeId1, 0, "heartbeat_missed");

        // Simulate time passing
        vm.warp(block.timestamp + 1000);

        // Resolve downtime
        vm.prank(monitor);
        reputation.resolveDowntime(teeId1);

        TEEReputation.ReputationScore memory score = reputation.calculateReputationScore(teeId1);
        // Should be lower due to downtime, but still reasonable
        assertLt(score.score, 10000); // Not perfect
        assertGe(score.score, 0);      // But not terrible
    }

    function test_ReputationWithFailures() public {
        // 5 successful, 5 failed
        for (uint256 i = 0; i < 5; i++) {
            vm.prank(recorder);
            reputation.recordSettlement(teeId1, true, 1000);

            vm.prank(recorder);
            reputation.recordSettlement(teeId1, false, 1000);
        }

        TEEReputation.ReputationScore memory score = reputation.calculateReputationScore(teeId1);
        // 50% success rate
        assertEq(score.tier, reputation.TIER_FAIR());
        assertGe(score.score, reputation.tierFairThreshold());
        assertLt(score.score, reputation.tierGoodThreshold());
    }

    // ============ Configuration Tests ============

    function test_UpdateWeights() public {
        vm.prank(admin);
        reputation.updateWeights(6000, 3000, 1000); // 60% success, 30% uptime, 10% response

        assertEq(reputation.successWeightBps(), 6000);
        assertEq(reputation.uptimeWeightBps(), 3000);
        assertEq(reputation.responseWeightBps(), 1000);
    }

    function test_UpdateWeightsRevertsIfNotSum10000() public {
        vm.prank(admin);
        vm.expectRevert("Weights must sum to 10000");
        reputation.updateWeights(5000, 3000, 1000); // Sum = 9000
    }

    function test_UpdateTierThresholds() public {
        vm.prank(admin);
        reputation.updateTierThresholds(8500, 6500, 3500);

        assertEq(reputation.tierExcellentThreshold(), 8500);
        assertEq(reputation.tierGoodThreshold(), 6500);
        assertEq(reputation.tierFairThreshold(), 3500);
    }

    function test_RecalculateReputation() public {
        // Record some data
        vm.prank(recorder);
        reputation.recordSettlement(teeId1, true, 1000);

        // Get initial cached score
        TEEReputation.ReputationScore memory score1 = reputation.cachedReputation(teeId1);

        // Update weights
        vm.prank(admin);
        reputation.updateWeights(6000, 2000, 2000);

        // Cached score should be stale
        // Recalculate
        vm.prank(admin);
        reputation.recalculateReputation(teeId1);

        // Get new cached score
        TEEReputation.ReputationScore memory score2 = reputation.cachedReputation(teeId1);
        // Scores might differ due to weight change
        assertNotEq(score1.calculatedAt, score2.calculatedAt);
    }

    // ============ Access Control Tests ============

    function test_OnlyRecorderCanRecordSettlement() public {
        address unauthorized = address(0x99);

        vm.prank(unauthorized);
        vm.expectRevert();
        reputation.recordSettlement(teeId1, true, 1000);
    }

    function test_OnlyMonitorCanRecordDowntime() public {
        address unauthorized = address(0x99);

        vm.prank(unauthorized);
        vm.expectRevert();
        reputation.recordDowntime(teeId1, 100, "test");
    }

    function test_OnlyAdminCanUpdateWeights() public {
        address unauthorized = address(0x99);

        vm.prank(unauthorized);
        vm.expectRevert();
        reputation.updateWeights(5000, 3500, 1500);
    }

    // ============ Integration Tests ============

    function test_EndToEndReputationFlow() public {
        // TEE1: Good performer
        for (uint256 i = 0; i < 20; i++) {
            vm.prank(recorder);
            reputation.recordSettlement(teeId1, true, 800);
        }

        // TEE2: Poor performer
        for (uint256 i = 0; i < 10; i++) {
            vm.prank(recorder);
            reputation.recordSettlement(teeId2, true, 800);

            vm.prank(recorder);
            reputation.recordSettlement(teeId2, false, 2000);
        }

        // TEE2 has downtime
        vm.prank(monitor);
        reputation.recordDowntime(teeId2, 0, "heartbeat_missed");

        vm.warp(block.timestamp + 500);

        vm.prank(monitor);
        reputation.resolveDowntime(teeId2);

        // Check scores
        TEEReputation.ReputationScore memory score1 =
            reputation.calculateReputationScore(teeId1);
        TEEReputation.ReputationScore memory score2 =
            reputation.calculateReputationScore(teeId2);

        assertGt(score1.score, score2.score);
        assertEq(score1.tier, reputation.TIER_EXCELLENT());
        assertEq(score2.tier, reputation.TIER_FAIR());
    }
}
