# TEE Reputation System - Implementation Guide

## Overview

The **TEEReputation** contract implements a reputation tracking system for Trusted Execution Environment (TEE) nodes in OpenGradient. It monitors TEE performance metrics and provides reputation scores to help clients select the best TEEs for inference requests.

**Status**: ✅ Implements issue #49 - "Add reputation contract for TEEs"

## Architecture

```
InferenceSettlementRelay (existing)
    ↓ emits IndividualSettlement events
    ↓ (off-chain indexer listens)
TEEReputation.recordSettlement()
    ↓
Stats Updated → Reputation Recalculated
    ↓
Client queries getTopTEEsByReputation()

TEERegistry (existing)
    ↓ monitors heartbeat/enable/disable
    ↓ (off-chain service listens)
TEEReputation.recordDowntime() / resolveDowntime()
    ↓
Downtime tracked → Reputation affected
```

## Key Features

### 1. **Performance Tracking**
- Total requests per TEE
- Successful vs. failed requests
- Average response time (moving average)
- Success rate calculation

### 2. **Downtime Monitoring**
- Records downtime windows with reasons:
  - `heartbeat_missed` - TEE failed to send periodic liveness proof
  - `pcr_revoked` - Enclave code became compromised
  - `manual_disable` - Owner/admin disabled TEE
- Tracks total downtime per TEE
- Audit trail of all downtime periods

### 3. **Reputation Scoring**
```
Reputation = (
    SuccessRate (50%) + 
    UptimeRatio (35%) + 
    ResponseQuality (15%)
) × 100%

Score: 0-10000 (10000 = 100%)
```

- **Tiers**: Poor (0-3999), Fair (4000-6999), Good (7000-8999), Excellent (9000+)
- **Configurable weights** via admin panel
- **Cached scores** for gas efficiency

### 4. **Client Query Interface**
```solidity
// Get top 10 "excellent" LLM inference TEEs
(bytes32[] memory tees, ReputationScore[] memory scores) = 
    reputation.getTopTEEsByReputation(
        1,              // teeType: LLM
        3,              // minTier: EXCELLENT  
        10              // limit
    );
```

## Integration Steps

### Step 1: Deploy the Contract

```bash
# In og-evm/contracts/solidity/
npx hardhat compile

# Deploy
npx hardhat run scripts/deploy-reputation.js --network <network>
```

**Constructor Requirements:**
```solidity
TEEReputation reputation = new TEEReputation(
    address(teeRegistry)  // existing TEERegistry contract address
);
```

### Step 2: Grant Roles

```solidity
// Admin account does:

// Grant settlement recording role to off-chain indexer service
reputation.grantRole(
    reputation.SETTLEMENT_RECORDER_ROLE(),
    INDEXER_SERVICE_ADDRESS
);

// Grant downtime monitoring role to heartbeat monitor service
reputation.grantRole(
    reputation.HEARTBEAT_MONITOR_ROLE(),
    HEARTBEAT_MONITOR_ADDRESS
);
```

### Step 3: Set Up Off-Chain Indexer

Create a service that listens to `InferenceSettlementRelay.IndividualSettlement` events:

```javascript
// Pseudo-code: off-chain indexer
const contract = new ethers.Contract(
    INFERENCE_SETTLEMENT_RELAY_ADDRESS,
    INFERENCE_SETTLEMENT_RELAY_ABI,
    provider
);

contract.on('IndividualSettlement', async (teeId, ethAddress, inputHash, 
    outputHash, timestamp, walrusBlobId, signature) => {
    
    // Query Walrus/IPFS to check if inference was successful
    const result = await queryResult(walrusBlobId);
    const successful = result.status === 'success';
    const responseTimeMs = result.responseTime || 1000;
    
    // Record on-chain
    const tx = await reputationContract.recordSettlement(
        teeId,
        successful,
        responseTimeMs,
        { gasPrice: ethers.utils.parseUnits('1', 'gwei') }
    );
    
    console.log(`Recorded settlement for TEE ${teeId}: ${tx.hash}`);
});
```

