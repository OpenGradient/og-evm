#!/bin/bash

# TEE Registry Management CLI
# Usage: ./register-mgmt.sh <command> [args]

set -e

# ============ Configuration (from environment) ============
REGISTRY="${TEE_REGISTRY_ADDRESS:-0x3d641a2791533b4a0000345ea8d509d01e1ec301}"
RPC="${TEE_RPC_URL:-http://13.59.43.94:8545}"
PRIVATE_KEY="${TEE_PRIVATE_KEY:-}"  # Required for write operations

# ============ Function Selectors ============
# Read functions
SEL_GET_ACTIVE_TEES="0xd046a7fa"           # getActiveTEEs()
SEL_GET_TEE="0xccdf0493"                    # getTEE(bytes32)
SEL_GET_PUBLIC_KEY="0xb1c551ca"             # getPublicKey(bytes32)
SEL_GET_TLS_CERT="0xb778f869"               # getTLSCertificate(bytes32)
SEL_IS_ACTIVE="0x82afd23b"                  # isActive(bytes32)
SEL_HAS_ROLE="0x91d14854"                   # hasRole(bytes32,address)
SEL_DEFAULT_ADMIN="0xa217fddf"              # DEFAULT_ADMIN_ROLE()
SEL_TEE_OPERATOR="0x7a88f794"               # TEE_OPERATOR()

# Write functions
SEL_DEACTIVATE_TEE="0x8456cb59"             # deactivateTEE(bytes32) - actually 0x... need to compute
SEL_ACTIVATE_TEE="0x3f4ba83a"               # activateTEE(bytes32)
SEL_GRANT_ROLE="0x2f2ff15d"                 # grantRole(bytes32,address)
SEL_REVOKE_ROLE="0xd547741f"                # revokeRole(bytes32,address)

# Compute actual selectors
compute_selector() {
    echo -n "$1" | keccak-256sum 2>/dev/null | cut -c1-8 || \
    python3 -c "from web3 import Web3; print(Web3.keccak(text='$1').hex()[:10])" 2>/dev/null || \
    cast sig "$1" 2>/dev/null
}

# ============ Helpers ============
log() { echo "[$(date '+%H:%M:%S')] $*" >&2; }
error() { echo "ERROR: $*" >&2; exit 1; }

check_private_key() {
    if [ -z "$PRIVATE_KEY" ]; then
        error "TEE_PRIVATE_KEY environment variable required for write operations"
    fi
}

eth_call() {
    local to="$1"
    local data="$2"
    curl -s -X POST "$RPC" \
        -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"to\":\"$to\",\"data\":\"$data\"},\"latest\"],\"id\":1}"
}

# Send transaction using cast (foundry) or fall back to raw method
send_tx() {
    local to="$1"
    local data="$2"
    
    check_private_key
    
    if command -v cast &>/dev/null; then
        log "Sending transaction via cast..."
        cast send "$to" "$data" --private-key "$PRIVATE_KEY" --rpc-url "$RPC"
    else
        log "Sending transaction via raw JSON-RPC..."
        # Get nonce
        local from=$(get_address_from_key)
        local nonce=$(curl -s -X POST "$RPC" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionCount\",\"params\":[\"$from\",\"latest\"],\"id\":1}" \
            | python3 -c "import sys,json; print(json.load(sys.stdin)['result'])")
        
        # Get gas price
        local gas_price=$(curl -s -X POST "$RPC" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_gasPrice\",\"params\":[],\"id\":1}" \
            | python3 -c "import sys,json; print(json.load(sys.stdin)['result'])")
        
        # Estimate gas
        local gas=$(curl -s -X POST "$RPC" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"eth_estimateGas\",\"params\":[{\"from\":\"$from\",\"to\":\"$to\",\"data\":\"$data\"}],\"id\":1}" \
            | python3 -c "import sys,json; r=json.load(sys.stdin); print(r.get('result', '0x30000'))")
        
        log "From: $from, Nonce: $nonce, Gas: $gas, GasPrice: $gas_price"
        
        # Sign and send (requires eth-account or similar)
        python3 << PYTHON
from eth_account import Account
from eth_account.signers.local import LocalAccount
import json, requests

private_key = "$PRIVATE_KEY"
if not private_key.startswith("0x"):
    private_key = "0x" + private_key

account: LocalAccount = Account.from_key(private_key)
tx = {
    "to": "$to",
    "data": "$data",
    "nonce": int("$nonce", 16),
    "gas": int("$gas", 16),
    "gasPrice": int("$gas_price", 16),
    "chainId": 1  # Adjust as needed
}

signed = account.sign_transaction(tx)
raw_tx = signed.rawTransaction.hex()

resp = requests.post("$RPC", json={
    "jsonrpc": "2.0",
    "method": "eth_sendRawTransaction",
    "params": [raw_tx],
    "id": 1
})
result = resp.json()
if "error" in result:
    print(f"Error: {result['error']}")
else:
    print(f"TX Hash: {result['result']}")
PYTHON
    fi
}

