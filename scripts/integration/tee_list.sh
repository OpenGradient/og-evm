#!/bin/bash

REGISTRY="0x3d641a2791533b4a0000345ea8d509d01e1ec301"
RPC="http://13.59.43.94:8545"

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

  # Get main TEE info
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
print(f'  pcrHash:       0x{result[5*64:6*64]}')
print(f'  teeType:       {word(6)} (0=LLMProxy 1=Validator)')
print(f'  active:        {bool(word(7))}')
print(f'  registeredAt:  {datetime.datetime.utcfromtimestamp(word(8))} UTC')
print(f'  lastUpdatedAt: {datetime.datetime.utcfromtimestamp(word(9))} UTC')
"

  # Get public key — getPublicKey(bytes32) selector: b1c551ca
  echo "  --- Public Key ---"
  RAW=$(curl -s -X POST $RPC \
-H "Content-Type: application/json" \
-d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"to\":\"$REGISTRY\",\"data\":\"0xb1c551ca$ID\"},\"latest\"],\"id\":1}")



echo "$RAW" | python3 -c "
import sys, json, base64
data = json.load(sys.stdin)
result = data['result'][2:]
length = int(result[64:128], 16)
key_hex = result[128:128 + length*2]
key_bytes = bytes.fromhex(key_hex)
print(f'  size:   {length} bytes')
print(f'  hex:    {key_hex[:32]}...')
print(f'  base64: {base64.b64encode(key_bytes).decode()[:64]}...')
# print as PEM
import base64 as b64
pem = '-----BEGIN PUBLIC KEY-----\n'
pem += b64.b64encode(key_bytes).decode()
pem += '\n-----END PUBLIC KEY-----'
print(f'  PEM:\n{pem}')
"

  # Get TLS certificate — getTLSCertificate(bytes32) selector: b778f869
  echo "  --- TLS Certificate ---"
  curl -s -X POST $RPC \
    -H "Content-Type: application/json" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"to\":\"$REGISTRY\",\"data\":\"0xb778f869$ID\"},\"latest\"],\"id\":1}" \
  | python3 -c "
import sys, json, base64, hashlib
data = json.load(sys.stdin)
result = data['result'][2:]
length = int(result[64:128], 16)
cert_hex = result[128:128 + length*2]
cert_bytes = bytes.fromhex(cert_hex)
print(f'  size:        {length} bytes')
print(f'  SHA256:      {hashlib.sha256(cert_bytes).hexdigest()}')
# print as PEM
pem = '-----BEGIN CERTIFICATE-----\n'
pem += base64.b64encode(cert_bytes).decode()
pem += '\n-----END CERTIFICATE-----'
print(f'  PEM:\n{pem}')
"

  echo ""
done