### Step 4: Set Up Downtime Monitor

Create a service that monitors heartbeat freshness and TEE status:

```javascript
// Pseudo-code: heartbeat monitor
async function monitorHeartbeats() {
    const teeRegistry = new ethers.Contract(...);
    const reputation = new ethers.Contract(...);
    
    setInterval(async () => {
        const allTEEs = await teeRegistry.getAllTEEs();
        const maxHeartbeatAge = 1800; // 30 minutes (from TEERegistry)
        
        for (const tee of allTEEs) {
            const teeData = await teeRegistry.getTEE(tee.id);
            const secondsSinceHeartbeat = Math.floor(Date.now() / 1000) - 
                teeData.lastHeartbeatAt.toNumber();
            
            if (secondsSinceHeartbeat > maxHeartbeatAge) {
                // TEE is down due to missed heartbeat
                const stats = await reputation.stats(tee.id);
                
                if (!stats.isCurrentlyDown) {
                    // Start downtime tracking
                    await reputation.recordDowntime(
                        tee.id,
                        0,  // duration 0 = ongoing
                        "heartbeat_missed"
                    );
                }
            } else if (secondsSinceHeartbeat <= maxHeartbeatAge) {
                // TEE recovered
                const stats = await reputation.stats(tee.id);
                
                if (stats.isCurrentlyDown) {
                    // Resolve downtime
                    await reputation.resolveDowntime(tee.id);
                }
            }
        }
    }, 60000); // Check every 60 seconds
}
```

## Usage Examples

### For Clients: Find Best TEEs

```solidity
// Get top 5 "good" or better LLM TEEs
(bytes32[] memory topTEEs, ) = reputation.getTopTEEsByReputation(
    1,      // LLM type
    2,      // GOOD tier minimum
    5       // Return top 5
);

for (uint256 i = 0; i < topTEEs.length; i++) {
    bytes32 teeId = topTEEs[i];
    // Connect to TEE endpoint and verify TLS certificate
    makeInferenceRequest(teeId);
}
```

### For Monitoring: Check TEE Status

```solidity
// Get reputation score
TEEReputation.ReputationScore memory score = 
    reputation.cachedReputation(teeId);

console.log("Score:", score.score, "/10000");
console.log("Tier:", score.tier); // 0=poor, 1=fair, 2=good, 3=excellent

// Get detailed stats
TEEReputation.TEEStats memory stats = reputation.stats(teeId);

console.log("Success rate:", 
    (stats.successfulRequests * 100) / stats.totalRequests, "%");
console.log("Total downtime:", stats.totalDowntimeSeconds, "seconds");
console.log("Currently down:", stats.isCurrentlyDown);
```

### For Administration: Configure Weights

```solidity
// Adjust scoring formula if needed 
// Example: emphasize availability over response time

reputation.updateWeights(
    5000,   // success rate: 50% (unchanged)
    4500,   // uptime ratio: 45% (increased from 35%)
    500     // response quality: 5% (decreased from 15%)
);

// Update tier thresholds if needed
reputation.updateTierThresholds(
    9500,   // excellent: >= 95%
    8000,   // good: >= 80%
    5000    // fair: >= 50%
);
```

## Data Structures

### TEEStats
```solidity
struct TEEStats {
    uint64 totalRequests;        // Total inference requests attributed
    uint64 successfulRequests;   // Completed successfully
    uint64 failedRequests;       // Failed/reverted
    
    uint32 totalDowntimeSeconds; // Total offline duration
    uint256 lastDowntimeStart;   // Current downtime start (if down)
    bool isCurrentlyDown;        // Quick status check
    
    uint256 averageResponseTime; // Milliseconds (moving average)
    
    uint256 firstRequestAt;      // Timestamp of first request
    uint256 lastRequestAt;       // Timestamp of most recent request
    uint256 lastUpdatedAt;       // Timestamp of last stats update
}
```

