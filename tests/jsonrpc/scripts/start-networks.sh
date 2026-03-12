#!/bin/bash

# Start both ogd and geth for JSON-RPC compatibility testing

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting both ogd and geth for compatibility testing...${NC}"

# Start ogd
echo -e "${YELLOW}Starting ogd...${NC}"
"$SCRIPT_DIR/ogd/start-ogd.sh"

echo
echo -e "${YELLOW}Starting geth...${NC}"
"$SCRIPT_DIR/geth/start-geth.sh"

echo
echo -e "${GREEN}Both nodes started successfully!${NC}"
echo -e "${YELLOW}Endpoints:${NC}"
echo -e "  ogd JSON-RPC: http://localhost:8545"
echo -e "  ogd WebSocket: ws://localhost:8546"
echo -e "  geth JSON-RPC: http://localhost:8547"
echo -e "  geth WebSocket: ws://localhost:8548"
echo
echo -e "${YELLOW}To stop both: $SCRIPT_DIR/stop-both.sh${NC}"
echo -e "${YELLOW}To compare APIs: $SCRIPT_DIR/compare-apis.sh${NC}"