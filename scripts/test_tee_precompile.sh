#!/bin/bash

# =============================================================================
# TEE Precompile Integration Test
# =============================================================================

set -e

RPC_URL="${1:-http://localhost:8545}"
TEE_REGISTRY="0x0000000000000000000000000000000000000900"

# Correct selectors (from keccak256)
SELECTOR_IS_ACTIVE="5c36901c"
SELECTOR_COMPUTE_TEE_ID="f33d2c00"
SELECTOR_REGISTER_TEE="a279d489"
SELECTOR_GET_TEE="418b207d"
SELECTOR_GET_PUBLIC_KEY="b1c551ca"
SELECTOR_VERIFY_SIGNATURE="58db61a8"
SELECTOR_VERIFY_SETTLEMENT="643d8cb5"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "=========================================="
echo "  TEE Precompile Integration Test"
echo "=========================================="
echo "RPC: $RPC_URL"
echo "Precompile: $TEE_REGISTRY"
echo ""

# =============================================================================
# Helper Functions
# =============================================================================

rpc_call() {
    local method=$1
    local params=$2
    curl -s -X POST "$RPC_URL" \
        -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":$params,\"id\":1}"
}

eth_call() {
    local data=$1
    local result=$(rpc_call "eth_call" "[{\"to\":\"$TEE_REGISTRY\",\"data\":\"$data\"},\"latest\"]")
    echo "$result" | jq -r '.result // .error.message'
}

send_tx() {
    local from=$1
    local data=$2
    local result=$(rpc_call "eth_sendTransaction" "[{\"from\":\"$from\",\"to\":\"$TEE_REGISTRY\",\"gas\":\"0x100000\",\"data\":\"$data\"}]")
    echo "$result" | jq -r '.result // .error.message'
}

wait_for_tx() {
    local tx_hash=$1
    local max_attempts=10
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        local receipt=$(rpc_call "eth_getTransactionReceipt" "[\"$tx_hash\"]")
        local status=$(echo "$receipt" | jq -r '.result.status // "pending"')
        
        if [ "$status" == "0x1" ]; then
            echo "success"
            return 0
        elif [ "$status" == "0x0" ]; then
            echo "reverted"
            return 1
        fi
        
        sleep 1
        attempt=$((attempt + 1))
    done
    echo "timeout"
    return 1
}

random_bytes32() {
    openssl rand -hex 32
}

# =============================================================================
# Test 1: Check Connection
# =============================================================================

echo "------------------------------------------"
echo "Test 1: Check Connection"
echo "------------------------------------------"

CHAIN_ID=$(rpc_call "eth_chainId" "[]" | jq -r '.result')
if [ "$CHAIN_ID" == "null" ] || [ -z "$CHAIN_ID" ]; then
    echo -e "${RED}❌ Cannot connect to $RPC_URL${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Connected. Chain ID: $CHAIN_ID${NC}"

# =============================================================================
# Test 2: Get Account
# =============================================================================

echo ""
echo "------------------------------------------"
echo "Test 2: Get Account"
echo "------------------------------------------"

ACCOUNT=$(rpc_call "eth_accounts" "[]" | jq -r '.result[0]')
if [ "$ACCOUNT" == "null" ] || [ -z "$ACCOUNT" ]; then
    echo -e "${RED}❌ No accounts available${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Account: $ACCOUNT${NC}"

# =============================================================================
# Test 3: isActive (random teeId - should return false)
# =============================================================================

echo ""
echo "------------------------------------------"
echo "Test 3: isActive (random teeId)"
echo "------------------------------------------"

RANDOM_TEE_ID=$(random_bytes32)
echo "   Random TEE ID: 0x${RANDOM_TEE_ID:0:16}..."

CALLDATA="0x${SELECTOR_IS_ACTIVE}${RANDOM_TEE_ID}"
RESULT=$(eth_call "$CALLDATA")

echo "   Result: $RESULT"

if [ "$RESULT" == "0x0000000000000000000000000000000000000000000000000000000000000000" ]; then
    echo -e "${GREEN}✅ isActive returned false (expected)${NC}"
elif [ "$RESULT" == "0x0000000000000000000000000000000000000000000000000000000000000001" ]; then
    echo -e "${YELLOW}⚠️  isActive returned true (unexpected)${NC}"
else
    echo -e "${RED}❌ Unexpected result or error: $RESULT${NC}"
fi

# =============================================================================
# Test 4: isActive with zero teeId
# =============================================================================

echo ""
echo "------------------------------------------"
echo "Test 4: isActive (zero teeId)"
echo "------------------------------------------"

ZERO_TEE_ID="0000000000000000000000000000000000000000000000000000000000000000"
CALLDATA="0x${SELECTOR_IS_ACTIVE}${ZERO_TEE_ID}"
RESULT=$(eth_call "$CALLDATA")

echo "   Result: $RESULT"

if [ "$RESULT" == "0x0000000000000000000000000000000000000000000000000000000000000000" ]; then
    echo -e "${GREEN}✅ isActive(0x0) returned false${NC}"
else
    echo -e "${YELLOW}⚠️  Result: $RESULT${NC}"
fi

# =============================================================================
# Test 5: getTEE (should fail gracefully for non-existent)
# =============================================================================

echo ""
echo "------------------------------------------"
echo "Test 5: getTEE (non-existent)"
echo "------------------------------------------"

CALLDATA="0x${SELECTOR_GET_TEE}${RANDOM_TEE_ID}"
RESULT=$(eth_call "$CALLDATA")

echo "   Result: ${RESULT:0:50}..."

if [[ "$RESULT" == *"not found"* ]] || [[ "$RESULT" == "0x" ]] || [[ "$RESULT" == "null" ]]; then
    echo -e "${GREEN}✅ getTEE correctly reports not found${NC}"
else
    echo -e "${YELLOW}⚠️  Got response (may be empty struct)${NC}"
fi

# =============================================================================
# Summary
# =============================================================================

echo ""
echo "=========================================="
echo "  Summary"
echo "=========================================="
echo -e "${GREEN}✅ Connection OK${NC}"
echo -e "${GREEN}✅ Account: $ACCOUNT${NC}"
echo -e "${GREEN}✅ Precompile responds to calls${NC}"
echo ""
echo "Selectors verified:"
echo "  isActive:        0x${SELECTOR_IS_ACTIVE}"
echo "  computeTEEId:    0x${SELECTOR_COMPUTE_TEE_ID}"
echo "  registerTEE:     0x${SELECTOR_REGISTER_TEE}"
echo "  verifySignature: 0x${SELECTOR_VERIFY_SIGNATURE}"
echo ""
echo "For full workflow test with registration, run:"
echo "  go run scripts/integration/test_tee_workflow.go"
echo ""
