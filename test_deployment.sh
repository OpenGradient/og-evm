#!/bin/bash

CHAINID="${CHAIN_ID:-10740}"
MONIKER="ogevmdevnettest"
# Remember to change to other types of keyring like 'file' in-case exposing to outside world,
# otherwise your balance will be wiped quickly
# The keyring test does not require private key to steal tokens from you
KEYRING="test"
KEYALGO="eth_secp256k1"

LOGLEVEL="info"
# Set dedicated home directory for the evmd instance
CHAINDIR="$HOME/.og-evm-devnet"

BASEFEE=10000000

# Path variables
CONFIG_TOML=$CHAINDIR/config/config.toml
APP_TOML=$CHAINDIR/config/app.toml
GENESIS=$CHAINDIR/config/genesis.json
TMP_GENESIS=$CHAINDIR/config/tmp_genesis.json

# validate dependencies are installed
command -v jq >/dev/null 2>&1 || {
  echo >&2 "jq not installed. More info: https://stedolan.github.io/jq/download/"
  exit 1
}

# used to exit on first error (any non-zero exit code)
set -e

# ------------- Flags -------------
install=false
overwrite=""
BUILD_FOR_DEBUG=false
ADDITIONAL_USERS=0
MNEMONIC_FILE=""      # output file (defaults later to $CHAINDIR/mnemonics.yaml)
MNEMONICS_INPUT=""    # input yaml to prefill dev keys

usage() {
  cat <<EOF
Usage: $0 [options]

Options:
  -y                       Overwrite existing chain data without prompt
  -n                       Do not overwrite existing chain data
  --no-install             Skip 'make install'
  --remote-debugging       Build with nooptimization,nostrip
  --additional-users N     Create N extra users: dev4, dev5, ...
  --mnemonic-file PATH     Where to write mnemonics YAML (default: \$HOME/.evmd/mnemonics.yaml)
  --mnemonics-input PATH   Read dev mnemonics from a yaml file (key: mnemonics:)
EOF
}

while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
    -y)
      echo "Flag -y passed -> Overwriting the previous chain data."
      overwrite="y"; shift
      ;;
    -n)
      echo "Flag -n passed -> Not overwriting the previous chain data."
      overwrite="n"; shift
      ;;
    --no-install)
      echo "Flag --no-install passed -> Skipping installation of the evmd binary."
      install=false; shift
      ;;
    --remote-debugging)
      echo "Flag --remote-debugging passed -> Building with remote debugging options."
      BUILD_FOR_DEBUG=true; shift
      ;;
    --additional-users)
      if [[ -z "${2:-}" || "$2" =~ ^- ]]; then
        echo "Error: --additional-users requires a number."; usage; exit 1
      fi
      ADDITIONAL_USERS="$2"; shift 2
      ;;
    --mnemonic-file)
      if [[ -z "${2:-}" || "$2" =~ ^- ]]; then
        echo "Error: --mnemonic-file requires a path."; usage; exit 1
      fi
      MNEMONIC_FILE="$2"; shift 2
      ;;
    --mnemonics-input)
      if [[ -z "${2:-}" || "$2" =~ ^- ]]; then
        echo "Error: --mnemonics-input requires a path."; usage; exit 1
      fi
      MNEMONICS_INPUT="$2"; shift 2
      ;;
    -h|--help)
      usage; exit 0
      ;;
    *)
      echo "Unknown flag passed: $key -> Aborting"; usage; exit 1
      ;;
  esac
done

if [[ -n "$MNEMONICS_INPUT" && "$ADDITIONAL_USERS" -gt 0 ]]; then
  echo "Error: --mnemonics-input and --additional-users cannot be used together."
  echo "Use --mnemonics-input to provide all dev account mnemonics, or use --additional-users to generate extra accounts."
  exit 1
fi

if [[ $install == true ]]; then
  if [[ $BUILD_FOR_DEBUG == true ]]; then
    # for remote debugging the optimization should be disabled and the debug info should not be stripped
    make install COSMOS_BUILD_OPTIONS=nooptimization,nostrip
  else
    make install
  fi
fi

# User prompt if neither -y nor -n was passed as a flag
# and an existing local node configuration is found.
if [[ $overwrite = "" ]]; then
  if [ -d "$CHAINDIR" ]; then
    # check if chaindir/config/genesis.json exists or not
    if [ -f "$CHAINDIR/config/genesis.json" ]; then
      printf "\nAn existing folder at '%s' with genesis was found. You can choose to delete this folder and start a new local node with new keys from genesis. When declined, the existing local node is started. \n" "$CHAINDIR"
      overwrite="n"
    else
      overwrite="y"
    fi
  else
    overwrite="y"
  fi
