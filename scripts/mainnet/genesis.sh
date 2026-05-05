#!/bin/bash
#
# OG-EVM Mainnet Genesis Builder
#
# Produces the canonical genesis.json for opengradient_1486-1.
#
# In normal mode the script does NOT generate validator gentxs — those come from each
# operator and are merged in via `evmd genesis collect-gentxs` (see README.md).
# In --dry-run mode the script generates ephemeral keys for all 4 validators, exercises
# a 1-validator gentx + collect cycle, and runs `evmd genesis validate` end-to-end.
#
# Usage:
#   bash scripts/mainnet/genesis.sh \
#     --validators-yaml scripts/mainnet/validators.yaml \
#     --foundation-yaml scripts/mainnet/foundation.yaml \
#     --genesis-time 2026-06-01T15:00:00Z \
#     --out-dir /tmp/og-mainnet
#
#   bash scripts/mainnet/genesis.sh --dry-run --out-dir /tmp/og-dryrun

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/precompiles.sh
source "$SCRIPT_DIR/../lib/precompiles.sh"

# ---------- Constants ----------
CHAIN_ID="opengradient_1486-1"
BASE_DENOM="ogwei"
DISPLAY_DENOM="OPG"
KEY_ALGO="eth_secp256k1"
KEYRING="test"          # ceremony validators use --keyring-backend=file; --dry-run uses test
TOTAL_SUPPLY_OPG=1000000000
SVIP_ALLOC_OPG=100000000
VALIDATOR_COUNT_EXPECTED=4
# Symbolic genesis self-stake. Voting power in PoA comes from /poa.MsgAddValidator,
# not from the gentx amount, so this is intentionally tiny (10 OPG). Validators can
# raise their self-delegation later with MsgDelegate / MsgEditValidator.
DEFAULT_SELF_STAKE_OPG=10

UNBONDING_TIME="1814400s"
MAX_VALIDATORS=75
MAX_ENTRIES=7
HISTORICAL_ENTRIES=10000
MIN_COMMISSION="0.050000000000000000"
COMMUNITY_TAX="0.000000000000000000"
INFLATION="0.000000000000000000"
GOAL_BONDED="0.670000000000000000"
BLOCKS_PER_YEAR=15768000
GOV_MIN_DEPOSIT_OPG=5000
GOV_EXP_MIN_DEPOSIT_OPG=25000
MAX_DEPOSIT_PERIOD="604800s"
VOTING_PERIOD="432000s"
QUORUM="0.334000000000000000"
THRESHOLD="0.500000000000000000"
VETO_THRESHOLD="0.334000000000000000"
EXP_VOTING_PERIOD="86400s"
EXP_THRESHOLD="0.667000000000000000"
SIGNED_BLOCKS_WINDOW="10000"
MIN_SIGNED_PER_WINDOW="0.050000000000000000"
DOWNTIME_JAIL_DURATION="600s"
SLASH_DOUBLE_SIGN="0.000000000000000000"
SLASH_DOWNTIME="0.000000000000000000"
BASE_FEE_OGWEI="1000000000.000000000000000000"
MIN_GAS_PRICE_OGWEI="1.000000000000000000"
BASE_FEE_DENOMINATOR=8
ELASTICITY_MULTIPLIER=2
MIN_GAS_MULTIPLIER="0.500000000000000000"
BLOCK_MAX_BYTES="22020096"
BLOCK_MAX_GAS="40000000"
EVIDENCE_MAX_AGE_BLOCKS="100000"
EVIDENCE_MAX_AGE_DURATION="172800000000000"
EVIDENCE_MAX_BYTES="1048576"
HISTORY_SERVE_WINDOW="8192"
CRISIS_FEE_OPG=10000
COMMISSION_MAX="0.500000000000000000"
COMMISSION_CHANGE_MAX="0.010000000000000000"
# Matches DEFAULT_SELF_STAKE_OPG: gentx fails if self-delegation < min-self-delegation.
# Each validator can raise their floor post-launch with MsgEditValidator.
MIN_SELF_DELEGATION_OPG=10

NATIVE_PRECOMPILE='"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"'

# ---------- Helpers ----------
die()     { echo "ERROR: $*" >&2; exit 1; }
require() { [[ -n "${!1:-}" ]] || die "$1 is required"; }
guard()   { [[ "$2" == "$3" ]] || die "GUARD FAIL: $1 = $2 (expected $3)"; ok "$1 = $2"; }
ok()      { echo "  ✓ $*"; }