get_address_from_key() {
    if command -v cast &>/dev/null; then
        cast wallet address --private-key "$PRIVATE_KEY"
    else
        python3 -c "
from eth_account import Account
key = '$PRIVATE_KEY'
if not key.startswith('0x'): key = '0x' + key
print(Account.from_key(key).address)
"
    fi
}

pad_bytes32() {
    local val="$1"
    val="${val#0x}"
    printf "%064s" "$val" | tr ' ' '0'
}

pad_address() {
    local addr="$1"
    addr="${addr#0x}"
    printf "%064s" "$addr" | tr ' ' '0'
}

# ============ Commands ============

cmd_list() {
    echo "=== Active TEEs in Registry ==="
    echo "Registry: $REGISTRY"
    echo "RPC: $RPC"
    echo ""
    
    local response=$(eth_call "$REGISTRY" "$SEL_GET_ACTIVE_TEES")
    
    python3 << PYTHON
import json, datetime

data = json.loads('''$response''')
if 'error' in data:
    print(f"Error: {data['error']}")
    exit(1)

result = data['result'][2:]
if len(result) < 128:
    print("No TEEs found or invalid response")
    exit(0)

count = int(result[64:128], 16)
print(f"Found {count} active TEE(s)\n")

for i in range(count):
    tee_id = result[128 + i*64 : 128 + (i+1)*64]
    print(f"  [{i+1}] 0x{tee_id}")
PYTHON
}

