// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "./TEERegistry.sol";

/**
 * @title TEEReputation - Track and Score TEE Performance
 * @notice On-chain reputation system that tracks TEE performance metrics
 *         (successful/failed requests, downtime, response times) and provides
 *         reputation scores to help clients select the best TEEs.
 *
 * @dev ## Design Overview
 *
 *  The reputation contract aggregates performance data from two sources:
 *  1. **InferenceSettlementRelay** events — track successful/failed requests
 *  2. **TEERegistry** state changes — track downtime, PCR revocations, enable/disable
 *
 *  Reputation scores are calculated using a weighted formula:
 *    - Success rate: 50% weight
 *    - Uptime ratio: 35% weight
 *    - Response quality: 15% weight
 *
 *  Scores range from 0-10000 (fixed-point, divide by 100 for percentage).
 *  Clients can query TEEs by reputation tier (poor, fair, good, excellent).
 *
 * ## Integration Flow
 *
 *  1. **Off-chain indexer** listens to InferenceSettlementRelay.IndividualSettlement
 *  2. Calls recordSettlement(teeId, successful, responseTimeMs)
 *  3. Contract updates stats and recalculates reputation
 *  4. Client queries getTopTEEsByReputation(teeType, minTier, limit)
 *  5. Downtime is tracked via recordDowntime() from heartbeat monitoring
 *
 * ## Downtime Tracking
 *
 *  Downtime windows are recorded for:
 *  - Heartbeat miss: TEE fails to send liveness proof
 *  - PCR revocation: Enclave code becomes compromised
 *  - Manual disable: Owner disables TEE
 *
 *  Downtime is resolved when:
 *  - TEE sends fresh heartbeat (if was heartbeat miss)
 *  - TEE is re-enabled after owner/admin action
 */