# OPG → ogwei (10^18). OPG is a non-negative integer; appending 18 zeros is exact and
# avoids needing `bc`.
opg_to_ogwei() {
  printf '%s000000000000000000\n' "$1"
}

# Apply one or more jq filters to genesis.json in place.
jqp() {
  jq "$@" "$GENESIS" > "$TMP"
  mv "$TMP" "$GENESIS"
}

# Cosmos SDK module account address: bech32(og, sha256(name)[:20]).
# Equivalent to Go: authtypes.NewModuleAddress(name).String() with the og bech32 prefix.
module_address() {
  local hex
  hex=$(printf '%s' "$1" | shasum -a 256 | awk '{print substr($1,1,40)}')
  evmd debug addr "$hex" 2>/dev/null | awk '/^Bech32 Acc / {print $NF}'
}

# ---------- CLI parsing ----------
VALIDATORS_YAML=""; FOUNDATION_YAML=""; GENESIS_TIME=""; OUT_DIR=""; DRY_RUN=false

usage() { sed -n '2,18p' "$0"; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --validators-yaml) VALIDATORS_YAML="$2"; shift 2 ;;
    --foundation-yaml) FOUNDATION_YAML="$2"; shift 2 ;;
    --genesis-time)    GENESIS_TIME="$2"; shift 2 ;;
    --out-dir)         OUT_DIR="$2"; shift 2 ;;
    --dry-run)         DRY_RUN=true; shift ;;
    -h|--help)         usage ;;
    *) die "unknown flag: $1" ;;
  esac
done

require OUT_DIR
if ! $DRY_RUN; then
  require VALIDATORS_YAML
  require FOUNDATION_YAML
  require GENESIS_TIME
fi

for cmd in evmd jq shasum bc; do command -v "$cmd" >/dev/null || die "$cmd not installed"; done
$DRY_RUN || command -v yq >/dev/null || die "yq not installed (Mike Farah Go version)"

CHAINDIR="$OUT_DIR"
GENESIS="$CHAINDIR/config/genesis.json"
TMP="$CHAINDIR/config/tmp_genesis.json"

echo "==> output dir: $CHAINDIR"
rm -rf "$CHAINDIR"

$DRY_RUN && GENESIS_TIME="2026-06-01T00:00:00Z"

# ---------- 1. Init chain ----------
mkdir -p "$CHAINDIR"
evmd init og-mainnet -o --chain-id "$CHAIN_ID" --home "$CHAINDIR" >/dev/null 2>&1

# ---------- 2. Top-level + denoms + denom_metadata ----------
echo "==> chain identity, denoms, denom_metadata"
jqp \
  --arg chain_id "$CHAIN_ID" \
  --arg genesis_time "$GENESIS_TIME" \
  --arg denom "$BASE_DENOM" \
  --arg display "$DISPLAY_DENOM" \
  '
    .chain_id = $chain_id
    | .genesis_time = $genesis_time
    | .app_state.staking.params.bond_denom = $denom
    | .app_state.gov.params.min_deposit[0].denom = $denom
    | .app_state.gov.params.expedited_min_deposit[0].denom = $denom
    | .app_state.evm.params.evm_denom = $denom
    | .app_state.mint.params.mint_denom = $denom
    | .app_state.bank.denom_metadata = [{
        description: "OpenGradient native token",
        denom_units: [
          { denom: $denom,   exponent: 0,  aliases: [] },
          { denom: $display, exponent: 18, aliases: [] }
        ],
        base: $denom, display: $display,
        name: "OpenGradient", symbol: $display,
        uri: "", uri_hash: ""
      }]
  '

# ---------- 3. Staking ----------
echo "==> staking"
jqp \
  --arg unbonding "$UNBONDING_TIME" \
  --argjson max_v "$MAX_VALIDATORS" \
  --argjson max_e "$MAX_ENTRIES" \
  --argjson hist "$HISTORICAL_ENTRIES" \
  --arg min_comm "$MIN_COMMISSION" \
  '.app_state.staking.params |= (
      .unbonding_time = $unbonding
    | .max_validators = $max_v
    | .max_entries = $max_e
    | .historical_entries = $hist
    | .min_commission_rate = $min_comm
  )'