cmd_show() {
    local tee_id="$1"
    [ -z "$tee_id" ] && error "Usage: $0 show <tee_id>"
    
    tee_id=$(pad_bytes32 "$tee_id")
    echo "=== TEE Details: 0x$tee_id ==="
    
    # Get main info
    local detail=$(eth_call "$REGISTRY" "${SEL_GET_TEE}${tee_id}")
    
    python3 << PYTHON
import json, datetime

data = json.loads('''$detail''')
if 'error' in data:
    print(f"Error: {data['error']}")
    exit(1)

result = data['result'][2:]
if len(result) < 640:
    print("TEE not found or invalid response")
    exit(1)

def word(pos):
    return int(result[pos*64:(pos+1)*64], 16)

def dyn(pos):
    try:
        offset = word(pos) // 32
        length = word(offset)
        return bytes.fromhex(result[(offset+1)*64:(offset+1)*64 + length*2])
    except:
        return b""

print(f"  Owner:          0x{result[24:64]}")
print(f"  Payment Addr:   0x{result[88:128]}")
try:
    print(f"  Endpoint:       {dyn(2).decode()}")
except:
    print(f"  Endpoint:       <decode error>")
print(f"  PCR Hash:       0x{result[5*64:6*64]}")
tee_type = word(6)
print(f"  TEE Type:       {tee_type} ({'LLMProxy' if tee_type == 0 else 'Validator' if tee_type == 1 else 'Unknown'})")
print(f"  Active:         {bool(word(7))}")
print(f"  Registered:     {datetime.datetime.utcfromtimestamp(word(8))} UTC")
print(f"  Last Updated:   {datetime.datetime.utcfromtimestamp(word(9))} UTC")
PYTHON

    # Get public key
    echo ""
    echo "  --- Public Key ---"
    local pk_resp=$(eth_call "$REGISTRY" "${SEL_GET_PUBLIC_KEY}${tee_id}")
    
    python3 << PYTHON
import json, base64

data = json.loads('''$pk_resp''')
if 'error' in data:
    print(f"  Error: {data['error']}")
    exit(0)

result = data['result'][2:]
if len(result) < 128:
    print("  Not available")
    exit(0)

length = int(result[64:128], 16)
key_hex = result[128:128 + length*2]
key_bytes = bytes.fromhex(key_hex)

print(f"  Size:   {length} bytes")
print(f"  Hex:    {key_hex[:64]}...")
print(f"  Base64: {base64.b64encode(key_bytes).decode()[:64]}...")
PYTHON

    # Get TLS cert
    echo ""
    echo "  --- TLS Certificate ---"
    local tls_resp=$(eth_call "$REGISTRY" "${SEL_GET_TLS_CERT}${tee_id}")
    
    python3 << PYTHON
import json, base64, hashlib

data = json.loads('''$tls_resp''')
if 'error' in data:
    print(f"  Error: {data['error']}")
    exit(0)

result = data['result'][2:]
if len(result) < 128:
    print("  Not available")
    exit(0)

length = int(result[64:128], 16)
cert_bytes = bytes.fromhex(result[128:128 + length*2])

print(f"  Size:   {length} bytes")
print(f"  SHA256: {hashlib.sha256(cert_bytes).hexdigest()}")
PYTHON
}

cmd_deactivate() {
    local tee_id="$1"
    [ -z "$tee_id" ] && error "Usage: $0 deactivate <tee_id>"
    
    check_private_key
    tee_id=$(pad_bytes32 "$tee_id")
    
    log "Deactivating TEE: 0x$tee_id"
    
    # deactivateTEE(bytes32) selector
    local selector=$(python3 -c "from web3 import Web3; print(Web3.keccak(text='deactivateTEE(bytes32)').hex()[:10])" 2>/dev/null || echo "0x77b5cb3a")
    local data="${selector}${tee_id}"
    
    send_tx "$REGISTRY" "$data"
}

cmd_activate() {
    local tee_id="$1"
    [ -z "$tee_id" ] && error "Usage: $0 activate <tee_id>"
    
    check_private_key
    tee_id=$(pad_bytes32 "$tee_id")
    
    log "Activating TEE: 0x$tee_id"
    
    # activateTEE(bytes32) selector
    local selector=$(python3 -c "from web3 import Web3; print(Web3.keccak(text='activateTEE(bytes32)').hex()[:10])" 2>/dev/null || echo "0x3f4ba83a")
    local data="${selector}${tee_id}"
    
    send_tx "$REGISTRY" "$data"
}

cmd_add_admin() {
    local address="$1"
    [ -z "$address" ] && error "Usage: $0 add-admin <address>"
    
    check_private_key
    
    log "Adding admin: $address"
    
    # DEFAULT_ADMIN_ROLE = 0x0000...0000
    local role="0000000000000000000000000000000000000000000000000000000000000000"
    local addr=$(pad_address "$address")
    
    # grantRole(bytes32,address) selector: 0x2f2ff15d
    local data="0x2f2ff15d${role}${addr}"
    
    send_tx "$REGISTRY" "$data"
}