### ReputationScore
```solidity
struct ReputationScore {
    uint256 score;         // 0-10000 (fixed-point)
    uint256 calculatedAt;  // Block timestamp
    uint8 tier;            // 0=poor, 1=fair, 2=good, 3=excellent
}
```

### DowntimeWindow (Audit Trail)
```solidity
struct DowntimeWindow {
    uint256 startTimestamp;   // When downtime started
    uint256 endTimestamp;     // When downtime ended (0 if ongoing)
    string reason;            // "heartbeat_missed" | "pcr_revoked" | "manual_disable"
}
```

## Role-Based Access Control

| Role | Permissions | Typical User |
|------|-------------|--------------|
| `DEFAULT_ADMIN_ROLE` | Update weights, thresholds, recalculate reputation | Admin/DAO |
| `SETTLEMENT_RECORDER_ROLE` | Call `recordSettlement()` | Off-chain indexer service |
| `HEARTBEAT_MONITOR_ROLE` | Call `recordDowntime()` / `resolveDowntime()` | Heartbeat monitor service |

## Gas Optimization Considerations

1. **Cached Reputation**: Scores are cached to avoid recalculation on every query
   - Recalculated when: settlements recorded, downtime resolved, weights changed
   - Query cached score via `cachedReputation[teeId]`

2. **Batch Operations**: For off-chain indexers recording many settlements:
   ```javascript
   // More gas-efficient than individual calls
   const txs = await Promise.all(
       settlements.map(s => 
           reputation.recordSettlement(s.teeId, s.success, s.responseTime)
       )
   );
   ```

3. **Downtime Efficiency**: Downtime windows stored in array (unbounded) for auditability
   - Consider pagination on front-end if TEE has very long history

## Testing

Run the comprehensive test suite:

```bash
cd contracts/solidity
npx hardhat test tests/TEEReputation.t.sol

# For Foundry tests (if using Foundry):
forge test --match-path contracts/solidity/tests/TEEReputation.t.sol
```

Test coverage includes:
- ✅ Settlement recording (success/fail, multiple, response time)
- ✅ Downtime tracking (record, resolve, multiple windows)
- ✅ Reputation calculation (various scenarios)
- ✅ Configuration (weights, thresholds)
- ✅ Access control (role-based restrictions)
- ✅ Integration (end-to-end flow)

## Future Enhancements

1. **Geographic Distribution**: Track TEE location for latency-aware selection
2. **Specialized Metrics**: 
   - Per-model-type success rates
   - Per-client success rates (for privacy-respecting recommendations)
3. **Reputation Decay**: Reduce weight of old data over time
4. **Slashing**: If TEE fails enough times, automatically blacklist
5. **Incentive Mechanism**: Reward high-reputation TEEs with priority task assignment

## Deployment Checklist

- [ ] Deploy TEEReputation contract (pass TEERegistry address)
- [ ] Grant SETTLEMENT_RECORDER_ROLE to indexer service
- [ ] Grant HEARTBEAT_MONITOR_ROLE to heartbeat monitor
- [ ] Start off-chain indexer listening to InferenceSettlementRelay events
- [ ] Start heartbeat monitor service
- [ ] Configure initial weights/thresholds if needed
- [ ] Verify first settlements are being recorded
- [ ] Verify reputation scores are calculating correctly
- [ ] Update client SDKs to use `getTopTEEsByReputation()` for selection
- [ ] Monitor through test period before production

## Support

For questions or issues:
- See TEERegistry documentation for chain of trust details
- See InferenceSettlementRelay for settlement event structure
- Check test suite for usage examples
- Open issue in GitHub repository

---

**File**: `/contracts/solidity/TEEReputation.sol`  
**Tests**: `/contracts/solidity/tests/TEEReputation.t.sol`  
**Related**: TEERegistry, InferenceSettlementRelay