# ---------- 4. Mint (zero inflation) ----------
echo "==> mint (zero inflation)"
jqp \
  --arg zero "$INFLATION" \
  --arg goal "$GOAL_BONDED" \
  --argjson bpy "$BLOCKS_PER_YEAR" \
  '
    .app_state.mint.minter |= (
        .inflation = $zero
      | .annual_provisions = "0.000000000000000000"
    )
    | .app_state.mint.params |= (
        .inflation_rate_change = $zero
      | .inflation_max = $zero
      | .inflation_min = $zero
      | .goal_bonded = $goal
      | .blocks_per_year = ($bpy | tostring)
    )
  '

# ---------- 5. Distribution (zero community tax) ----------
echo "==> distribution (zero community tax)"
jqp \
  --arg ctax "$COMMUNITY_TAX" \
  '.app_state.distribution.params |= (
      .community_tax = $ctax
    | .base_proposer_reward = "0.000000000000000000"
    | .bonus_proposer_reward = "0.000000000000000000"
    | .withdraw_addr_enabled = true
  )'

# ---------- 6. Governance ----------
echo "==> governance"
GOV_MIN_DEPOSIT_WEI=$(opg_to_ogwei "$GOV_MIN_DEPOSIT_OPG")
GOV_EXP_MIN_DEPOSIT_WEI=$(opg_to_ogwei "$GOV_EXP_MIN_DEPOSIT_OPG")
jqp \
  --arg denom "$BASE_DENOM" \
  --arg min_dep "$GOV_MIN_DEPOSIT_WEI" \
  --arg exp_min "$GOV_EXP_MIN_DEPOSIT_WEI" \
  --arg max_dep_period "$MAX_DEPOSIT_PERIOD" \
  --arg vote_period "$VOTING_PERIOD" \
  --arg quorum "$QUORUM" \
  --arg threshold "$THRESHOLD" \
  --arg veto "$VETO_THRESHOLD" \
  --arg exp_period "$EXP_VOTING_PERIOD" \
  --arg exp_thresh "$EXP_THRESHOLD" \
  '.app_state.gov.params |= (
      .min_deposit = [{denom: $denom, amount: $min_dep}]
    | .expedited_min_deposit = [{denom: $denom, amount: $exp_min}]
    | .max_deposit_period = $max_dep_period
    | .voting_period = $vote_period
    | .quorum = $quorum
    | .threshold = $threshold
    | .veto_threshold = $veto
    | .expedited_voting_period = $exp_period
    | .expedited_threshold = $exp_thresh
    | .burn_vote_veto = true
    | .burn_vote_quorum = false
    | .burn_proposal_deposit_prevote = false
  )'

# ---------- 7. Slashing (disabled at launch) ----------
echo "==> slashing (slash fractions = 0)"
jqp \
  --arg window "$SIGNED_BLOCKS_WINDOW" \
  --arg minsig "$MIN_SIGNED_PER_WINDOW" \
  --arg jail "$DOWNTIME_JAIL_DURATION" \
  --arg ds "$SLASH_DOUBLE_SIGN" \
  --arg dt "$SLASH_DOWNTIME" \
  '.app_state.slashing.params |= (
      .signed_blocks_window = $window
    | .min_signed_per_window = $minsig
    | .downtime_jail_duration = $jail
    | .slash_fraction_double_sign = $ds
    | .slash_fraction_downtime = $dt
  )'

# ---------- 8. EVM ----------
echo "==> evm + precompiles"
jqp \
  --argjson precompiles "$ACTIVE_STATIC_PRECOMPILES" \
  --arg history "$HISTORY_SERVE_WINDOW" \
  '.app_state.evm.params |= (
      .access_control.create.access_type = "ACCESS_TYPE_PERMISSIONLESS"
    | .access_control.call.access_type = "ACCESS_TYPE_PERMISSIONLESS"
    | .active_static_precompiles = $precompiles
    | .history_serve_window = $history
    | .extra_eips = []
  )'

# ---------- 9. Feemarket (EIP-1559) ----------
echo "==> feemarket (EIP-1559)"
jqp \
  --arg base_fee "$BASE_FEE_OGWEI" \
  --arg min_gas "$MIN_GAS_PRICE_OGWEI" \
  --argjson denom "$BASE_FEE_DENOMINATOR" \
  --argjson elastic "$ELASTICITY_MULTIPLIER" \
  --arg multiplier "$MIN_GAS_MULTIPLIER" \
  '.app_state.feemarket.params |= (
      .no_base_fee = false
    | .base_fee = $base_fee
    | .min_gas_price = $min_gas
    | .base_fee_change_denominator = $denom
    | .elasticity_multiplier = $elastic
    | .min_gas_multiplier = $multiplier
    | .enable_height = "0"
  )'

