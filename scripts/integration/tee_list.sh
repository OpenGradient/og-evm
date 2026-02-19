#!/bin/bash

REGISTRY="0x3d641a2791533b4a0000345ea8d509d01e1ec301"
RPC="http://localhost:8545"

echo "=== Fetching Active TEEs ==="
RESPONSE=$(curl -s -X POST $RPC \
  -H "Content-Type: application/json" \
  -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"to\":\"$REGISTRY\",\"data\":\"0xd046a7fa\"},\"latest\"],\"id\":1}")

TEE_IDS=$(echo $RESPONSE | python3 -c "
import sys, json
data = json.load(sys.stdin)
result = data['result'][2:]
count = int(result[64:128], 16)
print(f'Found {count} TEE(s)', file=sys.stderr)
for i in range(count):
    print(result[128 + i*64 : 128 + (i+1)*64])
")

echo ""
echo "=== TEE Details ==="

echo "$TEE_IDS" | while read ID; do
  [ -z "$ID" ] && continue
  echo "--- TEE: 0x$ID ---"

  DETAIL=$(curl -s -X POST $RPC \
    -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"to\":\"$REGISTRY\",\"data\":\"0xccdf0493$ID\"},\"latest\"],\"id\":1}")

  echo $DETAIL | python3 -c "
import sys, json, datetime

data = json.load(sys.stdin)
if 'error' in data:
    print(f'  Error: {data[\"error\"]}')
    sys.exit(0)

result = data['result'][2:]

def word(pos):
    return int(result[pos*64:(pos+1)*64], 16)

def dyn(pos):
    offset = word(pos) // 32
    length = word(offset)
    return bytes.fromhex(result[(offset+1)*64:(offset+1)*64 + length*2])

print(f'  owner:         0x{result[24:64]}')
print(f'  paymentAddr:   0x{result[88:128]}')
print(f'  endpoint:      {dyn(2).decode()}')
print(f'  publicKey:     {len(dyn(3))} bytes')
print(f'  tlsCert:       {len(dyn(4))} bytes')
print(f'  pcrHash:       0x{result[5*64:6*64]}')
print(f'  teeType:       {word(6)} (0=LLMProxy 1=Validator)')
print(f'  active:        {bool(word(7))}')
print(f'  registeredAt:  {datetime.datetime.utcfromtimestamp(word(8))} UTC')
print(f'  lastUpdatedAt: {datetime.datetime.utcfromtimestamp(word(9))} UTC')
"
  echo ""
done