fi

# ---------- YAML reader ----------
# reads a simple yaml with:
# mnemonics:
#   - "phrase here"
#   - another phrase
read_mnemonics_yaml() {
  local file="$1"
  awk '
    BEGIN { inlist=0 }
    /^[[:space:]]*mnemonics:[[:space:]]*$/ { inlist=1; next }
    inlist && /^[[:space:]]*-[[:space:]]*/ {
      line=$0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      gsub(/^"[[:space:]]*|[[:space:]]*"$/, "", line)
      gsub(/^'\''[[:space:]]*|[[:space:]]*'\''$/, "", line)
      print line
      next
    }
    inlist && NF==0 { next }
  ' "$file"
}

# ---------- yaml writer ----------
write_mnemonics_yaml() {
  local file_path="$1"; shift
  local -a mns=("$@")
  mkdir -p "$(dirname "$file_path")"
  {
    echo "mnemonics:"
    for m in "${mns[@]}"; do
      printf '  - "%s"\n' "$m"
    done
  } > "$file_path"
  echo "Wrote mnemonics to $file_path"
}

# ---------- Add funded account ----------
add_genesis_funds() {
  local keyname="$1"
  evmd genesis add-genesis-account "$keyname" 1000000000000000000000ogwei --keyring-backend "$KEYRING" --home "$CHAINDIR"
}