# ---------- 10. ERC-20 ----------
echo "==> erc20"
jqp \
  --argjson native "$NATIVE_PRECOMPILE" \
  --arg denom "$BASE_DENOM" \
  '
    .app_state.erc20.params |= (.enable_erc20 = true | .permissionless_registration = false)
    | .app_state.erc20.native_precompiles = [$native]
    | .app_state.erc20.token_pairs = [{
        contract_owner: 1,
        erc20_address: $native,
        denom: $denom,
        enabled: true
      }]
  '

# ---------- 11. SVIP (dormant at genesis) ----------
# Schema per proto/cosmos/svip/v1/genesis.proto on origin/feat/svip-module.
echo "==> svip (dormant)"
jqp '
  .app_state.svip = {
    params: { half_life_seconds: "0" },
    activated: false,
    paused: false,
    total_distributed: "0",
    pool_balance_at_activation: "0",
    activation_time: "0001-01-01T00:00:00Z",
    last_block_time: "0001-01-01T00:00:00Z",
    total_paused_seconds: "0"
  }
'

# ---------- 12. Crisis ----------
echo "==> crisis"
CRISIS_FEE_WEI=$(opg_to_ogwei "$CRISIS_FEE_OPG")
jqp \
  --arg denom "$BASE_DENOM" \
  --arg amt "$CRISIS_FEE_WEI" \
  '.app_state.crisis.constant_fee = {denom: $denom, amount: $amt}'

# ---------- 13. IBC transfer ----------
echo "==> ibc transfer"
jqp '.app_state.transfer.params |= (.send_enabled = true | .receive_enabled = true)'

# ---------- 14. Consensus params ----------
echo "==> consensus params"
jqp \
  --arg max_bytes "$BLOCK_MAX_BYTES" \
  --arg max_gas "$BLOCK_MAX_GAS" \
  --arg ev_blocks "$EVIDENCE_MAX_AGE_BLOCKS" \
  --arg ev_dur "$EVIDENCE_MAX_AGE_DURATION" \
  --arg ev_bytes "$EVIDENCE_MAX_BYTES" \
  '.consensus.params |= (
      .block.max_bytes = $max_bytes
    | .block.max_gas = $max_gas
    | .evidence.max_age_num_blocks = $ev_blocks
    | .evidence.max_age_duration = $ev_dur
    | .evidence.max_bytes = $ev_bytes
    | .validator.pub_key_types = ["ed25519"]
  )'

# ---------- 15. Read inputs (or generate dry-run inputs) ----------
declare -a VAL_MONIKERS VAL_ADDRS VAL_SELF_STAKES
declare FOUND_ADDR FOUND_ALLOC_OPG

if $DRY_RUN; then
  echo "==> DRY RUN: generating ephemeral keys for foundation + $VALIDATOR_COUNT_EXPECTED validators"
  evmd keys add foundation --keyring-backend "$KEYRING" --algo "$KEY_ALGO" --home "$CHAINDIR" >/dev/null 2>&1
  FOUND_ADDR=$(evmd keys show foundation -a --keyring-backend "$KEYRING" --home "$CHAINDIR")
  FOUND_ALLOC_OPG=null
  for i in $(seq 1 $VALIDATOR_COUNT_EXPECTED); do
    keyname="val${i}"
    evmd keys add "$keyname" --keyring-backend "$KEYRING" --algo "$KEY_ALGO" --home "$CHAINDIR" >/dev/null 2>&1
    VAL_MONIKERS[i]="og-mainnet-val-$i"
    VAL_ADDRS[i]=$(evmd keys show "$keyname" -a --keyring-backend "$KEYRING" --home "$CHAINDIR")
    VAL_SELF_STAKES[i]=$DEFAULT_SELF_STAKE_OPG
  done