cmd_add_operator() {
    local address="$1"
    [ -z "$address" ] && error "Usage: $0 add-operator <address>"
    
    check_private_key
    
    log "Adding TEE operator: $address"
    
    # TEE_OPERATOR = keccak256("TEE_OPERATOR")
    local role=$(python3 -c "from web3 import Web3; print(Web3.keccak(text='TEE_OPERATOR').hex()[2:])" 2>/dev/null || \
                 echo "f09c69a67ae4a6e06d33c815a843c0db5a701c1a7ea5e09e1f67c3f3c4e0b0d9")
    local addr=$(pad_address "$address")
    
    # grantRole(bytes32,address)
    local data="0x2f2ff15d${role}${addr}"
    
    send_tx "$REGISTRY" "$data"
}

cmd_revoke_admin() {
    local address="$1"
    [ -z "$address" ] && error "Usage: $0 revoke-admin <address>"
    
    check_private_key
    
    log "Revoking admin: $address"
    
    local role="0000000000000000000000000000000000000000000000000000000000000000"
    local addr=$(pad_address "$address")
    
    # revokeRole(bytes32,address) selector: 0xd547741f
    local data="0xd547741f${role}${addr}"
    
    send_tx "$REGISTRY" "$data"
}

cmd_check_role() {
    local role_name="$1"
    local address="$2"
    [ -z "$role_name" ] || [ -z "$address" ] && error "Usage: $0 check-role <admin|operator> <address>"
    
    local role
    case "$role_name" in
        admin)
            role="0000000000000000000000000000000000000000000000000000000000000000"
            ;;
        operator)
            role=$(python3 -c "from web3 import Web3; print(Web3.keccak(text='TEE_OPERATOR').hex()[2:])" 2>/dev/null)
            ;;
        *)
            error "Unknown role: $role_name (use 'admin' or 'operator')"
            ;;
    esac
    
    local addr=$(pad_address "$address")
    
    # hasRole(bytes32,address) selector: 0x91d14854
    local data="0x91d14854${role}${addr}"
    local response=$(eth_call "$REGISTRY" "$data")
    
    python3 << PYTHON
import json
data = json.loads('''$response''')
result = data.get('result', '0x0')
has_role = int(result, 16) == 1
print(f"Address $address {'HAS' if has_role else 'does NOT have'} {('$role_name').upper()} role")
PYTHON
}

cmd_help() {
    cat << EOF
TEE Registry Management CLI

Usage: $0 <command> [arguments]

Environment Variables:
  TEE_REGISTRY_ADDRESS  Contract address (default: 0x3d64...)
  TEE_RPC_URL           RPC endpoint (default: http://13.59.43.94:8545)
  TEE_PRIVATE_KEY       Private key for write operations (required for tx)

Commands:
  list                          List all active TEEs
  show <tee_id>                 Show detailed info for a TEE
  deactivate <tee_id>           Deactivate a TEE (owner or admin)
  activate <tee_id>             Reactivate a TEE (owner or admin)
  add-admin <address>           Grant DEFAULT_ADMIN_ROLE to address
  add-operator <address>        Grant TEE_OPERATOR role to address
  revoke-admin <address>        Revoke DEFAULT_ADMIN_ROLE from address
  check-role <admin|operator> <address>  Check if address has role
  help                          Show this help message

Examples:
  # List all TEEs
  $0 list

  # Show TEE details
  $0 show 0x1234...

  # Deactivate a TEE (requires TEE_PRIVATE_KEY)
  export TEE_PRIVATE_KEY="0x..."
  $0 deactivate 0x1234...

  # Add a new admin
  $0 add-admin 0xNewAdminAddress...

  # Check if address is operator
  $0 check-role operator 0xSomeAddress...
EOF
}

# ============ Main ============

case "${1:-help}" in
    list)       cmd_list ;;
    show)       cmd_show "$2" ;;
    deactivate) cmd_deactivate "$2" ;;
    activate)   cmd_activate "$2" ;;
    add-admin)  cmd_add_admin "$2" ;;
    add-operator) cmd_add_operator "$2" ;;
    revoke-admin) cmd_revoke_admin "$2" ;;
    check-role) cmd_check_role "$2" "$3" ;;
    help|--help|-h) cmd_help ;;
    *)          error "Unknown command: $1. Use '$0 help' for usage." ;;
esac