contract TEEReputation is AccessControl {
    // ============ Constants ============

    bytes32 public constant SETTLEMENT_RECORDER_ROLE = keccak256("SETTLEMENT_RECORDER");
    bytes32 public constant HEARTBEAT_MONITOR_ROLE = keccak256("HEARTBEAT_MONITOR");

    // Reputation tiers
    uint8 public constant TIER_POOR = 0;
    uint8 public constant TIER_FAIR = 1;
    uint8 public constant TIER_GOOD = 2;
    uint8 public constant TIER_EXCELLENT = 3;

    // Max reputation value (fixed-point: 10000 = 100%)
    uint256 public constant MAX_REPUTATION = 10000;

    // ============ Structs ============

    /// @notice Per-TEE performance statistics
    struct TEEStats {
        // Request tracking
        uint64 totalRequests;        // total inference requests attributed
        uint64 successfulRequests;   // completed successfully
        uint64 failedRequests;       // failed/reverted

        // Reliability metrics
        uint32 totalDowntimeSeconds; // aggregated offline duration
        uint256 lastDowntimeStart;   // if currently down, when it started
        bool isCurrentlyDown;        // quick check flag

        // Performance
        uint256 averageResponseTime; // milliseconds (tracked off-chain, stored here)

        // Timestamps
        uint256 firstRequestAt;      // block timestamp of first request
        uint256 lastRequestAt;       // block timestamp of most recent request
        uint256 lastUpdatedAt;       // block timestamp of last stats update
    }

    /// @notice Reputation score with calculation metadata
    struct ReputationScore {
        uint256 score;         // 0-10000 (fixed-point)
        uint256 calculatedAt;  // block timestamp of calculation
        uint8 tier;            // 0=poor, 1=fair, 2=good, 3=excellent
    }

    /// @notice Downtime window record for auditing
    struct DowntimeWindow {
        uint256 startTimestamp;  // when downtime started
        uint256 endTimestamp;    // when downtime ended (0 if ongoing)
        string reason;           // "heartbeat_missed", "pcr_revoked", "manual_disable"
    }

    // ============ Storage ============

    TEERegistry public immutable REGISTRY;

    // TEE statistics: teeId => TEEStats
    mapping(bytes32 => TEEStats) public stats;

    // Downtime history: teeId => DowntimeWindow array
    mapping(bytes32 => DowntimeWindow[]) public downtimeHistory;

    // Cached reputation scores: teeId => ReputationScore
    mapping(bytes32 => ReputationScore) public cachedReputation;

    // Current tracked downtime period: teeId => downtime window index (-1 if none)
    mapping(bytes32 => int256) public currentDowntimeIndex;

    // ============ Configuration (tunable via admin) ============

    /// @notice Success rate weight in basis points (default 5000 = 50%)
    uint256 public successWeightBps = 5000;

    /// @notice Uptime ratio weight in basis points (default 3500 = 35%)
    uint256 public uptimeWeightBps = 3500;

    /// @notice Response quality weight in basis points (default 1500 = 15%)
    uint256 public responseWeightBps = 1500;

    /// @notice Reputation tier thresholds
    uint256 public tierExcellentThreshold = 9000;  // >= 9000
    uint256 public tierGoodThreshold = 7000;       // >= 7000
    uint256 public tierFairThreshold = 4000;       // >= 4000
    // Below 4000 = poor

    // ============ Events ============

    event SettlementRecorded(
        bytes32 indexed teeId,
        bool successful,
        uint256 responseTimeMs,
        uint256 indexed blockNumber
    );

    event DowntimeRecorded(
        bytes32 indexed teeId,
        uint256 durationSeconds,
        string reason,
        uint256 indexed blockNumber
    );

    event DowntimeResolved(
        bytes32 indexed teeId,
        uint256 totalDowntimeSeconds,
        uint256 indexed blockNumber
    );

    event ReputationUpdated(
        bytes32 indexed teeId,
        uint256 score,
        uint8 tier,
        uint256 indexed blockNumber
    );

    event WeightsUpdated(
        uint256 successWeightBps,
        uint256 uptimeWeightBps,
        uint256 responseWeightBps
    );

    event TierThresholdsUpdated(
        uint256 excellentThreshold,
        uint256 goodThreshold,
        uint256 fairThreshold
    );

    // ============ Constructor ============

    constructor(address _registry) {
        require(_registry != address(0), "Invalid registry address");
        REGISTRY = TEERegistry(_registry);

        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(SETTLEMENT_RECORDER_ROLE, msg.sender);
        _grantRole(HEARTBEAT_MONITOR_ROLE, msg.sender);
    }

    // ============ Settlement Recording ============

    /**
     * @notice Record a settled inference request for a TEE
     * @dev Called by off-chain indexer listening to InferenceSettlementRelay events
     * @param teeId The TEE's unique identifier
     * @param successful Whether the request completed successfully
     * @param responseTimeMs Response time in milliseconds
     */
    function recordSettlement(
        bytes32 teeId,
        bool successful,
        uint256 responseTimeMs
    ) external onlyRole(SETTLEMENT_RECORDER_ROLE) {
        require(teeId != bytes32(0), "Invalid teeId");

        TEEStats storage teeStats = stats[teeId];

        // Initialize first request timestamp
        if (teeStats.totalRequests == 0) {
            teeStats.firstRequestAt = block.timestamp;
        }

        // Update counters
        teeStats.totalRequests++;
        if (successful) {
            teeStats.successfulRequests++;
        } else {
            teeStats.failedRequests++;
        }

        // Update response time (simple moving average)
        if (teeStats.averageResponseTime == 0) {
            teeStats.averageResponseTime = responseTimeMs;
        } else {
            teeStats.averageResponseTime =
                (teeStats.averageResponseTime * 9 + responseTimeMs * 1) / 10;
        }

        teeStats.lastRequestAt = block.timestamp;
        teeStats.lastUpdatedAt = block.timestamp;

        // Recalculate reputation
        _updateReputationScore(teeId);

        emit SettlementRecorded(teeId, successful, responseTimeMs, block.number);
    }

    // ============ Downtime Tracking ============

    /**
     * @notice Record a downtime window for a TEE
     * @dev Called by heartbeat monitor service
     * @param teeId The TEE's unique identifier
     * @param durationSeconds Duration of downtime in seconds (0 = ongoing)
     * @param reason Why the TEE went down
     */
    function recordDowntime(
        bytes32 teeId,
        uint256 durationSeconds,
        string calldata reason
    ) external onlyRole(HEARTBEAT_MONITOR_ROLE) {
        require(teeId != bytes32(0), "Invalid teeId");
        require(bytes(reason).length > 0, "Reason cannot be empty");

        TEEStats storage teeStats = stats[teeId];

        // If TEE is not already down, start a new downtime window
        if (currentDowntimeIndex[teeId] == -1) {
            // Create new downtime window
            uint256 endTime = durationSeconds == 0 ? 0 : block.timestamp + durationSeconds;
            downtimeHistory[teeId].push(
                DowntimeWindow({
                    startTimestamp: block.timestamp,
                    endTimestamp: endTime,
                    reason: reason
                })
            );

            // Update index
            currentDowntimeIndex[teeId] = int256(downtimeHistory[teeId].length) - 1;
            teeStats.isCurrentlyDown = true;
            teeStats.lastDowntimeStart = block.timestamp;

            emit DowntimeRecorded(teeId, durationSeconds, reason, block.number);
        }
    }

    /**
     * @notice Resolve ongoing downtime for a TEE
     * @dev Called when TEE recovers (fresh heartbeat, re-enabled, etc.)
     * @param teeId The TEE's unique identifier
     */
    function resolveDowntime(bytes32 teeId) external onlyRole(HEARTBEAT_MONITOR_ROLE) {
        require(teeId != bytes32(0), "Invalid teeId");

        TEEStats storage teeStats = stats[teeId];
        int256 currentIdx = currentDowntimeIndex[teeId];

        if (currentIdx >= 0) {
            DowntimeWindow storage window = downtimeHistory[teeId][uint256(currentIdx)];

            // Only resolve if this window is still ongoing
            if (window.endTimestamp == 0) {
                window.endTimestamp = block.timestamp;

                // Calculate downtime duration and add to total
                uint256 durationSeconds = window.endTimestamp - window.startTimestamp;
                teeStats.totalDowntimeSeconds += uint32(durationSeconds);

                // Clear flags
                teeStats.isCurrentlyDown = false;
                teeStats.lastDowntimeStart = 0;
                currentDowntimeIndex[teeId] = -1;

                // Recalculate reputation
                _updateReputationScore(teeId);

                emit DowntimeResolved(teeId, teeStats.totalDowntimeSeconds, block.number);
            }
        }
    }

    // ============ Reputation Calculation ============

    /**
     * @notice Calculate reputation score for a TEE
     * @dev Uses weighted formula: (success * 50% + uptime * 35% + response * 15%)
     * @param teeId The TEE's unique identifier
     * @return score Reputation score (0-10000, where 10000 = 100%)
     */
    function calculateReputationScore(bytes32 teeId) public view returns (ReputationScore memory) {
        TEEStats storage teeStats = stats[teeId];

        // If no requests yet, return neutral score
        if (teeStats.totalRequests == 0) {
            return ReputationScore({score: 5000, calculatedAt: block.timestamp, tier: TIER_FAIR});
        }

        // Calculate success rate (0-10000)
        uint256 successRate = (teeStats.successfulRequests * MAX_REPUTATION) / teeStats.totalRequests;

        // Calculate uptime ratio (0-10000)
        uint256 totalTime = block.timestamp - teeStats.firstRequestAt;
        uint256 uptimeSeconds = totalTime - teeStats.totalDowntimeSeconds;
        uint256 uptimeRatio = (uptimeSeconds * MAX_REPUTATION) / (totalTime > 0 ? totalTime : 1);

        // Calculate response quality (0-10000, where lower ms = higher score)
        // Normalize: 1000ms = 10000 points, 5000ms = 5000 points, etc.
        uint256 responseQuality;
        if (teeStats.averageResponseTime == 0) {
            responseQuality = MAX_REPUTATION; // Perfect if no data
        } else {
            responseQuality =
                MAX_REPUTATION - ((teeStats.averageResponseTime * MAX_REPUTATION) / 5000);
            if (responseQuality > MAX_REPUTATION) {
                responseQuality = 0; // Cap at 0 if very slow
            }
        }

        // Weighted combination
        uint256 score =
            ((successRate * successWeightBps) + (uptimeRatio * uptimeWeightBps) +
                (responseQuality * responseWeightBps)) / 10000;

        // Ensure score is within bounds
        if (score > MAX_REPUTATION) {
            score = MAX_REPUTATION;
        }

        // Determine tier
        uint8 tier = TIER_POOR;
        if (score >= tierExcellentThreshold) {
            tier = TIER_EXCELLENT;
        } else if (score >= tierGoodThreshold) {
            tier = TIER_GOOD;
        } else if (score >= tierFairThreshold) {
            tier = TIER_FAIR;
        }

        return ReputationScore({score: score, calculatedAt: block.timestamp, tier: tier});
    }

    /**
     * @notice Internal function to update cached reputation score
     * @param teeId The TEE's unique identifier
     */
    function _updateReputationScore(bytes32 teeId) internal {
        ReputationScore memory newScore = calculateReputationScore(teeId);
        cachedReputation[teeId] = newScore;
        emit ReputationUpdated(teeId, newScore.score, newScore.tier, block.number);
    }

    // ============ Query Functions ============

    /**
     * @notice Get top TEEs by reputation for a given type
     * @param teeType TEE type to filter by
     * @param minTierRequired Minimum reputation tier required (0=poor, 1=fair, 2=good, 3=excellent)
     * @param limit Maximum number of TEEs to return
     * @return topTEEs Array of TEE IDs sorted by reputation (highest first)
     * @return scores Array of corresponding reputation scores
     */
    function getTopTEEsByReputation(
        uint8 teeType,
        uint8 minTierRequired,
        uint256 limit
    ) external view returns (bytes32[] memory topTEEs, ReputationScore[] memory scores) {
        // Get enabled TEEs of the type from registry
        bytes32[] memory enabledTEEs = REGISTRY.getEnabledTEEs(teeType);

        if (enabledTEEs.length == 0) {
            return (new bytes32[](0), new ReputationScore[](0));
        }

        // Calculate scores for all, filter by tier, and sort
        ReputationScore[] memory allScores = new ReputationScore[](enabledTEEs.length);
        uint256 validCount = 0;

        for (uint256 i = 0; i < enabledTEEs.length; i++) {
            ReputationScore memory score = calculateReputationScore(enabledTEEs[i]);
            if (score.tier >= minTierRequired) {
                allScores[i] = score;
                validCount++;
            }
        }

        if (validCount == 0) {
            return (new bytes32[](0), new ReputationScore[](0));
        }

        // Limit the result set
        uint256 returnCount = validCount < limit ? validCount : limit;
        bytes32[] memory result = new bytes32[](returnCount);
        ReputationScore[] memory resultScores = new ReputationScore[](returnCount);

        // Simple selection sort to get top N (not optimal, but clear)
        bytes32[] memory sortedTEEs = enabledTEEs;
        uint256 resultIdx = 0;

        for (uint256 i = 0; i < returnCount && resultIdx < returnCount; i++) {
            uint256 maxScore = 0;
            uint256 maxIdx = type(uint256).max;

            // Find next highest score
            for (uint256 j = 0; j < sortedTEEs.length; j++) {
                if (sortedTEEs[j] != bytes32(0)) {
                    ReputationScore memory score = calculateReputationScore(sortedTEEs[j]);
                    if (score.tier >= minTierRequired && score.score > maxScore) {
                        maxScore = score.score;
                        maxIdx = j;
                    }
                }
            }

            if (maxIdx < type(uint256).max) {
                result[resultIdx] = sortedTEEs[maxIdx];
                resultScores[resultIdx] = calculateReputationScore(sortedTEEs[maxIdx]);
                sortedTEEs[maxIdx] = bytes32(0); // Mark as used
                resultIdx++;
            }
        }

        return (result, resultScores);
    }

    /**
     * @notice Get statistics for a TEE
     * @param teeId The TEE's unique identifier
     * @return stats The TEE's statistics
     */
    function getTEEStats(bytes32 teeId) external view returns (TEEStats memory) {
        return stats[teeId];
    }

    /**
     * @notice Get downtime history for a TEE
     * @param teeId The TEE's unique identifier
     * @return Array of downtime windows
     */
    function getDowntimeHistory(bytes32 teeId)
        external
        view
        returns (DowntimeWindow[] memory)
    {
        return downtimeHistory[teeId];
    }

    /**
     * @notice Get cached reputation score for a TEE
     * @param teeId The TEE's unique identifier
     * @return The cached reputation score
     */
    function getReputationScore(bytes32 teeId) external view returns (ReputationScore memory) {
        return cachedReputation[teeId];
    }

    // ============ Admin Functions ============

    /**
     * @notice Update scoring weights
     * @dev Weights must sum to 10000 (100%)
     * @param newSuccessWeightBps New weight for success rate
     * @param newUptimeWeightBps New weight for uptime
     * @param newResponseWeightBps New weight for response quality
     */
    function updateWeights(
        uint256 newSuccessWeightBps,
        uint256 newUptimeWeightBps,
        uint256 newResponseWeightBps
    ) external onlyRole(DEFAULT_ADMIN_ROLE) {
        require(
            newSuccessWeightBps + newUptimeWeightBps + newResponseWeightBps == 10000,
            "Weights must sum to 10000"
        );

        successWeightBps = newSuccessWeightBps;
        uptimeWeightBps = newUptimeWeightBps;
        responseWeightBps = newResponseWeightBps;

        emit WeightsUpdated(newSuccessWeightBps, newUptimeWeightBps, newResponseWeightBps);
    }

    /**
     * @notice Update reputation tier thresholds
     * @param newExcellentThreshold Score threshold for "excellent" tier
     * @param newGoodThreshold Score threshold for "good" tier
     * @param newFairThreshold Score threshold for "fair" tier
     */
    function updateTierThresholds(
        uint256 newExcellentThreshold,
        uint256 newGoodThreshold,
        uint256 newFairThreshold
    ) external onlyRole(DEFAULT_ADMIN_ROLE) {
        require(
            newExcellentThreshold >= newGoodThreshold &&
                newGoodThreshold >= newFairThreshold &&
                newFairThreshold > 0 &&
                newExcellentThreshold <= MAX_REPUTATION,
            "Invalid thresholds"
        );

        tierExcellentThreshold = newExcellentThreshold;
        tierGoodThreshold = newGoodThreshold;
        tierFairThreshold = newFairThreshold;

        emit TierThresholdsUpdated(newExcellentThreshold, newGoodThreshold, newFairThreshold);
    }

    /**
     * @notice Manually recalculate reputation for a TEE
     * @dev Useful if weights or thresholds have changed
     * @param teeId The TEE's unique identifier
     */
    function recalculateReputation(bytes32 teeId)
        external
        onlyRole(DEFAULT_ADMIN_ROLE)
    {
        _updateReputationScore(teeId);
    }
}