# Setup local node if overwrite is set to Yes, otherwise skip setup
if [[ $overwrite == "y" || $overwrite == "Y" ]]; then
  rm -rf "$CHAINDIR"

  evmd config set client chain-id "$CHAINID" --home "$CHAINDIR"
  evmd config set client keyring-backend "$KEYRING" --home "$CHAINDIR"

  # ---------------- Validator key ----------------
  VAL_KEY="mykey"
  evmd keys add "$VAL_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"


  provided_mnemonics=()
  if [[ -n "$MNEMONICS_INPUT" ]]; then
    if [[ ! -f "$MNEMONICS_INPUT" ]]; then
      echo "mnemonics input file not found: $MNEMONICS_INPUT"; exit 1
    fi

    tmpfile="$(mktemp -t mnemonics.XXXXXX)"
    read_mnemonics_yaml "$MNEMONICS_INPUT" > "$tmpfile"

    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      provided_mnemonics+=( "$line" )
    done < "$tmpfile"
    rm -f "$tmpfile"

    if [[ ${#provided_mnemonics[@]} -eq 0 ]]; then
      echo "no mnemonics found in $MNEMONICS_INPUT (expected a list under 'mnemonics:')"; exit 1
    fi
  fi

  # choose base list: prefer provided over defaults
  if [[ ${#provided_mnemonics[@]} -gt 0 ]]; then
    echo "using provided mnemonics"
    dev_mnemonics=("${provided_mnemonics[@]}")
  else
    echo "using default mnemonics"
    dev_mnemonics=("${default_mnemonics[@]}")
  fi

  # init chain w/ validator mnemonic
  evmd init $MONIKER -o --chain-id "$CHAINID" --home "$CHAINDIR"

  # ---------- Genesis customizations ----------
  jq '.app_state["staking"]["params"]["bond_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["expedited_min_deposit"][0]["denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["evm"]["params"]["evm_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["mint"]["params"]["mint_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["bank"]["denom_metadata"]=[{"description":"The native staking token for evmd.","denom_units":[{"denom":"ogwei","exponent":0,"aliases":[]},{"denom":"OGETH","exponent":18,"aliases":[]}],"base":"ogwei","display":"OGETH","name":"ETH Token","symbol":"OGETH","uri":"","uri_hash":""}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["evm"]["params"]["active_static_precompiles"]=["0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805", "0x0000000000000000000000000000000000000806", "0x0000000000000000000000000000000000000807"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["evm"]["params"]["evm_denom"]="ogwei"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state.erc20.native_precompiles=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state.erc20.token_pairs=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"ogwei",enabled:true}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.consensus.params.block.max_gas="10000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  # Change proposal periods
  sed -i.bak 's/"max_deposit_period": "172800s"/"max_deposit_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"voting_period": "172800s"/"voting_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"expedited_voting_period": "86400s"/"expedited_voting_period": "15s"/g' "$GENESIS"

  # fund validator (devs already funded in the loop)
  evmd genesis add-genesis-account "$VAL_KEY" 100000000000000000000000000ogwei --keyring-backend "$KEYRING" --home "$CHAINDIR"

  # ---------- Config customizations ----------
  sed -i.bak 's/timeout_propose = "3s"/timeout_propose = "2s"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_prevote = "1s"/timeout_prevote = "500ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_precommit = "1s"/timeout_precommit = "500ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "200ms"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$CONFIG_TOML"
  sed -i.bak 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "5s"/g' "$CONFIG_TOML"

  # enable prometheus metrics and all APIs for dev node
  sed -i.bak 's/prometheus = false/prometheus = true/' "$CONFIG_TOML"
  sed -i.bak 's/prometheus-retention-time  = "0"/prometheus-retention-time  = "1000000000000"/g' "$APP_TOML"
  sed -i.bak 's/enabled = false/enabled = true/g' "$APP_TOML"
  sed -i.bak 's/enable = false/enable = true/g' "$APP_TOML"
  sed -i.bak 's/enable-indexer = false/enable-indexer = true/g' "$APP_TOML"

  # --------- maybe generate additional users ---------
  # start with provided/default list
  final_mnemonics=("${dev_mnemonics[@]}")

  # default output path if not set
  if [[ -z "$MNEMONIC_FILE" ]]; then
    MNEMONIC_FILE="$CHAINDIR/mnemonics.yaml"
  fi

  # Generate 100 dev accounts (dev0 through dev99)
  for ((i=0; i<100; i++)); do
    keyname="dev${i}"

    echo "adding key for $keyname"

    # Create key
    evmd keys add "$keyname" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"

    # Fund the account in genesis
    add_genesis_funds "$keyname"
  done

  if [[ "$ADDITIONAL_USERS" -gt 0 ]]; then
    start_index=${#dev_mnemonics[@]}   # continue after last provided/default entry
    for ((i=0; i<ADDITIONAL_USERS; i++)); do
      idx=$((start_index + i))
      keyname="dev${idx}"

      # create key and capture mnemonic
      mnemonic_out="$(evmd keys add "$keyname" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR" 2>&1)"
      # try to grab a line that looks like a seed phrase (>=12 words), else last line
      user_mnemonic="$(echo "$mnemonic_out" | grep -E '([[:alpha:]]+[[:space:]]+){11,}[[:alpha:]]+$' | tail -1)"
      if [[ -z "$user_mnemonic" ]]; then
        user_mnemonic="$(echo "$mnemonic_out" | tail -n 1)"
      fi
      user_mnemonic="$(echo "$user_mnemonic" | tr -d '\r')"

      if [[ -z "$user_mnemonic" ]]; then
        echo "failed to capture mnemonic for $keyname"; exit 1
      fi

      final_mnemonics+=("$user_mnemonic")
      add_genesis_funds "$keyname"
      echo "created $keyname"
    done
  fi

  # --------- Finalize genesis ---------
  evmd genesis gentx "$VAL_KEY" 1000000000000000000000ogwei --gas-prices ${BASEFEE}ogwei --keyring-backend "$KEYRING" --chain-id "$CHAINID" --home "$CHAINDIR"
  evmd genesis collect-gentxs --home "$CHAINDIR"
  evmd genesis validate-genesis --home "$CHAINDIR"

  # --------- Write YAML with mnemonics if the user specified more ---------
  if [[ "$ADDITIONAL_USERS" -gt 0 ]]; then
    write_mnemonics_yaml "$MNEMONIC_FILE" "${final_mnemonics[@]}"
  fi

  if [[ $1 == "pending" ]]; then
    echo "pending mode is on, please wait for the first block committed."
  fi
fi

# Start the node
evmd start "$TRACE" \
	--pruning nothing \
	--log_level $LOGLEVEL \
	--minimum-gas-prices=0ogwei \
	--evm.min-tip=0 \
	--home "$CHAINDIR" \
	--json-rpc.api eth,txpool,personal,net,debug,web3 \
  --json-rpc.address 0.0.0.0:8545 \
  --json-rpc.ws-address 0.0.0.0:8546 \
  --rpc.laddr tcp://0.0.0.0:26657 \
	--chain-id "$CHAINID"
