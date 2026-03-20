#!/bin/bash

CHAINID="${CHAIN_ID:-10740}"
MONIKER="ogevmdevnettest"
KEYRING="test"
KEYALGO="eth_secp256k1"
LOGLEVEL="info"
BASEFEE=10000000
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-3}"

NODE_NUMBER="${NODE_NUMBER:-}"
START_VALIDATOR="${START_VALIDATOR:-false}"
GENERATE_GENESIS="${GENERATE_GENESIS:-false}"

get_p2p_port() { echo $((26656 + ($1 * 100))); }
get_rpc_port() { echo $((26657 + ($1 * 100))); }
get_grpc_port() { echo $((9090 + ($1 * 10))); }
get_jsonrpc_port() { echo $((8545 + ($1 * 10))); }

get_val_mnemonic() {
  local idx="$1"
  local var_name="VAL${idx}_MNEMONIC"
  echo "${!var_name:-}"
}

get_home_dir() { echo "$BASEDIR/val$1"; }

command -v jq >/dev/null 2>&1 || { echo >&2 "jq not installed."; exit 1; }

set -e

usage() {
  echo "Usage: $0 [options]"
  echo ""
  echo "Environment Variables:"
  echo "  GENERATE_GENESIS=true    Generate genesis for all validators"
  echo "  START_VALIDATOR=true     Start a validator"
  echo "  NODE_NUMBER=0..N-1       Which validator to start"
  echo "  VALIDATOR_COUNT=3        Validator count for genesis/startup"
  echo "  BASEDIR=path             Base directory (default: ~/.og-evm-devnet)"
  echo ""
  echo "Options:"
  echo "  -y                       Overwrite existing chain data"
  echo "  -h, --help               Show this help"
}