else
  echo "==> reading $FOUNDATION_YAML and $VALIDATORS_YAML"
  FOUND_ADDR=$(yq -r '.foundation.address' "$FOUNDATION_YAML")
  FOUND_ALLOC_OPG=$(yq -r '.foundation.allocation_opg' "$FOUNDATION_YAML")
  # Catch a forgotten template: refuse to bootstrap with placeholder multisig metadata.
  FOUND_THRESHOLD=$(yq -r '.foundation.multisig_threshold' "$FOUNDATION_YAML")
  FOUND_MEMBERS=$(yq -r '.foundation.multisig_members | length' "$FOUNDATION_YAML")
  [[ "$FOUND_THRESHOLD" =~ ^[0-9]+$ && "$FOUND_THRESHOLD" -gt 0 ]] \
    || die "foundation.multisig_threshold must be a positive integer (got '$FOUND_THRESHOLD')"
  [[ "$FOUND_MEMBERS" -ge "$FOUND_THRESHOLD" ]] \
    || die "foundation.multisig_members has $FOUND_MEMBERS entries, fewer than threshold $FOUND_THRESHOLD"
  vcount=$(yq -r '.validators | length' "$VALIDATORS_YAML")
  [[ "$vcount" == "$VALIDATOR_COUNT_EXPECTED" ]] || die "expected $VALIDATOR_COUNT_EXPECTED validators, got $vcount"
  for i in $(seq 1 "$vcount"); do
    idx=$((i-1))
    VAL_MONIKERS[i]=$(yq -r ".validators[$idx].moniker" "$VALIDATORS_YAML")
    VAL_ADDRS[i]=$(yq -r ".validators[$idx].operator_addr" "$VALIDATORS_YAML")
    VAL_SELF_STAKES[i]=$(yq -r ".validators[$idx].self_stake_opg" "$VALIDATORS_YAML")
  done
fi

[[ "$FOUND_ADDR" =~ ^og1 ]] || die "foundation address must start with og1: '$FOUND_ADDR'"
for i in $(seq 1 $VALIDATOR_COUNT_EXPECTED); do
  [[ "${VAL_ADDRS[i]}" =~ ^og1 ]] || die "validator $i address must start with og1: '${VAL_ADDRS[i]}'"
done

# ---------- 16. Compute supply allocation ----------
TOTAL_VAL_STAKE_OPG=0
for i in $(seq 1 $VALIDATOR_COUNT_EXPECTED); do
  TOTAL_VAL_STAKE_OPG=$((TOTAL_VAL_STAKE_OPG + VAL_SELF_STAKES[i]))
done

if [[ "$FOUND_ALLOC_OPG" == "null" || -z "$FOUND_ALLOC_OPG" ]]; then
  FOUND_ALLOC_OPG=$((TOTAL_SUPPLY_OPG - SVIP_ALLOC_OPG - TOTAL_VAL_STAKE_OPG))
  echo "==> foundation allocation auto-computed: $FOUND_ALLOC_OPG OPG"
fi

SUM=$((FOUND_ALLOC_OPG + SVIP_ALLOC_OPG + TOTAL_VAL_STAKE_OPG))
[[ "$SUM" == "$TOTAL_SUPPLY_OPG" ]] || die "allocation sum is $SUM OPG, expected $TOTAL_SUPPLY_OPG"

# ---------- 17. Add genesis accounts ----------
echo "==> add genesis accounts"
evmd genesis add-genesis-account "$FOUND_ADDR" "$(opg_to_ogwei "$FOUND_ALLOC_OPG")${BASE_DENOM}" --home "$CHAINDIR" >/dev/null
for i in $(seq 1 $VALIDATOR_COUNT_EXPECTED); do
  evmd genesis add-genesis-account "${VAL_ADDRS[i]}" "$(opg_to_ogwei "${VAL_SELF_STAKES[i]}")${BASE_DENOM}" --home "$CHAINDIR" >/dev/null
done

# ---------- 18. Pre-fund x/svip module account ----------
SVIP_ADDR=$(module_address "svip")
[[ "$SVIP_ADDR" =~ ^og1 ]] || die "failed to derive svip module address"
echo "==> svip module account: $SVIP_ADDR ($SVIP_ALLOC_OPG OPG)"
SVIP_WEI=$(opg_to_ogwei "$SVIP_ALLOC_OPG")
# add-genesis-account would create a BaseAccount. The svip module account is materialised
# by auth's InitGenesis (via maccperms) — we just seed the bank balance here.
jqp \
  --arg addr "$SVIP_ADDR" \
  --arg denom "$BASE_DENOM" \
  --arg amt "$SVIP_WEI" \
  '.app_state.bank.balances += [{address: $addr, coins: [{denom: $denom, amount: $amt}]}]'

# ---------- 19. Update bank.supply ----------
TOTAL_SUPPLY_WEI=$(opg_to_ogwei "$TOTAL_SUPPLY_OPG")
jqp \
  --arg denom "$BASE_DENOM" \
  --arg amt "$TOTAL_SUPPLY_WEI" \
  '.app_state.bank.supply = [{denom: $denom, amount: $amt}]'

# ---------- 20. Gentxs ----------
if $DRY_RUN; then
  echo "==> DRY RUN: producing gentx for val1"
  evmd genesis gentx val1 \
    "$(opg_to_ogwei "${VAL_SELF_STAKES[1]}")${BASE_DENOM}" \
    --commission-rate "0.05" \
    --commission-max-rate "$COMMISSION_MAX" \
    --commission-max-change-rate "$COMMISSION_CHANGE_MAX" \
    --min-self-delegation "$(opg_to_ogwei "$MIN_SELF_DELEGATION_OPG")" \
    --keyring-backend "$KEYRING" \
    --chain-id "$CHAIN_ID" \
    --home "$CHAINDIR" \
    --moniker "${VAL_MONIKERS[1]}" \
    --gas-prices "${BASE_FEE_OGWEI%.*}${BASE_DENOM}" \
    >/dev/null
  evmd genesis collect-gentxs --home "$CHAINDIR" >/dev/null
else
  cat <<EOF

==> Pre-gentx genesis assembled. Distribute $GENESIS to all $VALIDATOR_COUNT_EXPECTED operators.

   Each operator (with their own \$NODE_HOME):

     cp <received-genesis.json> \$NODE_HOME/config/genesis.json
     evmd genesis gentx <operator-key> $(opg_to_ogwei $DEFAULT_SELF_STAKE_OPG)$BASE_DENOM \\
       --commission-rate 0.05 --commission-max-rate $COMMISSION_MAX \\
       --commission-max-change-rate $COMMISSION_CHANGE_MAX \\
       --min-self-delegation $(opg_to_ogwei $MIN_SELF_DELEGATION_OPG) \\
       --keyring-backend file --chain-id $CHAIN_ID \\
       --gas-prices ${BASE_FEE_OGWEI%.*}${BASE_DENOM} --moniker <moniker>

   Operators submit the gentx file. Foundation drops all $VALIDATOR_COUNT_EXPECTED into
   \$CHAINDIR/config/gentx/ then runs:

     evmd genesis collect-gentxs --home $CHAINDIR

EOF
fi

# ---------- 21. Hard guards ----------
echo ""
echo "==> hard-guard verification"
read -r INF INF_MAX CTAX MAX_GAS SVIP_ACT BOND_D EVM_D MINT_D <<< "$(
  jq -r '[
    .app_state.mint.minter.inflation,
    .app_state.mint.params.inflation_max,
    .app_state.distribution.params.community_tax,
    .consensus.params.block.max_gas,
    (.app_state.svip.activated | tostring),
    .app_state.staking.params.bond_denom,
    .app_state.evm.params.evm_denom,
    .app_state.mint.params.mint_denom
  ] | @tsv' "$GENESIS"
)"

guard "mint.inflation"           "$INF"      "$INFLATION"
guard "mint.inflation_max"       "$INF_MAX"  "$INFLATION"
guard "distribution.community_tax" "$CTAX"   "$COMMUNITY_TAX"
[[ "$MAX_GAS" != "-1" ]] || die "block.max_gas = -1 (unbounded)"
ok "block.max_gas = $MAX_GAS (finite)"
guard "svip.activated"           "$SVIP_ACT" "false"
[[ "$BOND_D" == "$BASE_DENOM" && "$EVM_D" == "$BASE_DENOM" && "$MINT_D" == "$BASE_DENOM" ]] \
  || die "denom mismatch (bond=$BOND_D evm=$EVM_D mint=$MINT_D)"
ok "bond_denom = evm_denom = mint_denom = $BASE_DENOM"

# `bc` for the only arbitrary-precision arithmetic (27-digit ogwei sum overflows bash int).
balances_sum=$(jq -r '.app_state.bank.balances[].coins[] | select(.denom=="ogwei") | .amount' "$GENESIS" \
  | paste -sd+ - | bc)
supply=$(jq -r '.app_state.bank.supply[0].amount' "$GENESIS")
guard "Σ balances vs supply"     "$balances_sum" "$supply"
guard "supply"                   "$supply"   "$TOTAL_SUPPLY_WEI"

# ---------- 22. evmd genesis validate ----------
echo ""
echo "==> evmd genesis validate"
evmd genesis validate --home "$CHAINDIR"

echo ""
echo "==> SUCCESS"
echo "    chain_id:     $CHAIN_ID"
echo "    genesis_time: $GENESIS_TIME"
echo "    genesis.json: $GENESIS"
echo "    sha256:       $(shasum -a 256 "$GENESIS" | awk '{print $1}')"