overwrite=""
while [[ $# -gt 0 ]]; do
  case $1 in
    -y) overwrite="y"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown flag: $1"; usage; exit 1 ;;
  esac
done

apply_genesis_customizations() {
  local GENESIS="$1"
  local TMP_GENESIS="${GENESIS}.tmp"

  jq '.app_state["staking"]["params"]["bond_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["expedited_min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["evm"]["params"]["evm_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["mint"]["params"]["mint_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["bank"]["denom_metadata"]=[{"description":"The native staking token for evmd.","denom_units":[{"denom":"ogwei","exponent":0,"aliases":[]},{"denom":"OGETH","exponent":18,"aliases":[]}],"base":"ogwei","display":"OGETH","name":"ETH Token","symbol":"OGETH","uri":"","uri_hash":""}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["evm"]["params"]["active_static_precompiles"]=["0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805","0x0000000000000000000000000000000000000806","0x0000000000000000000000000000000000000807"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state.erc20.native_precompiles=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state.erc20.token_pairs=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"ogwei",enabled:true}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.consensus.params.block.max_gas="10000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  sed -i.bak 's/"max_deposit_period": "172800s"/"max_deposit_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"voting_period": "172800s"/"voting_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"expedited_voting_period": "86400s"/"expedited_voting_period": "15s"/g' "$GENESIS"
  rm -f "${GENESIS}.bak"
}

apply_config_customizations() {
  local HOME_DIR="$1"
  local NODE_NUM="$2"
  local CONFIG_TOML="$HOME_DIR/config/config.toml"
  local APP_TOML="$HOME_DIR/config/app.toml"

  local P2P_PORT=$(get_p2p_port $NODE_NUM)
  local RPC_PORT=$(get_rpc_port $NODE_NUM)
  local GRPC_PORT=$(get_grpc_port $NODE_NUM)
  local JSONRPC_PORT=$(get_jsonrpc_port $NODE_NUM)
  local PROM_PORT=$((26660 + NODE_NUM))
  local PPROF_PORT=$((6060 + NODE_NUM))
  local WS_PORT=$((8546 + NODE_NUM))
  local GETH_METRICS_PORT=$((8100 + NODE_NUM))
  local EVM_METRICS_PORT=$((6065 + NODE_NUM))

  sed -i.bak 's/timeout_propose = "3s"/timeout_propose = "2s"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_prevote = "1s"/timeout_prevote = "500ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_precommit = "1s"/timeout_precommit = "500ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "5s"/g' "$CONFIG_TOML"

  sed -i.bak "s|laddr = \"tcp://127.0.0.1:26657\"|laddr = \"tcp://0.0.0.0:${RPC_PORT}\"|g" "$CONFIG_TOML"
  sed -i.bak "s|laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://0.0.0.0:${P2P_PORT}\"|g" "$CONFIG_TOML"

  sed -i.bak 's/prometheus = false/prometheus = true/' "$CONFIG_TOML"
  sed -i.bak 's/addr_book_strict = true/addr_book_strict = false/' "$CONFIG_TOML"
  sed -i.bak 's/allow_duplicate_ip = false/allow_duplicate_ip = true/' "$CONFIG_TOML"
  sed -i.bak "s|prometheus_listen_addr = \":26660\"|prometheus_listen_addr = \":${PROM_PORT}\"|g" "$CONFIG_TOML"
  sed -i.bak "s|pprof_laddr = \"localhost:6060\"|pprof_laddr = \"localhost:${PPROF_PORT}\"|g" "$CONFIG_TOML"
  sed -i.bak 's/prometheus-retention-time  = "0"/prometheus-retention-time  = "1000000000000"/g' "$APP_TOML"
  sed -i.bak 's/enabled = false/enabled = true/g' "$APP_TOML"
  sed -i.bak 's/enable = false/enable = true/g' "$APP_TOML"
  sed -i.bak 's/enable-indexer = false/enable-indexer = true/g' "$APP_TOML"

  sed -i.bak "s|address = \"0.0.0.0:9090\"|address = \"0.0.0.0:${GRPC_PORT}\"|g" "$APP_TOML"
  sed -i.bak "s|address = \"localhost:9090\"|address = \"0.0.0.0:${GRPC_PORT}\"|g" "$APP_TOML"

  sed -i.bak "s|address = \"127.0.0.1:8545\"|address = \"0.0.0.0:${JSONRPC_PORT}\"|g" "$APP_TOML"
  sed -i.bak "s|address = \"0.0.0.0:8545\"|address = \"0.0.0.0:${JSONRPC_PORT}\"|g" "$APP_TOML"

  sed -i.bak "s|geth-metrics-address = \"127.0.0.1:8100\"|geth-metrics-address = \"127.0.0.1:${GETH_METRICS_PORT}\"|g" "$APP_TOML"
  sed -i.bak "s|ws-address = \"127.0.0.1:8546\"|ws-address = \"127.0.0.1:${WS_PORT}\"|g" "$APP_TOML"
  sed -i.bak "s|metrics-address = \"127.0.0.1:6065\"|metrics-address = \"127.0.0.1:${EVM_METRICS_PORT}\"|g" "$APP_TOML"

  rm -f "$CONFIG_TOML.bak" "$APP_TOML.bak"
}

set_persistent_peers() {
  local HOME_DIR="$1"
  local NODE_NUM="$2"
  shift 2
  local NODE_IDS=("$@")
  local CONFIG_TOML="$HOME_DIR/config/config.toml"

  local PEERS=""
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    if [[ $i -ne $NODE_NUM ]]; then
      local PEER_PORT=$(get_p2p_port $i)
      if [[ -n "$PEERS" ]]; then
        PEERS="${PEERS},"
      fi
      PEERS="${PEERS}${NODE_IDS[$i]}@127.0.0.1:${PEER_PORT}"
    fi
  done

  sed -i.bak "s|persistent_peers = \"\"|persistent_peers = \"${PEERS}\"|g" "$CONFIG_TOML"
  rm -f "$CONFIG_TOML.bak"
  echo "Set persistent_peers for val$NODE_NUM: $PEERS"
}

generate_dev_accounts() {
  local NUM_ACCOUNTS="$1"
  local GENESIS_HOME="$2"
  local OUTPUT_FILE="$BASEDIR/dev_accounts.txt"

  echo ""
  echo ">>> Generating $NUM_ACCOUNTS dev accounts with funds..."

  mkdir -p "$BASEDIR"
  echo "# Dev Accounts - Generated $(date)" > "$OUTPUT_FILE"
  echo "# Each account funded with 1000000000000000000000000ogwei (1M OGETH)" >> "$OUTPUT_FILE"
  echo "#" >> "$OUTPUT_FILE"

  local DEV_HOME="$BASEDIR/.dev_keys_tmp"

  for i in $(seq 0 $((NUM_ACCOUNTS - 1))); do
    local KEYNAME="dev${i}"

    rm -rf "$DEV_HOME"
    mkdir -p "$DEV_HOME"

    local FULL_OUTPUT=$(evmd keys add "$KEYNAME" --keyring-backend test --algo "$KEYALGO" --home "$DEV_HOME" 2>&1)
    local MNEMONIC=$(echo "$FULL_OUTPUT" | tail -1)

    local ADDRESS=$(evmd keys show "$KEYNAME" -a --keyring-backend test --home "$DEV_HOME")
    local PRIVKEY=$(evmd keys unsafe-export-eth-key "$KEYNAME" --keyring-backend test --home "$DEV_HOME" 2>&1)

    evmd genesis add-genesis-account "$ADDRESS" 1000000000000000000000000ogwei --home "$GENESIS_HOME"

    echo "" >> "$OUTPUT_FILE"
    echo "dev${i}:" >> "$OUTPUT_FILE"
    echo "  address: $ADDRESS" >> "$OUTPUT_FILE"
    echo "  private_key: 0x$PRIVKEY" >> "$OUTPUT_FILE"
    echo "  mnemonic: $MNEMONIC" >> "$OUTPUT_FILE"

    echo "Created dev${i}: $ADDRESS"
  done

  rm -rf "$DEV_HOME"
  echo ""
  echo "Dev accounts saved to: $OUTPUT_FILE"
}

generate_genesis() {
  echo "=========================================="
  echo "Generating genesis for $VALIDATOR_COUNT validators..."
  echo "Base directory: $BASEDIR"
  echo "=========================================="

  if [[ -z "$overwrite" && -d "$BASEDIR" ]]; then
    echo "Existing data found at $BASEDIR. Overwrite? [y/n]"
    read -r overwrite
  fi
  [[ -z "$overwrite" ]] && overwrite="y"

  if [[ "$overwrite" != "y" && "$overwrite" != "Y" ]]; then
    echo "Aborting."
    exit 0
  fi

  rm -rf "$BASEDIR"
  mkdir -p "$BASEDIR"

  declare -a NODE_IDS

  echo ""
  echo ">>> Step 1: Initializing all validators..."
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    HOME_DIR=$(get_home_dir $i)
    MNEMONIC=$(get_val_mnemonic $i)
    VALKEY="val${i}"
    if [[ -z "$MNEMONIC" ]]; then
      echo "Error: VAL${i}_MNEMONIC is required for validator $i"
      exit 1
    fi


    echo "--- Initializing validator $i at $HOME_DIR ---"

    evmd config set client chain-id "$CHAINID" --home "$HOME_DIR"
    evmd config set client keyring-backend "$KEYRING" --home "$HOME_DIR"

    echo "$MNEMONIC" | evmd keys add "$VALKEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOME_DIR"

    echo "$MNEMONIC" | evmd init "${MONIKER}-val${i}" -o --chain-id "$CHAINID" --home "$HOME_DIR" --recover

    NODE_ID=$(evmd comet show-node-id --home "$HOME_DIR")
    NODE_IDS+=("$NODE_ID")
    echo "Validator $i Node ID: $NODE_ID"
  done

  echo ""
  echo ">>> Step 2: Customizing genesis..."
  GENESIS="$(get_home_dir 0)/config/genesis.json"
  apply_genesis_customizations "$GENESIS"

  echo ""
  echo ">>> Step 3: Adding all validator accounts with initial balances..."
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    VALKEY="val${i}"
    VAL_HOME=$(get_home_dir $i)

    VAL_ADDR=$(evmd keys show "$VALKEY" -a --keyring-backend "$KEYRING" --home "$VAL_HOME")

    echo "Adding $VALKEY ($VAL_ADDR) with 100000000000000000000000000ogwei"
    evmd genesis add-genesis-account "$VAL_ADDR" 100000000000000000000000000ogwei --home "$(get_home_dir 0)"
  done

  echo ""
  echo ">>> Step 4: Generating dev accounts..."
  generate_dev_accounts 10 "$(get_home_dir 0)"

  echo ""
  echo ">>> Step 5: Copying genesis to all validators..."
  for i in $(seq 1 $((VALIDATOR_COUNT - 1))); do
    cp "$GENESIS" "$(get_home_dir $i)/config/genesis.json"
    echo "Copied genesis to val$i"
  done

  echo ""
  echo ">>> Step 6: Creating gentx for each validator..."
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    HOME_DIR=$(get_home_dir $i)
    VALKEY="val${i}"
    P2P_PORT=$(get_p2p_port $i)

    echo "Creating gentx for $VALKEY (P2P port: $P2P_PORT)..."
    evmd genesis gentx "$VALKEY" 10000000000000000000000ogwei \
      --gas-prices ${BASEFEE}ogwei \
      --keyring-backend "$KEYRING" \
      --chain-id "$CHAINID" \
      --home "$HOME_DIR" \
      --ip "127.0.0.1" \
      --p2p-port "$P2P_PORT"
  done

  echo ""
  echo ">>> Step 7: Collecting all gentxs..."
  GENTX_DIR="$(get_home_dir 0)/config/gentx"
  for i in $(seq 1 $((VALIDATOR_COUNT - 1))); do
    cp "$(get_home_dir $i)/config/gentx/"*.json "$GENTX_DIR/"
    echo "Copied gentx from val$i"
  done

  evmd genesis collect-gentxs --home "$(get_home_dir 0)"
  evmd genesis validate-genesis --home "$(get_home_dir 0)"
  echo "Genesis validated successfully!"

  echo ""
  echo ">>> Step 8: Distributing final genesis to all validators..."
  FINAL_GENESIS="$(get_home_dir 0)/config/genesis.json"
  for i in $(seq 1 $((VALIDATOR_COUNT - 1))); do
    cp "$FINAL_GENESIS" "$(get_home_dir $i)/config/genesis.json"
    echo "Copied final genesis to val$i"
  done

  echo ""
  echo ">>> Step 9: Applying config customizations and setting peers..."
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    HOME_DIR=$(get_home_dir $i)
    apply_config_customizations "$HOME_DIR" "$i"
    set_persistent_peers "$HOME_DIR" "$i" "${NODE_IDS[@]}"
  done

  : > "$BASEDIR/node_ids.txt"
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    echo "NODE${i}_ID=${NODE_IDS[$i]}" >> "$BASEDIR/node_ids.txt"
  done

  echo ""
  echo "=========================================="
  echo "Genesis generation complete!"
  echo "=========================================="
  echo ""
  echo "Directory structure:"
  echo "  $BASEDIR/"
  echo "  ├── val0/            (Validator 0 home)"
  echo "  ├── val1/            (Validator 1 home)"
  if (( VALIDATOR_COUNT >= 3 )); then
    echo "  ├── val2/            (Validator 2 home)"
  fi
  if (( VALIDATOR_COUNT > 3 )); then
    echo "  ├── ...              (Validator 3..$((VALIDATOR_COUNT - 1)) home)"
  fi
  echo "  ├── dev_accounts.txt (10 funded dev accounts)"
  echo "  └── node_ids.txt"
  echo ""
  echo "Port mapping:"
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    echo "  val${i}: P2P=$(get_p2p_port "$i"), RPC=$(get_rpc_port "$i"), gRPC=$(get_grpc_port "$i"), JSON-RPC=$(get_jsonrpc_port "$i")"
  done
  echo ""
  echo "Validators funded: 100000000000000000000000000ogwei each"
  echo "Dev accounts funded: 1000000000000000000000000ogwei each"
  echo "Dev keys saved to: $BASEDIR/dev_accounts.txt"
  echo "=========================================="
}

start_validator() {
  if [[ -z "$NODE_NUMBER" ]]; then
    echo "Error: NODE_NUMBER env variable required (0..$((VALIDATOR_COUNT - 1)))"
    exit 1
  fi

  if [[ ! "$NODE_NUMBER" =~ ^[0-9]+$ ]] || (( NODE_NUMBER < 0 || NODE_NUMBER >= VALIDATOR_COUNT )); then
    echo "Error: NODE_NUMBER must be between 0 and $((VALIDATOR_COUNT - 1))"
    exit 1
  fi

  HOME_DIR=$(get_home_dir $NODE_NUMBER)
  JSONRPC_PORT=$(get_jsonrpc_port $NODE_NUMBER)
  P2P_PORT=$(get_p2p_port $NODE_NUMBER)
  RPC_PORT=$(get_rpc_port $NODE_NUMBER)

  if [[ ! -d "$HOME_DIR" ]]; then
    echo "Error: Validator directory not found: $HOME_DIR"
    echo "Run with GENERATE_GENESIS=true first"
    exit 1
  fi

  echo "=========================================="
  echo "Starting Validator $NODE_NUMBER"
  echo "=========================================="
  echo "Home:      $HOME_DIR"
  echo "P2P:       $P2P_PORT"
  echo "RPC:       $RPC_PORT"
  echo "JSON-RPC:  $JSONRPC_PORT"
  echo "=========================================="

  START_ARGS=(
    --pruning nothing
    --log_level "$LOGLEVEL"
    --minimum-gas-prices=0ogwei
    --evm.min-tip=0
    --home "$HOME_DIR"
    --json-rpc.api eth,txpool,personal,net,debug,web3
    --chain-id "$CHAINID"
  )

  exec evmd start "${START_ARGS[@]}"
}

if [[ "$GENERATE_GENESIS" == "true" ]]; then
  generate_genesis
fi

if [[ "$START_VALIDATOR" == "true" ]]; then
  start_validator
fi

if [[ "$GENERATE_GENESIS" != "true" && "$START_VALIDATOR" != "true" ]]; then
  echo "No mode specified."
  echo ""
  echo "To generate genesis:"
  echo "  GENERATE_GENESIS=true $0 -y"
  echo ""
  echo "To start a validator:"
  echo "  START_VALIDATOR=true NODE_NUMBER=0 $0"
  echo ""
  usage
fi
