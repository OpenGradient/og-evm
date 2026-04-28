#!/usr/bin/env bash
set -euo pipefail

# rebalance_scenario_runner.sh — local E2E for x/poolrebalancer with a CommunityPool contract delegator.
# Interactive / manual inspection (pending queues, watch modes), not a deterministic CI harness.
#
# Scenarios: happy_path | caps | threshold_boundary | expansion
# Watch: watch (queues + pool reads)
# user_flow_multikey: CommunityPool multi-account E2E (user_flow_multikey.sh); see usage() "user_flow_multikey — what it is for"
#
# Caveat: user-withdraw undelegation maturity follows wall clock (header time vs completion), not block height.
# ============================================================================

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BASEDIR="${BASEDIR:-"$HOME/.og-evm-devnet"}"
NODE_RPC="${NODE_RPC:-"tcp://127.0.0.1:26657"}"
CHAIN_ID="${CHAIN_ID:-10740}"
KEYRING="${KEYRING:-test}"
HOME0="$BASEDIR/val0"
CHAIN_HOME="${CHAIN_HOME:-$BASEDIR}"
POOL_DELEGATOR_MODE="${POOL_DELEGATOR_MODE:-contract}"
POOL_OWNER_PK="${POOL_OWNER_PK:-0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305}" # gitleaks:allow
POOL_DEPOSITOR_PK="${POOL_DEPOSITOR_PK:-0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544}" # gitleaks:allow
MODULE_EVM="${MODULE_EVM:-0x786c305E2aAc2168BB7555Ef522c5F20a2cd0dA9}"
BOND_PRECOMPILE="${BOND_PRECOMPILE:-0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE}"
POOL_CONTRACT_ADDR="${POOL_CONTRACT_ADDR:-}"
GOV_FROM="${GOV_FROM:-mykey}"
GOV_HOME="${GOV_HOME:-}"
GOV_DEPOSIT="${GOV_DEPOSIT:-10000000ogwei}"
GOV_FEES="${GOV_FEES:-400000000000000ogwei}"
GOV_WAIT_INITIAL="${GOV_WAIT_INITIAL:-20}"
GOV_POLL_TIMEOUT="${GOV_POLL_TIMEOUT:-20}"
GOV_STATUS_TIMEOUT="${GOV_STATUS_TIMEOUT:-120}"
EVM_RPC="${EVM_RPC:-http://127.0.0.1:8545}"
POOL_SEED_DEPOSIT_AMOUNT="${POOL_SEED_DEPOSIT_AMOUNT:-200000000000000000000}"
DEFAULT_POOL_OWNER_PK="0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305"
DEFAULT_POOL_DEPOSITOR_PK="0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544"

# -----------------------------------------------------------------------------
# Runtime knobs (env vars take precedence)
# -----------------------------------------------------------------------------
# Track which knobs were explicitly provided via environment so scenario defaults
# can apply only when not set by the user.
USER_SET_MAX_TARGET_VALIDATORS=false
[[ -n "${POOLREBALANCER_MAX_TARGET_VALIDATORS+x}" ]] && USER_SET_MAX_TARGET_VALIDATORS=true
USER_SET_THRESHOLD_BP=false
[[ -n "${POOLREBALANCER_THRESHOLD_BP+x}" ]] && USER_SET_THRESHOLD_BP=true
USER_SET_MAX_OPS_PER_BLOCK=false
[[ -n "${POOLREBALANCER_MAX_OPS_PER_BLOCK+x}" ]] && USER_SET_MAX_OPS_PER_BLOCK=true
USER_SET_MAX_MOVE_PER_OP=false
[[ -n "${POOLREBALANCER_MAX_MOVE_PER_OP+x}" ]] && USER_SET_MAX_MOVE_PER_OP=true
USER_SET_STAKING_MAX_ENTRIES=false
[[ -n "${STAKING_MAX_ENTRIES+x}" ]] && USER_SET_STAKING_MAX_ENTRIES=true
USER_SET_IMBALANCE_MINOR_DELEGATION=false
[[ -n "${IMBALANCE_MINOR_DELEGATION+x}" ]] && USER_SET_IMBALANCE_MINOR_DELEGATION=true
USER_SET_POOL_SEED_DEPOSIT_AMOUNT=false
[[ -n "${POOL_SEED_DEPOSIT_AMOUNT+x}" ]] && USER_SET_POOL_SEED_DEPOSIT_AMOUNT=true
USER_SET_STAKING_UNBONDING_TIME=false
[[ -n "${STAKING_UNBONDING_TIME+x}" ]] && USER_SET_STAKING_UNBONDING_TIME=true
USER_SET_POLL_SAMPLES=false
[[ -n "${POLL_SAMPLES+x}" ]] && USER_SET_POLL_SAMPLES=true
USER_SET_POLL_SLEEP_SECS=false
[[ -n "${POLL_SLEEP_SECS+x}" ]] && USER_SET_POLL_SLEEP_SECS=true

SCENARIO="${SCENARIO:-happy_path}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-}"
POOLREBALANCER_MAX_TARGET_VALIDATORS="${POOLREBALANCER_MAX_TARGET_VALIDATORS:-3}"
# Demo profile controls default speed so users can observe behavior.
# slow   = very gradual progress (good for watching)
# medium = balanced default for demo
# fast   = converges quickly
DEMO_PROFILE="${DEMO_PROFILE:-medium}"
POOLREBALANCER_THRESHOLD_BP="${POOLREBALANCER_THRESHOLD_BP:-0}"
POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-2}"
POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-100000000000000000000}" # 1e20

# Tune staking params so maturity behavior is visible in test runs.
STAKING_UNBONDING_TIME="${STAKING_UNBONDING_TIME:-30s}"
STAKING_MAX_ENTRIES="${STAKING_MAX_ENTRIES:-100}"

TX_FEES="${TX_FEES:-200000000000000ogwei}" # denom will be rewritten after chain start

# Seed amounts used to create a clear imbalance (safe with default dev funding).
IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION:-200000000000000000000000ogwei}" # denom rewritten after chain start
IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION:-100ogwei}"

POLL_SAMPLES="${POLL_SAMPLES:-25}"
POLL_SLEEP_SECS="${POLL_SLEEP_SECS:-2}"
# Always-on observability/runtime behavior for CLI usage.
STREAM_VALIDATOR_LOGS="true"
KEEP_RUNNING="true"
WATCH_COMPACT="${WATCH_COMPACT:-false}"

LOG_STREAM_PIDS=()
CURRENT_PHASE="init"
SETUP_STARTED="false"
POOL_DEL_ADDR=""
POOL_EVM_ADDR=""
RESOLVED_GOV_FROM=""
RESOLVED_GOV_HOME=""
WATCH_INITIAL_DELEGATIONS_LOGGED="false"
EXPANSION_MISSING_DSTS=()
EXPANSION_OBSERVED_DSTS_TEXT=""
EXPANSION_INITIAL_DELEGATED=()

on_interrupt() {
  echo
  echo "==> Interrupt received, stopping test setup..."
  # Stop child processes spawned by this script first.
  pkill -TERM -P "$$" >/dev/null 2>&1 || true
  cleanup_on_exit
  exit 130
}

cleanup_on_exit() {
  cleanup_log_streams
  if [[ "$SETUP_STARTED" == "true" ]]; then
    stop_nodes
  fi
}

usage() {
  cat <<EOF
Usage:
  $0 [options]
  $0 watch [options]
  $0 user_flow_multikey [options]
  $0 help

Runs an E2E test scenario for x/poolrebalancer:
- Bootstraps an isolated multi-validator test chain using multi_node_startup.sh
- Patches genesis staking + poolrebalancer non-delegator params
- Starts val0..valN, deploys CommunityPool, and sets pool delegator params at runtime
- Seeds scenario-specific CommunityPool deposit/stake state
- Polls pending queues so engineers can inspect behavior interactively

What gets patched before node start:
  staking.params:
    - unbonding_time=$STAKING_UNBONDING_TIME
    - max_entries=$STAKING_MAX_ENTRIES
  poolrebalancer.params (genesis):
    - max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS
    - rebalance_threshold_bp=$POOLREBALANCER_THRESHOLD_BP
    - max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK
    - max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP

Runtime setup after chain start:
  - deploy CommunityPool contract (unless POOL_CONTRACT_ADDR is provided)
  - set CommunityPool automationCaller to MODULE_EVM
  - set poolrebalancer.params.pool_delegator_address to CommunityPool bech32 account

Parameter precedence:
  1) Explicit environment variables (highest priority)
  2) Scenario defaults (applied only for knobs not explicitly set)
  3) Script baseline defaults

Manual caveats: low minStake → same-block stake() can zero the ledger after a credit (not a failed credit).
  Undelegation maturity is time-based. automationCaller=MODULE_EVM and pool_delegator_address=pool contract.

Commands:
  run (default)                     Full test setup + scenario execution
  watch                             Live monitor for an already running test chain
  user_flow_multikey                CommunityPool multi-account E2E (see block below); then polls RPCs and runs the script.
  help                              Show this help

user_flow_multikey — what it is for:
  Purpose:  End-to-end check of the CommunityPool Solidity contract from multiple EOAs (dev0, dev1, …),
            not the poolrebalancer scheduler. You verify deposit → withdraw queue → unbonding maturity →
            claimWithdraw, and optionally a separate claimRewards() pass after claimWithdraws.
  What runs:  For each configured dev account: bond approve + deposit; optional fractional withdraw();
            wall-clock wait for staking unbonding + on-chain maturity; claimWithdraw(uint256) per request;
            optional POST_CLAIMWITHDRAW claimRewards() on several users (tests reward accounting vs rewards
            folded into withdraw()).
  What you see:  Structured logs — pool aggregates (totalUnits, principalAssets, totalStaked,
            stakeablePrincipalLedger), per-user snapshots (native balance, bond ERC20, pool units) before/after
            each step, maturity waits, and native balance deltas when claimRewards runs.
  Optional stress: pass --stress-profile 100users (alias: stress100) to run an opt-in
            throughput profile (high user count + retry-oriented knobs) and print aggregate
            success/failure + retry metrics. This does not replace default USER_COUNT=5 runs.
  Prerequisites:  Validators already running; \$BASEDIR/dev_accounts.txt; pool wired
            (poolrebalancer.params.pool_delegator_address). This command waits until that param is set
            (or use POOL_CONTRACT_ADDR=0x… to skip the wait). Does not start nodes or run gov.

community_pool_edge_cases — what it is for:
  Purpose:  Scripted CommunityPool checks with explicit pass/fail (ACL reverts, reconcile recovery, withdraw math, …).
  Phases (run one at a time via positional arg, or combine with all / comma list):
    auth              Step 1 — non-owner reverts on privileged calls
    drift             Step 2 — owner skew syncTotalStaked; poll reconcile vs staking
    withdraw_sizing   Step 3 — one withdraw(); amountOut formula + pendingRebalanceUnbondReserve unchanged
    liquidity         Step 4 — claimWithdraw(requestId) reverts before maturity; optional matured-claim stress
    dust              Step 5 — dust deposit/withdraw rounding reverts + setConfig boundary/no-op stake checks
    rewards           Step 6 — multi-harvest + claimRewards sanity + reserve invariants
    all               Runs auth,drift,withdraw_sizing,liquidity,dust,rewards in one process (same as auth,drift,withdraw_sizing,liquidity,dust,rewards)
  Default:  No positional arg → COMMUNITY_POOL_EDGE_PHASES defaults to auth only (single case) unless you set COMMUNITY_POOL_EDGE_PHASES in the environment.
  Positional arg overrides COMMUNITY_POOL_EDGE_PHASES when present.
  What runs:  tests/e2e/poolrebalancer/community_pool_edge_cases.sh
  Signers:  AUTH_NON_OWNER_ACCOUNT (default dev1); POOL_OWNER_PK or dev0 for drift/rewards; WITHDRAW_SIZING_ACCOUNT (default dev2) for withdraw_sizing; LIQUIDITY_ACCOUNT (default WITHDRAW_SIZING_ACCOUNT) for liquidity; DUST_ACCOUNT/DUST_SECONDARY_ACCOUNT for dust; REWARDS_ACCOUNT for claimRewards.
  Tunables:  DRIFT_*, POOL_DEL_BECH32, WITHDRAW_SIZING_*, LIQUIDITY_*, CLAIM_STRESS_*, DUST_*, REWARDS_*, SKIP_EMPTY_POOL_HARVEST, …
  Prerequisites:  Same as user_flow_multikey; does not start nodes or run gov.
            withdraw_sizing/liquidity auto-deposit from dev accounts by default when units/stake are missing.
            Prefer happy_path/caps for deterministic user-withdraw checks.

CLI options:
  -n, --nodes <count>               Number of validators/nodes to run
  -s, --scenario <name>             Scenario name (same as SCENARIO env var)
  -p, --profile <name>              Demo profile: slow|medium|fast
  --stress-profile <name>           user_flow_multikey profile (100users|stress100)
  --user-count <n>                  user_flow_multikey USER_COUNT override
  --withdraw-users <n>              user_flow_multikey WITHDRAW_USERS override
  --flow-mode <name>                user_flow_multikey mode: serial|parallel
  --deposit-concurrency <n>         user_flow_multikey deposit worker concurrency
  --withdraw-concurrency <n>        user_flow_multikey withdraw submit worker concurrency
  --claim-concurrency <n>           user_flow_multikey claimWithdraw worker concurrency
  --claim-rewards-concurrency <n>   user_flow_multikey claimRewards worker concurrency
  --batch-delay-ms <n>              user_flow_multikey inter-batch delay milliseconds
  -h, --help                        Show this help

Scenarios:
  happy_path
    Goal: baseline rebalance scheduling from a heavily skewed delegation.
    Setup: contract-seeded skew (single-validator staking target + large deposit).
    Params: uses baseline defaults (unless overridden by environment).
    Watch for: pending redelegations to underweight validators.

  caps
    Goal: verify scheduling respects max_ops_per_block and max_move_per_op.
    Setup: same contract-seeded skew as happy_path, but with tight scheduling caps.
    Params: default poolrebalancer max_ops_per_block=1, max_move_per_op=1e18.
    Watch for: capped move sizes and slower progression.

  threshold_boundary
    Goal: verify tiny drift is ignored when threshold is high enough.
    Setup: seed a small contract deposit with single-validator target.
    Params: default poolrebalancer rebalance_threshold_bp=5000.
    Watch for: little or no scheduling when drift stays below threshold.

  expansion
    Goal: verify the target validator set can expand to bonded validators outside the initial seed set.
    Setup: five validators; pool stakes from contract across three validators only (main + two minor deposits).
    Params: max_target_validators=5, max_ops_per_block=1, max_move_per_op=1e19, larger minor seed amount.
    Watch for: redelegations with dst outside the initial three-validator delegation set (expansion_progress in logs).

Profiles:
  slow                              max_ops_per_block=1, capped move per op
  medium                            default balancing profile
  fast                              more ops per block, no move cap

Environment variables:
  BASEDIR                           Test chain base dir (default: $HOME/.og-evm-devnet)
  NODE_RPC                          RPC endpoint (default: tcp://127.0.0.1:26657)
  CHAIN_ID                          Chain ID (default: 10740)
  TX_FEES                           Fees for txs (default: $TX_FEES)
  CHAIN_HOME                        Home for governance tx signer (default: $CHAIN_HOME)

  VAL0_MNEMONIC ... VALN_MNEMONIC  Optional explicit mnemonics. Any missing values are auto-generated.

  POOLREBALANCER_MAX_TARGET_VALIDATORS
  SCENARIO                          happy_path|caps|threshold_boundary|expansion
                                    (aliases: baseline_3val, max_target_gt_bonded_3val,
                                    target_set_expansion_5val — normalized in apply_scenario_defaults)
  VALIDATOR_COUNT                   Validators to start (default 3; expansion defaults to 5 if unset)
  DEMO_PROFILE                      slow|medium|fast tuning for rebalance visibility (default: medium)
  POOLREBALANCER_THRESHOLD_BP
  POOLREBALANCER_MAX_OPS_PER_BLOCK
  POOLREBALANCER_MAX_MOVE_PER_OP
  POOL_DELEGATOR_MODE               contract|eoa (default: contract)
  POOL_OWNER_PK                     Private key for deploying/configuring CommunityPool
  POOL_DEPOSITOR_PK                 Private key for CommunityPool deposit txs during seeding
  MODULE_EVM                        Poolrebalancer module EVM address
  BOND_PRECOMPILE                   Bond precompile address
  EVM_RPC                           EVM RPC endpoint for cast calls (default: http://127.0.0.1:8545)
  POOL_CONTRACT_ADDR                Optional existing CommunityPool EVM address to reuse
  POOL_SEED_DEPOSIT_AMOUNT          Main contract seed amount (raw uint, default: 200000000000000000000)
  GOV_FROM                          Key name used for gov submit/vote (default: mykey)
  GOV_HOME                          Home for gov signer keyring (auto-detected if empty)
  GOV_DEPOSIT                       Gov proposal deposit (default: 10000000ogwei)
  GOV_FEES                          Fees for gov submit/vote txs (default: 400000000000000ogwei)
  GOV_WAIT_INITIAL                  Initial wait before checking proposal status (default: 20)
  GOV_POLL_TIMEOUT                  Timeout waiting for params propagation seconds (default: 20)
  GOV_STATUS_TIMEOUT                Timeout waiting proposal to pass (default: 120)

  USER_FLOW_POOL_DELEGATOR_POLL_INTERVAL_SECS  user_flow_multikey: seconds between checks when chain is up but address empty (default: 40)
  USER_FLOW_CHAIN_NOT_READY_POLL_INTERVAL_SECS  user_flow_multikey: poll while evmd/not-ready or query errors (default: 5)
  USER_FLOW_POOL_DELEGATOR_MAX_WAIT_SECS       user_flow_multikey: give up after this many seconds (default: 0 = no limit)

  COMMUNITY_POOL_EDGE_PHASES        community_pool_edge_cases: comma-separated phases (default: auth if unset; or pass positional arg / all)
  AUTH_NON_OWNER_ACCOUNT            community_pool_edge_cases: dev account name for non-owner txs (default: dev1)
  DRIFT_SKEW_WEI                     community_pool_edge_cases: added to totalStaked for drift test (default: 1e18 wei)
  DRIFT_RECOVER_MAX_WAIT_SECS        community_pool_edge_cases: wait for reconcile to match staking (default: 180)
  POOL_DEL_BECH32                    community_pool_edge_cases: optional pool delegator bech32 override
  WITHDRAW_SIZING_ACCOUNT            community_pool_edge_cases: dev account with LP units for withdraw test (default: dev2)
  WITHDRAW_SIZING_FRACTION_BP        community_pool_edge_cases: basis points of user units to withdraw (default: 1000 = 10%)
  WITHDRAW_SIZING_CANDIDATE_BP_LIST  ordered fallback BPs when withdraw reverts (default: 1000,500,200,100,50,20,10,5,1)
  WITHDRAW_SIZING_PENDING_RESERVE_POLL_SECS  optional wait for pendingRebalanceUnbondReserve>0 (default: 60; 0=skip)
  WITHDRAW_SIZING_GAS_LIMIT          gas limit for withdraw() tx (default: 9500000)
  WITHDRAW_SIZING_AUTO_DEPOSIT       if 1, auto approve+deposit when withdraw_sizing preconditions are missing (default: 1)
  WITHDRAW_SIZING_AUTO_DEPOSIT_USERS number of dev accounts to auto-deposit from (dev0..devN-1, default: 3)
  WITHDRAW_SIZING_AUTO_DEPOSIT_AMOUNT_WEI  auto-deposit amount per account (default: 100000000000000000000)
  WITHDRAW_SIZING_AUTO_DEPOSIT_INTERVAL_SECS  seconds between auto-deposit txs (default: 1)
  WITHDRAW_SIZING_TOTAL_STAKED_WAIT_SECS  wait after auto-deposit for totalStaked>0 (default: 120)
  LIQUIDITY_ACCOUNT                 liquidity phase: dev account for withdraw+claim flow (default: WITHDRAW_SIZING_ACCOUNT)
  LIQUIDITY_FRACTION_BP             liquidity phase: basis points of user units to withdraw (default: WITHDRAW_SIZING_FRACTION_BP)
  LIQUIDITY_CANDIDATE_BP_LIST       liquidity phase: ordered fallback BPs when withdraw reverts
  LIQUIDITY_GAS_LIMIT               liquidity phase: gas limit for withdraw()/claimWithdraw() txs
  LIQUIDITY_MATURITY_MAX_WAIT_SECS  liquidity phase: max wall-clock wait for maturity in optional stress (default: 300)
  CLAIM_STRESS_INSUFFICIENT_LIQUID  liquidity phase: if 1, best-effort retry matured claimWithdraw (default: 0)
  CLAIM_STRESS_MAX_ATTEMPTS         liquidity phase: retries after maturity in optional stress (default: 20)
  CLAIM_STRESS_POLL_INTERVAL_SECS   liquidity phase: seconds between optional stress retries (default: 2)
  DUST_ACCOUNT                      dust phase: primary dev account for seed deposit / withdraw(1) revert (default: dev1)
  DUST_SECONDARY_ACCOUNT            dust phase: secondary dev account for deposit(1) revert (default: dev2)
  DUST_SEED_DEPOSIT_AMOUNT_WEI      dust phase: seed deposit amount before no-op stake + dust reverts (default: 1e18)
  DUST_BOUNDARY_MAX_VALIDATORS      dust phase: valid boundary maxValidators used in setConfig (default: 1)
  DUST_HIGH_MIN_STAKE_AMOUNT_WEI    dust phase: elevated minStakeAmount used to force stake() no-op (default: uint256 max)
  REWARDS_ACCOUNT                   rewards phase: dev account that calls claimRewards() (default: dev1)
  REWARDS_HARVEST_COUNT             rewards phase: number of owner harvest() calls (default: 3)
  REWARDS_HARVEST_INTERVAL_SECS     rewards phase: sleep between harvests (default: 1)
  REWARDS_REQUIRE_HARVEST_SUCCESS   rewards phase: if 1, require harvest() to succeed; if 0, allow graceful skip (default: 0)
  SKIP_EMPTY_POOL_HARVEST           rewards phase: if 1, skip empty-pool harvest path with Forge pointer (default: 1)

  STAKING_UNBONDING_TIME            Reduce so pending queues mature quickly (default: 30s)
  STAKING_MAX_ENTRIES               Raise/lower redelegation entry pressure (default: 100)

  POLL_SAMPLES                      Initial poll samples before giving up if no pending ops (default: 25)
  POLL_SLEEP_SECS                   Seconds between samples (default: 2)

  IMBALANCE_MAIN_DELEGATION         Large delegation to validator[0]
  IMBALANCE_MINOR_DELEGATION        Small delegations to validator[1], validator[2]
  WATCH_COMPACT                     Compact lines for watch mode (default: false)

Note:
  Any variable set in the environment overrides scenario defaults when the script respects USER_SET_* flags.

Examples:
  # Standard rebalance flow
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario happy_path --nodes 3 --profile medium

  # Cap-focused behavior
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario caps --nodes 3 --profile slow

  # Threshold gating (expect no scheduling for small drift)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario threshold_boundary --nodes 3

  # Target-set expansion (5 validators; pool initially delegated to 3)
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh --scenario expansion --nodes 5 --profile medium

  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh watch

  # After happy_path (or any scenario) has started the chain and deployed the pool:
  bash tests/e2e/poolrebalancer/rebalance_scenario_runner.sh user_flow_multikey

EOF
}

# --- user_flow_multikey subcommand helpers (poll chain until pool is wired, then exec user_flow_multikey.sh) ---

# First Error:/rpc line from evmd stderr (avoids dumping full Usage after failures).
_user_flow_evmd_error_summary() {
  printf '%s\n' "$1" | awk '
    /^Error:/ { sub(/^Error:[[:space:]]*/, ""); print; exit }
    /^rpc error:/ { print; exit }
    NR==1 && length($0) { print }
  '
}

_user_flow_tendermint_latest_height() {
  curl -sS --max-time 2 "$(tendermint_status_url)" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // empty' 2>/dev/null || echo ""
}

# Poll evmd query poolrebalancer params until pool_delegator_address is non-empty (or timeout).
# Shorter interval while RPC returns "not ready" / no first block; longer once chain serves queries but gov not done.
wait_for_pool_delegator_address_configured() {
  local interval="${USER_FLOW_POOL_DELEGATOR_POLL_INTERVAL_SECS:-40}"
  local interval_chain_not_ready="${USER_FLOW_CHAIN_NOT_READY_POLL_INTERVAL_SECS:-5}"
  local max_wait="${USER_FLOW_POOL_DELEGATOR_MAX_WAIT_SECS:-0}"
  local start del qerr sleep_s err1 sync_h
  start="$(date +%s)"
  echo "==> Waiting for poolrebalancer.params.pool_delegator_address to be set (needed before user_flow_multikey.sh)"
  echo "    NODE_RPC=$NODE_RPC"
  echo "    After the chain is ready: poll every ${interval}s if the address is still empty"
  echo "    While evmd reports 'not ready' / no first block: poll every ${interval_chain_not_ready}s (USER_FLOW_CHAIN_NOT_READY_POLL_INTERVAL_SECS)"
  if [[ "$max_wait" =~ ^[0-9]+$ ]] && (( max_wait > 0 )); then
    echo "    Max wait ${max_wait}s (unset USER_FLOW_POOL_DELEGATOR_MAX_WAIT_SECS or set 0 for no limit)"
  else
    echo "    No max wait (interrupt with Ctrl+C); set USER_FLOW_POOL_DELEGATOR_MAX_WAIT_SECS to cap"
  fi
  while true; do
    del=""
    qerr=""
    sleep_s="$interval"
    sync_h="$(_user_flow_tendermint_latest_height)"
    if ! qerr="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json 2>&1)"; then
      err1="$(_user_flow_evmd_error_summary "$qerr")"
      if [[ "$qerr" == *"not ready"* ]] || [[ "$qerr" == *"first block"* ]] || [[ "$qerr" == *"invalid height"* ]]; then
        echo "==> ($(date -u +%Y-%m-%dT%H:%M:%SZ)) still waiting: evmd app not ready yet (ABCI queries blocked until the first block is committed)"
        echo "    tendermint latest_block_height=${sync_h:-unknown} — start or wait for validators, then this will clear"
        echo "    evmd: ${err1:0:240}"
        sleep_s="$interval_chain_not_ready"
      else
        echo "==> ($(date -u +%Y-%m-%dT%H:%M:%SZ)) still waiting: poolrebalancer query failed"
        echo "    tendermint latest_block_height=${sync_h:-unknown}"
        echo "    evmd: ${err1:0:240}"
        sleep_s="$interval_chain_not_ready"
      fi
    else
      del="$(printf '%s\n' "$qerr" | jq -r '.params.pool_delegator_address // empty' 2>/dev/null || echo "")"
      if [[ -n "$del" && "$del" != "null" ]]; then
        echo "==> pool_delegator_address is set: $del"
        return 0
      fi
      echo "==> ($(date -u +%Y-%m-%dT%H:%M:%SZ)) still waiting: pool_delegator_address is empty (chain is up — finish CommunityPool deploy + gov pool_delegator_address update)"
      sleep_s="$interval"
    fi
    if [[ "$max_wait" =~ ^[0-9]+$ ]] && (( max_wait > 0 )); then
      if (( $(date +%s) - start >= max_wait )); then
        echo "error: timed out after ${max_wait}s waiting for pool_delegator_address" >&2
        return 1
      fi
    fi
    sleep "$sleep_s"
  done
}

# Preconditions: BASEDIR/dev_accounts.txt; chain up. Sets CHAIN_HOME=val0 for bech32 debug. Optional POOL_CONTRACT_ADDR skips wait.
run_user_flow_multikey_subcommand() {
  local script="$ROOT_DIR/tests/e2e/poolrebalancer/user_flow_multikey.sh"
  if [[ ! -f "$script" ]]; then
    echo "error: missing $script" >&2
    exit 1
  fi
  if [[ ! -f "$BASEDIR/dev_accounts.txt" ]]; then
    echo "error: missing $BASEDIR/dev_accounts.txt" >&2
    echo "hint: start a devnet with this runner (or multi_node_startup) so dev accounts exist" >&2
    exit 1
  fi
  # Runner defaults CHAIN_HOME to BASEDIR (repo root home); user_flow_multikey.sh expects val0 for evmd debug addr.
  if [[ -z "${CHAIN_HOME:-}" ]] || [[ "${CHAIN_HOME}" == "${BASEDIR}" ]]; then
    export CHAIN_HOME="$BASEDIR/val0"
  fi
  echo "==> user_flow_multikey: BASEDIR=$BASEDIR CHAIN_HOME=$CHAIN_HOME NODE_RPC=$NODE_RPC"
  if [[ -n "${POOL_CONTRACT_ADDR:-}" ]]; then
    echo "==> POOL_CONTRACT_ADDR is set; skipping wait for poolrebalancer.params.pool_delegator_address"
  else
    wait_for_pool_delegator_address_configured || exit 1
  fi
  ensure_evm_rpc_ready || exit 1
  echo "==> EVM_RPC=$EVM_RPC — invoking user_flow_multikey.sh"
  if [[ -n "${PARSED_USER_FLOW_STRESS_PROFILE:-}" ]]; then
    export USER_FLOW_STRESS_PROFILE="$PARSED_USER_FLOW_STRESS_PROFILE"
    echo "==> USER_FLOW_STRESS_PROFILE=$USER_FLOW_STRESS_PROFILE (from subcommand)"
  fi
  if [[ -n "${PARSED_USER_FLOW_USER_COUNT:-}" ]]; then
    export USER_COUNT="$PARSED_USER_FLOW_USER_COUNT"
    echo "==> USER_COUNT=$USER_COUNT (from subcommand)"
  fi
  if [[ -n "${PARSED_USER_FLOW_WITHDRAW_USERS:-}" ]]; then
    export WITHDRAW_USERS="$PARSED_USER_FLOW_WITHDRAW_USERS"
    echo "==> WITHDRAW_USERS=$WITHDRAW_USERS (from subcommand)"
  fi
  if [[ -n "${PARSED_USER_FLOW_MODE:-}" ]]; then
    export USER_FLOW_MODE="$PARSED_USER_FLOW_MODE"
    echo "==> USER_FLOW_MODE=$USER_FLOW_MODE (from subcommand)"
  fi
  if [[ -n "${PARSED_DEPOSIT_CONCURRENCY:-}" ]]; then
    export DEPOSIT_CONCURRENCY="$PARSED_DEPOSIT_CONCURRENCY"
    echo "==> DEPOSIT_CONCURRENCY=$DEPOSIT_CONCURRENCY (from subcommand)"
  fi
  if [[ -n "${PARSED_WITHDRAW_CONCURRENCY:-}" ]]; then
    export WITHDRAW_CONCURRENCY="$PARSED_WITHDRAW_CONCURRENCY"
    echo "==> WITHDRAW_CONCURRENCY=$WITHDRAW_CONCURRENCY (from subcommand)"
  fi
  if [[ -n "${PARSED_CLAIM_CONCURRENCY:-}" ]]; then
    export CLAIM_CONCURRENCY="$PARSED_CLAIM_CONCURRENCY"
    echo "==> CLAIM_CONCURRENCY=$CLAIM_CONCURRENCY (from subcommand)"
  fi
  if [[ -n "${PARSED_CLAIM_REWARDS_CONCURRENCY:-}" ]]; then
    export CLAIM_REWARDS_CONCURRENCY="$PARSED_CLAIM_REWARDS_CONCURRENCY"
    echo "==> CLAIM_REWARDS_CONCURRENCY=$CLAIM_REWARDS_CONCURRENCY (from subcommand)"
  fi
  if [[ -n "${PARSED_BATCH_DELAY_MS:-}" ]]; then
    export BATCH_DELAY_MS="$PARSED_BATCH_DELAY_MS"
    echo "==> BATCH_DELAY_MS=$BATCH_DELAY_MS (from subcommand)"
  fi
  bash "$script"
}

user_flow_chain_ready() {
  local status_url h
  status_url="$(tendermint_status_url)"
  h="$(curl -sS --max-time 1 "$status_url" 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"' 2>/dev/null || echo "0")"
  [[ "$h" =~ ^[0-9]+$ ]] || h=0
  (( h > 0 ))
}

user_flow_pool_delegator_ready() {
  local out del
  out="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json 2>/dev/null || true)"
  del="$(echo "$out" | jq -r '.params.pool_delegator_address // empty' 2>/dev/null || true)"
  [[ -n "$del" ]]
}

# Preconditions: BASEDIR/dev_accounts.txt; chain up. Optional POOL_CONTRACT_ADDR skips wait.
run_community_pool_edge_cases_subcommand() {
  local script="$ROOT_DIR/tests/e2e/poolrebalancer/community_pool_edge_cases.sh"
  if [[ ! -f "$script" ]]; then
    echo "error: missing $script" >&2
    exit 1
  fi
  if [[ ! -f "$BASEDIR/dev_accounts.txt" ]]; then
    echo "error: missing $BASEDIR/dev_accounts.txt" >&2
    echo "hint: start a devnet with this runner (or multi_node_startup) so dev accounts exist" >&2
    exit 1
  fi
  if [[ -z "${CHAIN_HOME:-}" ]] || [[ "${CHAIN_HOME}" == "${BASEDIR}" ]]; then
    export CHAIN_HOME="$BASEDIR/val0"
  fi
  echo "==> community_pool_edge_cases: BASEDIR=$BASEDIR CHAIN_HOME=$CHAIN_HOME NODE_RPC=$NODE_RPC"
  if [[ -n "${POOL_CONTRACT_ADDR:-}" ]]; then
    echo "==> POOL_CONTRACT_ADDR is set; skipping wait for poolrebalancer.params.pool_delegator_address"
  else
    wait_for_pool_delegator_address_configured || exit 1
  fi
  ensure_evm_rpc_ready || exit 1
  echo "==> EVM_RPC=$EVM_RPC — invoking community_pool_edge_cases.sh"
  if [[ -n "${PARSED_COMMUNITY_POOL_EDGE_PHASES:-}" ]]; then
    if [[ "$PARSED_COMMUNITY_POOL_EDGE_PHASES" == "all" ]]; then
      export COMMUNITY_POOL_EDGE_PHASES="auth,drift,withdraw_sizing,liquidity,dust,rewards"
    else
      export COMMUNITY_POOL_EDGE_PHASES="$PARSED_COMMUNITY_POOL_EDGE_PHASES"
    fi
    echo "==> COMMUNITY_POOL_EDGE_PHASES=$COMMUNITY_POOL_EDGE_PHASES (from subcommand)"
  fi
  bash "$script"
}

parse_cli_args() {
  local subcommand=""
  PARSED_COMMUNITY_POOL_EDGE_PHASES=""
  PARSED_USER_FLOW_STRESS_PROFILE=""
  PARSED_USER_FLOW_USER_COUNT=""
  PARSED_USER_FLOW_WITHDRAW_USERS=""
  PARSED_USER_FLOW_MODE=""
  PARSED_DEPOSIT_CONCURRENCY=""
  PARSED_WITHDRAW_CONCURRENCY=""
  PARSED_CLAIM_CONCURRENCY=""
  PARSED_CLAIM_REWARDS_CONCURRENCY=""
  PARSED_BATCH_DELAY_MS=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      watch)
        subcommand="watch"
        shift
        ;;
      help)
        subcommand="help"
        shift
        ;;
      user_flow_multikey)
        subcommand="user_flow_multikey"
        shift
        ;;
      community_pool_edge_cases)
        subcommand="community_pool_edge_cases"
        shift
        ;;
      -n|--nodes)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        VALIDATOR_COUNT="$2"
        shift 2
        ;;
      -s|--scenario)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        SCENARIO="$2"
        shift 2
        ;;
      -p|--profile)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        DEMO_PROFILE="$2"
        shift 2
        ;;
      -h|--help)
        subcommand="help"
        shift
        ;;
      --stress-profile)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        PARSED_USER_FLOW_STRESS_PROFILE="$2"
        shift 2
        ;;
      --user-count)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]] || (( "$2" < 1 )); then
          echo "error: --user-count must be a positive integer (got: $2)" >&2
          exit 1
        fi
        PARSED_USER_FLOW_USER_COUNT="$2"
        shift 2
        ;;
      --withdraw-users)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]]; then
          echo "error: --withdraw-users must be a non-negative integer (got: $2)" >&2
          exit 1
        fi
        PARSED_USER_FLOW_WITHDRAW_USERS="$2"
        shift 2
        ;;
      --flow-mode)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ "$2" != "serial" && "$2" != "parallel" ]]; then
          echo "error: --flow-mode must be serial or parallel (got: $2)" >&2
          exit 1
        fi
        PARSED_USER_FLOW_MODE="$2"
        shift 2
        ;;
      --deposit-concurrency)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]] || (( "$2" < 1 )); then
          echo "error: --deposit-concurrency must be a positive integer (got: $2)" >&2
          exit 1
        fi
        PARSED_DEPOSIT_CONCURRENCY="$2"
        shift 2
        ;;
      --withdraw-concurrency)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]] || (( "$2" < 1 )); then
          echo "error: --withdraw-concurrency must be a positive integer (got: $2)" >&2
          exit 1
        fi
        PARSED_WITHDRAW_CONCURRENCY="$2"
        shift 2
        ;;
      --claim-concurrency)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]] || (( "$2" < 1 )); then
          echo "error: --claim-concurrency must be a positive integer (got: $2)" >&2
          exit 1
        fi
        PARSED_CLAIM_CONCURRENCY="$2"
        shift 2
        ;;
      --claim-rewards-concurrency)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]] || (( "$2" < 1 )); then
          echo "error: --claim-rewards-concurrency must be a positive integer (got: $2)" >&2
          exit 1
        fi
        PARSED_CLAIM_REWARDS_CONCURRENCY="$2"
        shift 2
        ;;
      --batch-delay-ms)
        if [[ $# -lt 2 ]]; then
          echo "missing value for $1" >&2
          exit 1
        fi
        if [[ ! "$2" =~ ^[0-9]+$ ]]; then
          echo "error: --batch-delay-ms must be a non-negative integer (got: $2)" >&2
          exit 1
        fi
        PARSED_BATCH_DELAY_MS="$2"
        shift 2
        ;;
      --)
        shift
        break
        ;;
      run)
        # Explicit no-op command for readability.
        shift
        ;;
      *)
        if [[ "$subcommand" == "community_pool_edge_cases" && -z "${PARSED_COMMUNITY_POOL_EDGE_PHASES:-}" ]]; then
          PARSED_COMMUNITY_POOL_EDGE_PHASES="$1"
          shift
        else
          echo "unknown argument: $1" >&2
          return 1
        fi
        ;;
    esac
  done
  PARSED_SUBCOMMAND="$subcommand"
  return 0
}

require_bin() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing dependency: $1" >&2; exit 1; }
}

stop_nodes() {
  # Aggressive cleanup: multi_node_startup.sh launches `evmd start` processes directly.
  pkill -f "evmd start" >/dev/null 2>&1 || true
  pkill -f "multi_node_startup.sh" >/dev/null 2>&1 || true
  # Give the OS a moment to release RPC/P2P ports.
  sleep 1
}

cleanup_log_streams() {
  if (( ${#LOG_STREAM_PIDS[@]} == 0 )); then
    return 0
  fi
  for pid in "${LOG_STREAM_PIDS[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  LOG_STREAM_PIDS=()
}

start_validator_log_streams() {
  mkdir -p "$BASEDIR/logs"
  for v in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    local f="$BASEDIR/logs/val${v}.log"
    touch "$f"
    tail -n 0 -F "$f" | sed -u "s/^/[val${v}] /" &
    LOG_STREAM_PIDS+=("$!")
  done
}

wait_for_height() {
  local timeout_secs="${1:-30}"
  local start status_url
  status_url="$(tendermint_status_url)"
  start="$(date +%s)"
  while true; do
    local h
    h="$(curl -sS --max-time 1 "$status_url" 2>/dev/null | jq -r '.result.sync_info.latest_block_height' 2>/dev/null || echo 0)"
    if [[ "$h" != "0" ]]; then
      echo "$h"
      return 0
    fi
    if (( $(date +%s) - start > timeout_secs )); then
      echo "timed out waiting for height > 0" >&2
      return 1
    fi
    sleep 1
  done
}

tendermint_status_url() {
  local hp="${NODE_RPC#tcp://}"
  hp="${hp#http://}"
  hp="${hp#https://}"
  printf 'http://%s/status' "$hp"
}

derive_evm_rpc_from_node_rpc() {
  local node="$1"
  local hostport host port idx jsonrpc_port
  hostport="${node#tcp://}"
  hostport="${hostport#http://}"
  hostport="${hostport#https://}"
  host="${hostport%%:*}"
  port="${hostport##*:}"
  if [[ "$port" =~ ^[0-9]+$ ]] && (( port >= 26657 )) && (( (port - 26657) % 100 == 0 )); then
    idx=$(( (port - 26657) / 100 ))
    jsonrpc_port=$((8545 + (idx * 10)))
    printf 'http://%s:%s' "$host" "$jsonrpc_port"
    return 0
  fi
  return 1
}

ensure_evm_rpc_ready() {
  CURRENT_PHASE="ensure_evm_rpc_ready"
  local candidates=()
  local derived=""
  local c start now

  candidates+=("$EVM_RPC")
  if derived="$(derive_evm_rpc_from_node_rpc "$NODE_RPC" 2>/dev/null || true)"; then
    candidates+=("$derived")
  fi
  candidates+=("http://127.0.0.1:8545" "http://127.0.0.1:8555" "http://127.0.0.1:8565" "http://127.0.0.1:8575")

  echo "==> Waiting for EVM RPC readiness"
  start="$(date +%s)"
  while true; do
    for c in "${candidates[@]}"; do
      [[ -z "$c" ]] && continue
      if cast chain-id --rpc-url "$c" >/dev/null 2>&1; then
        if [[ "$EVM_RPC" != "$c" ]]; then
          echo "==> Using detected EVM RPC endpoint: $c"
        fi
        EVM_RPC="$c"
        return 0
      fi
    done
    now="$(date +%s)"
    if (( now - start > 90 )); then
      echo "error: no reachable EVM RPC endpoint found after 90s" >&2
      echo "tried: ${candidates[*]}" >&2
      return 1
    fi
    sleep 2
  done
}

dev_account_private_key_from_file() {
  local account_name="$1"
  local f="$BASEDIR/dev_accounts.txt"
  [[ -f "$f" ]] || return 1
  awk -v name="$account_name" '
    $0 ~ ("^" name ":") {in_block=1; next}
    in_block && $1=="private_key:" {print $2; exit}
    in_block && /^[^[:space:]]/ {in_block=0}
  ' "$f"
}

resolve_pool_runtime_keys() {
  if [[ "$POOL_DELEGATOR_MODE" != "contract" ]]; then
    return 0
  fi
  local dev0_pk dev1_pk
  if [[ "$POOL_OWNER_PK" == "$DEFAULT_POOL_OWNER_PK" ]]; then
    dev0_pk="$(dev_account_private_key_from_file "dev0" || true)"
    if [[ -n "$dev0_pk" ]]; then
      POOL_OWNER_PK="$dev0_pk"
      echo "==> Using generated dev0 private key as POOL_OWNER_PK"
    fi
  fi
  if [[ "$POOL_DEPOSITOR_PK" == "$DEFAULT_POOL_DEPOSITOR_PK" ]]; then
    dev1_pk="$(dev_account_private_key_from_file "dev1" || true)"
    if [[ -n "$dev1_pk" ]]; then
      POOL_DEPOSITOR_PK="$dev1_pk"
      echo "==> Using generated dev1 private key as POOL_DEPOSITOR_PK"
    fi
  fi
}

resolve_governance_signer() {
  CURRENT_PHASE="resolve_governance_signer"
  local requested_home candidate_from candidate_home
  requested_home="${GOV_HOME:-$CHAIN_HOME}"

  # 1) honor configured GOV_FROM when available
  if evmd keys show "$GOV_FROM" --keyring-backend "$KEYRING" --home "$requested_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$GOV_FROM"
    RESOLVED_GOV_HOME="$requested_home"
    echo "==> Using configured governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  # 2) fallback to val0 in validator 0 home (multi_node_startup default key naming)
  candidate_from="val0"
  candidate_home="$HOME0"
  if evmd keys show "$candidate_from" --keyring-backend "$KEYRING" --home "$candidate_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$candidate_from"
    RESOLVED_GOV_HOME="$candidate_home"
    echo "==> Falling back to governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  # 3) fallback to mykey in CHAIN_HOME for local_node-style setups
  candidate_from="mykey"
  candidate_home="$CHAIN_HOME"
  if evmd keys show "$candidate_from" --keyring-backend "$KEYRING" --home "$candidate_home" >/dev/null 2>&1; then
    RESOLVED_GOV_FROM="$candidate_from"
    RESOLVED_GOV_HOME="$candidate_home"
    echo "==> Falling back to governance signer: from=$RESOLVED_GOV_FROM home=$RESOLVED_GOV_HOME"
    return 0
  fi

  echo "error: could not resolve a governance signer key for submit/vote" >&2
  echo "tried: GOV_FROM=$GOV_FROM home=$requested_home, val0@$HOME0, mykey@$CHAIN_HOME" >&2
  return 1
}

vote_proposal_with_validator_majority() {
  local proposal_id="$1"
  local success_votes=0
  local required_votes=$((VALIDATOR_COUNT / 2 + 1))
  local i from home vote_out vote_code vote_log

  echo "==> Voting proposal_id=$proposal_id with validator keys (target yes votes: $required_votes/$VALIDATOR_COUNT)"
  for i in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    from="val${i}"
    home="$BASEDIR/val${i}"
    if ! evmd keys show "$from" --keyring-backend "$KEYRING" --home "$home" >/dev/null 2>&1; then
      continue
    fi
    vote_out="$(evmd tx gov vote "$proposal_id" yes \
      --from "$from" --keyring-backend "$KEYRING" --home "$home" \
      --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
      --fees "$GOV_FEES" --gas auto --gas-adjustment 1.3 -y -o json 2>/dev/null || true)"
    vote_code="$(echo "$vote_out" | jq -r '.code // 0' 2>/dev/null || echo 1)"
    if [[ "$vote_code" == "0" ]]; then
      success_votes=$((success_votes + 1))
    else
      vote_log="$(echo "$vote_out" | jq -r '.raw_log // .log // empty' 2>/dev/null || true)"
      echo "warning: vote from $from failed (code=$vote_code): $vote_log" >&2
    fi
  done

  # Fallback vote path using resolved signer (for local_node-style key layouts).
  if (( success_votes < required_votes )); then
    vote_out="$(evmd tx gov vote "$proposal_id" yes \
      --from "$RESOLVED_GOV_FROM" --keyring-backend "$KEYRING" --home "$RESOLVED_GOV_HOME" \
      --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
      --fees "$GOV_FEES" --gas auto --gas-adjustment 1.3 -y -o json 2>/dev/null || true)"
    vote_code="$(echo "$vote_out" | jq -r '.code // 0' 2>/dev/null || echo 1)"
    if [[ "$vote_code" == "0" ]]; then
      success_votes=$((success_votes + 1))
    fi
  fi

  echo "governance_votes_submitted=$success_votes"
  if (( success_votes < required_votes )); then
    echo "error: insufficient successful governance votes submitted ($success_votes/$required_votes required)" >&2
    return 1
  fi
  return 0
}

auto_generate_validator_mnemonic() {
  local idx="$1"
  local tmp_home
  local key_name
  local out
  local mnemonic

  tmp_home="$(mktemp -d "${TMPDIR:-/tmp}/poolrebalancer-mnemonic-${idx}-XXXXXX")"
  key_name="autoval${idx}"
  out="$(evmd keys add "$key_name" --keyring-backend test --algo eth_secp256k1 --home "$tmp_home" 2>&1)"
  mnemonic="$(echo "$out" | awk 'NF{line=$0} END{print line}')"
  rm -rf "$tmp_home"

  if [[ -z "$mnemonic" ]]; then
    echo "failed to auto-generate mnemonic for validator $idx" >&2
    return 1
  fi
  echo "$mnemonic"
}

resolve_mnemonics() {
  local missing=()
  local need="$VALIDATOR_COUNT"

  for i in $(seq 0 $((need - 1))); do
    local name="VAL${i}_MNEMONIC"
    local current="${!name:-}"
    if [[ -z "$current" ]]; then
      current="$(auto_generate_validator_mnemonic "$i" || true)"
      if [[ -n "$current" ]]; then
        export "$name=$current"
      fi
    fi
    if [[ -z "${!name:-}" ]]; then
      missing+=("$name")
    fi
  done

  if (( ${#missing[@]} > 0 )); then
    echo "missing required mnemonics: ${missing[*]}" >&2
    echo "set them in env or ensure validator mnemonic generation is available" >&2
    exit 1
  fi
}

patch_genesis_poolrebalancer_params() {
  local gen0="$BASEDIR/val0/config/genesis.json"
  local tmp="$BASEDIR/val0/config/genesis.tmp.json"

  jq --argjson maxTargets "$POOLREBALANCER_MAX_TARGET_VALIDATORS" \
     --argjson thr "$POOLREBALANCER_THRESHOLD_BP" \
     --argjson maxOps "$POOLREBALANCER_MAX_OPS_PER_BLOCK" \
     --arg maxMove "$POOLREBALANCER_MAX_MOVE_PER_OP" \
     ' .app_state.poolrebalancer.params.max_target_validators = $maxTargets
       | .app_state.poolrebalancer.params.rebalance_threshold_bp = $thr
       | .app_state.poolrebalancer.params.max_ops_per_block = $maxOps
       | .app_state.poolrebalancer.params.max_move_per_op = $maxMove
     ' "$gen0" > "$tmp"

  mv "$tmp" "$gen0"
  for v in $(seq 1 $((VALIDATOR_COUNT - 1))); do
    cp "$gen0" "$BASEDIR/val${v}/config/genesis.json"
  done

  evmd genesis validate-genesis --home "$BASEDIR/val0" >/dev/null
}

patch_genesis_staking_params() {
  local gen0="$BASEDIR/val0/config/genesis.json"
  local tmp="$BASEDIR/val0/config/genesis.tmp.json"

  jq --arg unbond "$STAKING_UNBONDING_TIME" \
     --argjson maxEntries "$STAKING_MAX_ENTRIES" \
     ' .app_state.staking.params.unbonding_time = $unbond
       | .app_state.staking.params.max_entries = $maxEntries
     ' "$gen0" > "$tmp"

  mv "$tmp" "$gen0"
}

evmd_debug_addr() {
  local addr="$1"
  if [[ -n "${CHAIN_HOME:-}" && -d "$CHAIN_HOME" ]]; then
    evmd debug addr "$addr" --home "$CHAIN_HOME" 2>/dev/null || evmd debug addr "$addr" 2>/dev/null || true
  else
    evmd debug addr "$addr" 2>/dev/null || true
  fi
}

resolve_evm_hex_from_bech32() {
  local bech="$1"
  local dbg hex
  dbg="$(evmd_debug_addr "$bech")"
  hex="$(printf '%s\n' "$dbg" | awk '{for(i=1;i<=NF;i++){if($i ~ /^0x[0-9a-fA-F]{40}$/){print $i; exit}}}')"
  if [[ -z "$hex" ]]; then
    # Fallback: derive from bech32 bytes using evmd debug output fields.
    hex="$(printf '%s\n' "$dbg" | awk -F': ' '/Address \(hex\)/{print $2; exit}')"
  fi
  if [[ "$hex" =~ ^[0-9a-fA-F]{40}$ ]]; then
    hex="0x$hex"
  fi
  printf '%s' "$hex"
}

pool_addr_from_cast_deploy_output() {
  local raw="$1"
  local addr=""
  if addr="$(printf '%s' "$raw" | jq -r '.contractAddress // .receipt.contractAddress // empty' 2>/dev/null)" &&
    [[ -n "$addr" && "$addr" != "null" ]]; then
    printf '%s' "$addr"
    return 0
  fi
  if [[ "$raw" =~ \"contractAddress\"[[:space:]]*:[[:space:]]*\"(0x[0-9a-fA-F]{40})\" ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

resolve_pool_bech32() {
  if [[ -z "$POOL_EVM_ADDR" ]]; then
    echo "error: resolve_pool_bech32 requires POOL_EVM_ADDR" >&2
    exit 1
  fi
  POOL_DEL_ADDR="$(evmd_debug_addr "$POOL_EVM_ADDR" | awk '/Bech32 Acc/{print $3; exit}')"
  if [[ -z "$POOL_DEL_ADDR" ]]; then
    echo "error: could not resolve bech32 address for pool contract: $POOL_EVM_ADDR" >&2
    exit 1
  fi
}

normalize_cast_uint256_output() {
  local s="${1:-}"
  s="${s//$'\r'/}"
  s="${s%%$'\n'*}"
  s="${s%% *}"
  s="${s//$'\t'/}"
  [[ "$s" =~ ^[0-9]+$ ]] && { printf '%s' "$s"; return 0; }
  # cast call usually returns hex (e.g. 0x000...001); decode so minStake / ledger reads match chain.
  if [[ "$s" =~ ^0x[0-9a-fA-F]+$ ]] && command -v python3 >/dev/null 2>&1; then
    python3 -c "print(int('$s',16))" 2>/dev/null && return 0
  fi
  return 1
}

pool_call_uint256() {
  local sig="$1"
  local raw norm
  [[ -z "${POOL_EVM_ADDR:-}" ]] && { printf 'n/a'; return 0; }
  raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "$sig" 2>/dev/null || true)"
  if norm="$(normalize_cast_uint256_output "$raw")"; then
    printf '%s' "$norm"
  else
    printf 'n/a'
  fi
}

wait_evm_nonce_settled_for_pk() {
  local pk="$1"
  local deadline_sec="${2:-45}"
  local addr pending latest t0
  addr="$(cast wallet address --private-key "$pk")"
  t0="$(date +%s)"
  while true; do
    pending="$(cast nonce --rpc-url "$EVM_RPC" --block pending "$addr" 2>/dev/null || true)"
    latest="$(cast nonce --rpc-url "$EVM_RPC" --block latest "$addr" 2>/dev/null || true)"
    [[ -z "$pending" || -z "$latest" ]] && return 0
    [[ "$pending" == "$latest" ]] && return 0
    if (( $(date +%s) - t0 > deadline_sec )); then
      return 0
    fi
    sleep 1
  done
}

_bumped_gas_price() {
  local gp gp2
  gp="$(cast gas-price --rpc-url "$EVM_RPC" 2>/dev/null || echo 1000000)"
  gp2="$(awk -v g="$gp" 'BEGIN { print int(g) * 2 }' 2>/dev/null || true)"
  [[ -z "$gp2" || "$gp2" == "0" ]] && gp2="$gp"
  printf '%s' "$gp2"
}

deposit_to_pool_once() {
  local amount="$1"
  local approve_json="${2:-/tmp/pool_seed_approve.json}"
  local deposit_json="${3:-/tmp/pool_seed_deposit.json}"
  local errf gp2
  wait_evm_nonce_settled_for_pk "$POOL_DEPOSITOR_PK" 45
  errf="$(mktemp -t pool_dep.XXXXXX)"
  if cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_EVM_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" "$POOL_EVM_ADDR" \
      "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"; then
    rm -f "$errf"
    return 0
  fi
  gp2="$(_bumped_gas_price)"
  cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" --gas-price "$gp2" "$BOND_PRECOMPILE" \
    "approve(address,uint256)" "$POOL_EVM_ADDR" "$amount" >"$approve_json" 2>"$errf" \
    && cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_DEPOSITOR_PK" --gas-price "$gp2" "$POOL_EVM_ADDR" \
      "deposit(uint256)" "$amount" >"$deposit_json" 2>"$errf"
  local st=$?
  rm -f "$errf"
  return "$st"
}

coin_amount_to_uint() {
  local coin="$1"
  local amount
  amount="$(printf '%s' "$coin" | sed -E 's/^([0-9]+).*/\1/')"
  if [[ -z "$amount" || ! "$amount" =~ ^[0-9]+$ ]]; then
    echo "error: could not parse numeric amount from coin string: $coin" >&2
    return 1
  fi
  printf '%s' "$amount"
}

# Printed before deploy + wiring so every scenario run explains pool setup, timing, and active knobs.
log_pool_contract_setup_banner() {
  echo
  echo "──────────────────────────────────────────────────────────────────────────────"
  echo "CommunityPool setup  (scenario=$SCENARIO)"
  echo "  What happens next: deploy or reuse contract → set automationCaller (EVM tx) → governance"
  echo "    updates poolrebalancer.pool_delegator_address to this pool’s bech32 account (vote + propagate)."
  echo "  Deploy timing: fresh deploy is ONE cast create tx. Expect ~15–90s wall-clock on a local devnet:"
  echo "    blocks must include the tx; nonce/gas contention can add retries. Reusing POOL_CONTRACT_ADDR skips deploy."
  echo "  Gov wiring timing: proposal + majority votes + param propagation (often ~1–3 min; cap $GOV_STATUS_TIMEOUT s)."
  echo "  Typical local devnet: pool_delegator_address usually shows up around block heights ~30–32 (varies with block time / votes)."
  echo "  Params already chosen for this run (genesis + scenario):"
  echo "    poolrebalancer: max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS  threshold_bp=$POOLREBALANCER_THRESHOLD_BP"
  echo "                    max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK  max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP"
  echo "    staking:        unbonding_time=$STAKING_UNBONDING_TIME  max_entries=$STAKING_MAX_ENTRIES"
  echo "    validators:     VALIDATOR_COUNT=$VALIDATOR_COUNT  EVM_RPC=$EVM_RPC"
  echo "──────────────────────────────────────────────────────────────────────────────"
  echo
}

deploy_pool_contract_if_needed() {
  CURRENT_PHASE="deploy_pool_contract"
  if [[ "$POOL_DELEGATOR_MODE" != "contract" ]]; then
    echo "error: unsupported POOL_DELEGATOR_MODE=$POOL_DELEGATOR_MODE (phase1 supports contract only)" >&2
    exit 1
  fi
  if [[ -n "$POOL_CONTRACT_ADDR" ]]; then
    POOL_EVM_ADDR="$POOL_CONTRACT_ADDR"
    echo "==> Reusing existing CommunityPool (POOL_CONTRACT_ADDR=$POOL_CONTRACT_ADDR) — no deploy tx; address ready immediately."
  else
    echo "==> Deploying CommunityPool contract (contract creation via cast send --create)"
    echo "    Typical wait: one mined block + any gas-price retry; if this hangs, check validator logs and EVM RPC reachability."
  local owner bytecode ctor_args data deploy_raw deploy_err owner_balance gp2
    if ! cast chain-id --rpc-url "$EVM_RPC" >/dev/null 2>&1; then
      echo "error: EVM RPC is not reachable at $EVM_RPC (cast chain-id failed)" >&2
      exit 1
    fi
    owner="$(cast wallet address --private-key "$POOL_OWNER_PK")"
    owner_balance="$(cast balance --rpc-url "$EVM_RPC" "$owner" 2>/dev/null || echo "0")"
    if [[ "$owner_balance" =~ ^[0-9]+$ ]] && [[ "$owner_balance" == "0" ]]; then
      echo "warning: pool owner address $owner has zero EVM balance on $EVM_RPC; deploy may fail" >&2
    fi
    bytecode="$(jq -r '.bytecode // empty' "$ROOT_DIR/contracts/solidity/pool/CommunityPool.json" 2>/dev/null || true)"
    if [[ -z "$bytecode" || "$bytecode" == "null" ]]; then
      echo "error: missing CommunityPool bytecode in contracts/solidity/pool/CommunityPool.json" >&2
      exit 1
    fi
    echo "==> Constructor minStakeAmount=1"
    ctor_args="$(cast abi-encode "constructor(address,uint32,uint32,uint256,address)" "$BOND_PRECOMPILE" 10 5 1 "$owner")"
    data="${bytecode}${ctor_args#0x}"
    deploy_err="$(mktemp -t pool_deploy_err.XXXXXX)"
    deploy_raw="$(cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" --create "$data" 2>"$deploy_err" || true)"
    if [[ -z "$deploy_raw" ]]; then
      gp2="$(_bumped_gas_price)"
      deploy_raw="$(cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" --gas-price "$gp2" --create "$data" 2>"$deploy_err" || true)"
    fi
    if [[ -z "$deploy_raw" ]]; then
      echo "error: failed to deploy CommunityPool contract" >&2
      echo "detail: $(tr '\n' ' ' <"$deploy_err" | head -c 500)" >&2
      rm -f "$deploy_err"
      exit 1
    fi
    if ! POOL_EVM_ADDR="$(pool_addr_from_cast_deploy_output "$deploy_raw")" || [[ -z "$POOL_EVM_ADDR" ]]; then
      echo "error: could not parse CommunityPool contract address from deploy output" >&2
      echo "deploy_output: $(printf '%s' "$deploy_raw" | head -c 500)" >&2
      echo "deploy_error: $(tr '\n' ' ' <"$deploy_err" | head -c 500)" >&2
      rm -f "$deploy_err"
      exit 1
    fi
    rm -f "$deploy_err"
  fi
  resolve_pool_bech32
  echo "pool_contract_evm=$POOL_EVM_ADDR pool_delegator_bech32=$POOL_DEL_ADDR"
}

set_pool_automation_caller() {
  CURRENT_PHASE="set_pool_automation_caller"
  echo "==> Setting CommunityPool automationCaller=$MODULE_EVM (single EVM tx; poolrebalancer module must be this caller)"
  cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" \
    "$POOL_EVM_ADDR" "setAutomationCaller(address)" "$MODULE_EVM" >/dev/null
}

set_pool_delegator_param_runtime() {
  CURRENT_PHASE="set_pool_delegator_param_runtime"
  echo "==> Governance: set poolrebalancer.params.pool_delegator_address=$POOL_DEL_ADDR"
  echo "    This can take up to ~$GOV_STATUS_TIMEOUT s (vote + PROPOSAL_STATUS_PASSED + param poll) — longer than deploy on some machines."
  local gov_auth current proposal_json status current_addr t0 elapsed submit_out proposal_id before_latest after_latest submit_code submit_log vote_out vote_code vote_log
  resolve_governance_signer
  gov_auth="$(evmd query auth module-account gov --node "$NODE_RPC" -o json | jq -r '.account.value.address')"
  current="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json)"
  before_latest="$(evmd query gov proposals --node "$NODE_RPC" -o json 2>/dev/null | jq -r '[.proposals[]?.id | tonumber] | max // 0')"
  proposal_json="$(echo "$current" | jq --arg gov "$gov_auth" --arg del "$POOL_DEL_ADDR" --arg dep "$GOV_DEPOSIT" '{
    messages:[{
      "@type":"/cosmos.poolrebalancer.v1.MsgUpdateParams",
      authority:$gov,
      params:{
        pool_delegator_address:$del,
        max_target_validators:.params.max_target_validators,
        rebalance_threshold_bp:.params.rebalance_threshold_bp,
        max_ops_per_block:.params.max_ops_per_block,
        max_move_per_op:.params.max_move_per_op
      }
    }],
    metadata:"",
    deposit:$dep,
    title:"Set pool delegator for rebalance scenario runner",
    summary:"Set CommunityPool account for poolrebalancer runtime scenarios.",
    expedited:false
  }')"
  submit_out="$(evmd tx gov submit-proposal <(echo "$proposal_json") \
    --from "$RESOLVED_GOV_FROM" --keyring-backend "$KEYRING" --home "$RESOLVED_GOV_HOME" \
    --chain-id "$CHAIN_ID" --node "$NODE_RPC" \
    --fees "$GOV_FEES" --gas auto --gas-adjustment 1.5 -y -o json)"
  submit_code="$(echo "$submit_out" | jq -r '.code // 0')"
  if [[ "$submit_code" != "0" ]]; then
    submit_log="$(echo "$submit_out" | jq -r '.raw_log // .log // empty')"
    echo "error: governance submit-proposal failed (code=$submit_code)" >&2
    echo "detail: $submit_log" >&2
    exit 1
  fi
  proposal_id="$(echo "$submit_out" | jq -r '
    .proposal_id // .tx_response?.proposal_id // .tx_response?.events[]? | select(.type=="submit_proposal") | .attributes[]? | select(.key=="proposal_id" or .key=="cHJvcG9zYWxfaWQ=") | .value
  ' 2>/dev/null | tail -n 1)"
  if [[ -z "$proposal_id" || "$proposal_id" == "null" ]]; then
    t0="$(date +%s)"
    while true; do
      after_latest="$(evmd query gov proposals --node "$NODE_RPC" -o json 2>/dev/null | jq -r '[.proposals[]?.id | tonumber] | max // 0')"
      if [[ "$after_latest" =~ ^[0-9]+$ ]] && (( after_latest > before_latest )); then
        proposal_id="$after_latest"
        break
      fi
      elapsed="$(($(date +%s) - t0))"
      if (( elapsed > 30 )); then
        break
      fi
      sleep 2
    done
  fi
  if [[ -z "$proposal_id" || "$proposal_id" == "null" ]]; then
    echo "error: could not determine governance proposal id for pool delegator update" >&2
    echo "debug: before_latest=$before_latest submit_out_snippet=$(printf '%s' "$submit_out" | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g' | cut -c1-300)" >&2
    exit 1
  fi
  echo "proposal_id=$proposal_id"
  if ! vote_proposal_with_validator_majority "$proposal_id"; then
    exit 1
  fi
  sleep "$GOV_WAIT_INITIAL"
  t0="$(date +%s)"
  while true; do
    status="$(evmd query gov proposal "$proposal_id" --node "$NODE_RPC" -o json | jq -r '.proposal.status')"
    case "$status" in
      PROPOSAL_STATUS_PASSED)
        break
        ;;
      PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED|PROPOSAL_STATUS_ABORTED)
        echo "error: governance proposal reached terminal non-passed status=$status" >&2
        exit 1
        ;;
      *)
        elapsed="$(($(date +%s) - t0))"
        if (( elapsed > GOV_STATUS_TIMEOUT )); then
          echo "error: governance proposal did not pass before timeout (status=$status timeout=${GOV_STATUS_TIMEOUT}s)" >&2
          exit 1
        fi
        sleep 2
        ;;
    esac
  done
  t0="$(date +%s)"
  while true; do
    current_addr="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address // ""')"
    [[ "$current_addr" == "$POOL_DEL_ADDR" ]] && break
    elapsed="$(($(date +%s) - t0))"
    if [[ "$elapsed" -gt "$GOV_POLL_TIMEOUT" ]]; then
      echo "error: pool_delegator_address not propagated (have=$current_addr want=$POOL_DEL_ADDR)" >&2
      exit 1
    fi
    sleep 2
  done
}

configure_contract_pool_delegator() {
  log_pool_contract_setup_banner
  deploy_pool_contract_if_needed
  set_pool_automation_caller
  set_pool_delegator_param_runtime
}

set_pool_contract_config() {
  local max_retrieve="$1"
  local max_validators="$2"
  local min_stake="$3"
  local errf
  errf="$(mktemp "${TMPDIR:-/tmp}/poolrebalancer-setconfig.XXXXXX")"
  if ! cast send --json --rpc-url "$EVM_RPC" --private-key "$POOL_OWNER_PK" \
    "$POOL_EVM_ADDR" "setConfig(uint32,uint32,uint256)" "$max_retrieve" "$max_validators" "$min_stake" 2>"$errf" >/dev/null; then
    echo "error: CommunityPool setConfig failed (max_retrieve=$max_retrieve max_validators=$max_validators min_stake=$min_stake pool=$POOL_EVM_ADDR)" >&2
    cat "$errf" >&2
    rm -f "$errf"
  return 1
  fi
  rm -f "$errf"
}

verify_contract_pool_readiness() {
  CURRENT_PHASE="verify_contract_pool_readiness"
  echo "==> Verifying contract pool readiness"
  local onchain_del caller_raw caller_lc module_lc stakeable total_staked principal_assets
  onchain_del="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address // ""')"
  if [[ "$onchain_del" != "$POOL_DEL_ADDR" ]]; then
    echo "error: readiness failed; pool_delegator_address mismatch (have=$onchain_del want=$POOL_DEL_ADDR)" >&2
    exit 1
  fi
  caller_raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "automationCaller()(address)" 2>/dev/null || true)"
  caller_lc="$(printf '%s' "$caller_raw" | tr '[:upper:]' '[:lower:]')"
  module_lc="$(printf '%s' "$MODULE_EVM" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "$caller_lc" || "$caller_lc" != "$module_lc" ]]; then
    echo "error: readiness failed; automationCaller mismatch (have=$caller_raw want=$MODULE_EVM)" >&2
    exit 1
  fi

  stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
  total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
  principal_assets="$(pool_call_uint256 "principalAssets()(uint256)")"
  if [[ "$stakeable" == "n/a" || "$total_staked" == "n/a" || "$principal_assets" == "n/a" ]]; then
    echo "error: readiness failed; unable to query CommunityPool accounting views" >&2
    exit 1
  fi
  echo "pool_readiness: stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets"
}

wait_for_pool_stake_activation() {
  local timeout_secs="${1:-120}"
  local start now total_staked del_count
  start="$(date +%s)"
  while true; do
    total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
    del_count="$(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json 2>/dev/null | jq -r '.delegation_responses | length // 0')"
    if [[ "$total_staked" =~ ^[0-9]+$ ]] && (( total_staked > 0 )) && [[ "$del_count" =~ ^[0-9]+$ ]] && (( del_count > 0 )); then
      echo "pool_stake_activated: total_staked=$total_staked delegations_count=$del_count"
      return 0
    fi
    now="$(date +%s)"
    if (( now - start > timeout_secs )); then
      echo "error: timed out waiting for CommunityPool automated stake activation" >&2
      echo "hint: verify automationCaller, pool_delegator_address, and chain EndBlock logs" >&2
      return 1
    fi
    sleep 2
  done
}

seed_contract_imbalance() {
  CURRENT_PHASE="seed_contract_imbalance"
  echo "==> Creating contract-driven initial imbalance (scenario=$SCENARIO)"
  local seed_max_validators=1
  local seed_amount_main seed_amount_minor
  seed_amount_main="$POOL_SEED_DEPOSIT_AMOUNT"
  seed_amount_minor="$(coin_amount_to_uint "$IMBALANCE_MINOR_DELEGATION")"
  echo "seed_plan: main=$seed_amount_main minor=$seed_amount_minor"

  case "$SCENARIO" in
    expansion)
      seed_max_validators=3
      ;;
    threshold_boundary|happy_path|caps)
      seed_max_validators=1
      ;;
    *)
      echo "error: unsupported SCENARIO in contract seeding: $SCENARIO" >&2
      exit 1
      ;;
  esac

  echo "==> Applying CommunityPool setConfig for seeding (max_retrieve=10 max_validators=$seed_max_validators minStake=1)"
  set_pool_contract_config 10 "$seed_max_validators" 1

  case "$SCENARIO" in
    threshold_boundary)
      echo "==> Seeding small contract deposit: amount=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
    expansion)
      echo "==> Seeding contract deposits for expansion profile: main=$seed_amount_main minor=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_main"
      deposit_to_pool_once "$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
    happy_path|caps)
      echo "==> Seeding contract deposits for skew profile: main=$seed_amount_main minor=$seed_amount_minor"
      deposit_to_pool_once "$seed_amount_main"
      deposit_to_pool_once "$seed_amount_minor"
      ;;
  esac

  wait_for_pool_stake_activation 150

  if [[ "$SCENARIO" == "expansion" ]]; then
    EXPANSION_INITIAL_DELEGATED=()
    while IFS= read -r val; do
      [[ -z "$val" ]] && continue
      EXPANSION_INITIAL_DELEGATED+=("$val")
    done < <(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json | jq -r '.delegation_responses[]?.delegation.validator_address' | head -n 3)
    echo "expansion_seeded_validators=${#EXPANSION_INITIAL_DELEGATED[@]}"
  fi

  # Restore broader staking spread for post-seed automation behavior.
  local post_seed_min_stake="1"
  echo "==> Final CommunityPool setConfig (max_retrieve=10 max_validators=5 minStake=$post_seed_min_stake)"
  wait_evm_nonce_settled_for_pk "$POOL_OWNER_PK" 90
  set_pool_contract_config 10 5 "$post_seed_min_stake" || exit 1
}

# First-line context for watch when the chain is up but setup has not wired the pool yet.
log_watch_pool_delegator_setup_hint() {
  local mode_label="${1:-watch}"
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local rule="──────────────────────────────────────────────────────────────────────────────"
  local params del
  params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || true)"
  del="$(printf '%s' "$params" | jq -r '.params.pool_delegator_address // empty' 2>/dev/null || true)"
  if [[ -n "$del" ]]; then
    return 0
  fi
  printf '%s\n' "$rule"
  echo "$mode_label: pool_delegator_address is not set on-chain yet."
  echo "If another shell is still running this script (default run flow), it may be deploying CommunityPool,"
  echo "configuring automation, and passing governance to set poolrebalancer.params.pool_delegator_address."
  echo "Rough guide: on a typical local devnet, wiring often completes around block heights ~30–32; wall-clock often ~1–3 min."
  echo "Caps: proposal status poll up to ${GOV_STATUS_TIMEOUT}s, then param propagation up to ${GOV_POLL_TIMEOUT}s."
  echo "This watch keeps polling until reads succeed."
  printf '%s\n' "$rule"
}

check_pending_invariants() {
  local json="$1"
  local cap="$2"
  local max_ops="$3"

  # Important nuance:
  # pending-redelegations query returns primary records that can merge multiple ops
  # sharing (delegator, denom, dst, completionTime). With max_ops_per_block > 1,
  # a merged record amount can exceed max_move_per_op even if each individual op respected the cap.
  # So strict cap checking is only sound when max_ops_per_block == 1.
  if [[ "$cap" != "0" && "$max_ops" == "1" ]]; then
    local badAmt
    badAmt="$(echo "$json" | jq -r --argjson cap "$cap" '[.redelegations[] | (.amount.amount|tonumber) > $cap] | any')"
    if [[ "$badAmt" != "false" ]]; then
      echo "warning: found pending amount > max_move_per_op" >&2
      return 1
    fi
  elif [[ "$cap" != "0" && "$max_ops" != "1" ]]; then
    echo "note: skipping strict max_move_per_op check on merged primary entries (max_ops_per_block=$max_ops)"
  fi

  # Transitive safety: a source validator must not also be an in-flight destination.
  local badTrans
  badTrans="$(echo "$json" | jq -r '([.redelegations[].src_validator_address] | unique) as $srcs | ([.redelegations[].dst_validator_address] | unique) as $dsts | ([ $srcs[] | . as $s | (($dsts | index($s)) != null) ] | any)')"
  if [[ "$badTrans" != "false" ]]; then
    echo "warning: transitive safety violated (src appears in dst set)" >&2
    return 1
  fi
}

watch_rebalance_status() {
  # Read-only watch mode for an already running test chain.
  # Use this to inspect params/pending queues without re-running setup.
  CURRENT_PHASE="watch"
  local node="${NODE_RPC:-tcp://127.0.0.1:26657}"
  local status_url last_h=""
  status_url="$(tendermint_status_url)"

  while true; do
    local h params del pr stakeable total_staked principal_assets reward_reserve caller_raw caller_lc module_lc automation_ready pending_red_json
    h="$(curl -sS --max-time 2 "$status_url" | jq -r '.result.sync_info.latest_block_height // "n/a"')"
    if [[ -z "$h" || "$h" == "n/a" || "$h" == "$last_h" ]]; then
      sleep 1
      continue
    fi
    last_h="$h"
    params="$(evmd query poolrebalancer params --node "$node" -o json 2>/dev/null || echo '{}')"
    del="$(echo "$params" | jq -r '.params.pool_delegator_address // empty')"
    if [[ -z "${POOL_EVM_ADDR:-}" && -n "$del" ]]; then
      POOL_EVM_ADDR="$(resolve_evm_hex_from_bech32 "$del")"
      if [[ -n "$POOL_EVM_ADDR" ]]; then
        POOL_DEL_ADDR="$del"
      fi
    fi
    pending_red_json="$(evmd query poolrebalancer pending-redelegations --node "$node" -o json 2>/dev/null || echo '{"redelegations":[]}' )"
    pr="$(echo "$pending_red_json" | jq -r '.redelegations | length // 0')"
    stakeable="n/a"
    total_staked="n/a"
    principal_assets="n/a"
    reward_reserve="n/a"
    caller_raw=""
    automation_ready="no"
    if [[ -n "${POOL_EVM_ADDR:-}" ]]; then
      stakeable="$(pool_call_uint256 "stakeablePrincipalLedger()(uint256)")"
      total_staked="$(pool_call_uint256 "totalStaked()(uint256)")"
      principal_assets="$(pool_call_uint256 "principalAssets()(uint256)")"
      reward_reserve="$(pool_call_uint256 "rewardReserve()(uint256)")"
      caller_raw="$(cast call --rpc-url "$EVM_RPC" "$POOL_EVM_ADDR" "automationCaller()(address)" 2>/dev/null || true)"
      caller_lc="$(printf '%s' "$caller_raw" | tr '[:upper:]' '[:lower:]')"
      module_lc="$(printf '%s' "$MODULE_EVM" | tr '[:upper:]' '[:lower:]')"
      if [[ -n "$caller_lc" && -n "$module_lc" && "$caller_lc" == "$module_lc" && -n "$del" && "$del" == "$POOL_DEL_ADDR" ]]; then
        automation_ready="yes"
      fi
    fi

    if [[ "$WATCH_COMPACT" == "true" ]]; then
      echo "watch phase=$CURRENT_PHASE height=$h pending_red=$pr stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets reward_reserve=$reward_reserve automation_ready=$automation_ready scenario=$SCENARIO"
    else
      echo "----- rebalance watch -----"
      echo "phase=$CURRENT_PHASE height=$h pending_red=$pr stakeable=$stakeable total_staked=$total_staked principal_assets=$principal_assets reward_reserve=$reward_reserve automation_ready=$automation_ready"
      echo "$params" | jq -r '.params | {pool_delegator_address,max_target_validators,rebalance_threshold_bp,max_ops_per_block,max_move_per_op}'

      if [[ -n "$del" ]]; then
        local del_json
        del_json="$(evmd query staking delegations "$del" --node "$node" -o json 2>/dev/null || echo '{"delegation_responses":[]}' )"
        echo "$del_json" | jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
        if [[ "$WATCH_INITIAL_DELEGATIONS_LOGGED" != "true" ]]; then
          local del_count
          del_count="$(echo "$del_json" | jq -r '.delegation_responses | length // 0')"
          if [[ "$del_count" =~ ^[0-9]+$ ]] && (( del_count > 0 )); then
            if [[ "$pr" == "0" ]]; then
              echo "pre_rebalance_initial_delegations:"
            else
              echo "initial_delegations_first_observed (pending already started):"
            fi
            echo "$del_json" | jq -r '.delegation_responses[]? | {validator: .delegation.validator_address, amount: .balance.amount, denom: .balance.denom}'
            WATCH_INITIAL_DELEGATIONS_LOGGED="true"
          fi
        fi
      else
        echo "pool delegator not configured"
      fi
      echo
    fi
  done
}

setup_localnet() {
  CURRENT_PHASE="setup_localnet"
  SETUP_STARTED="true"
  echo "==> Stopping any existing test chain"
  stop_nodes

  echo "==> Generating test genesis ($VALIDATOR_COUNT validators) at $BASEDIR"
  # multi_node_startup.sh is verbose during init; silence setup noise here.
  (cd "$ROOT_DIR" && VALIDATOR_COUNT="$VALIDATOR_COUNT" DEV_ACCOUNT_COUNT="${DEV_ACCOUNT_COUNT:-100}" GENERATE_GENESIS=true ./multi_node_startup.sh -y >/dev/null 2>&1)
  resolve_pool_runtime_keys

}

configure_genesis_params() {
  CURRENT_PHASE="configure_genesis"
  echo "==> Pool delegator mode = $POOL_DELEGATOR_MODE"
  echo "==> SCENARIO=$SCENARIO VALIDATOR_COUNT=$VALIDATOR_COUNT DEMO_PROFILE=$DEMO_PROFILE threshold_bp=$POOLREBALANCER_THRESHOLD_BP max_target_validators=$POOLREBALANCER_MAX_TARGET_VALIDATORS max_ops_per_block=$POOLREBALANCER_MAX_OPS_PER_BLOCK max_move_per_op=$POOLREBALANCER_MAX_MOVE_PER_OP"
  echo "==> Patching genesis staking params (unbonding_time + max_entries)"
  patch_genesis_staking_params
  echo "==> Patching genesis poolrebalancer params (pool_delegator_address configured at runtime)"
  patch_genesis_poolrebalancer_params
}

start_validators() {
  CURRENT_PHASE="start_validators"
  echo "==> Starting validators"
  mkdir -p "$BASEDIR/logs"
  for v in $(seq 0 $((VALIDATOR_COUNT - 1))); do
    (cd "$ROOT_DIR" && VALIDATOR_COUNT="$VALIDATOR_COUNT" START_VALIDATOR=true NODE_NUMBER="$v" ./multi_node_startup.sh >"$BASEDIR/logs/val${v}.log" 2>&1 &)
  done
  if [[ "$STREAM_VALIDATOR_LOGS" == "true" ]]; then
    echo "==> Streaming validator logs (val0..val$((VALIDATOR_COUNT - 1)))"
    start_validator_log_streams
  fi
}

wait_chain_ready() {
  CURRENT_PHASE="wait_chain_ready"
  echo "==> Waiting for block production"
  local h
  h="$(wait_for_height 60)"
  echo "height=$h"

  # Resolve chain bond denom and rewrite amount knobs to match this network.
  BOND_DENOM="$(evmd query staking params --node "$NODE_RPC" -o json | jq -r '.params.bond_denom // .bond_denom')"
  if [[ -z "$BOND_DENOM" || "$BOND_DENOM" == "null" ]]; then
    echo "error: could not determine bond_denom from staking params" >&2
    exit 1
  fi
  echo "bond_denom=$BOND_DENOM"
  TX_FEES="${TX_FEES%ogwei}${BOND_DENOM}"
  IMBALANCE_MAIN_DELEGATION="${IMBALANCE_MAIN_DELEGATION%ogwei}${BOND_DENOM}"
  IMBALANCE_MINOR_DELEGATION="${IMBALANCE_MINOR_DELEGATION%ogwei}${BOND_DENOM}"
  ensure_evm_rpc_ready
}

seed_initial_imbalance() {
  CURRENT_PHASE="seed_initial_imbalance"
  if [[ "$POOL_DELEGATOR_MODE" == "contract" ]]; then
    seed_contract_imbalance
    return 0
  fi
  echo "error: unsupported POOL_DELEGATOR_MODE=$POOL_DELEGATOR_MODE" >&2
  exit 1
}

run_sanity_checks() {
  CURRENT_PHASE="run_sanity_checks"
  echo "==> Sanity checks (params + delegations)"
  local onchain_del
  onchain_del="$(evmd query poolrebalancer params --node "$NODE_RPC" -o json | jq -r '.params.pool_delegator_address')"
  if [[ "$onchain_del" != "$POOL_DEL_ADDR" ]]; then
    echo "error: poolrebalancer params.pool_delegator_address mismatch" >&2
    echo "  expected: $POOL_DEL_ADDR" >&2
    echo "  got:      $onchain_del" >&2
    exit 1
  fi

  local del_count
  del_count="$(evmd query staking delegations "$POOL_DEL_ADDR" --node "$NODE_RPC" -o json | jq -r '.delegation_responses | length')"
  echo "delegations_count=$del_count"

  local bonded_count
  bonded_count="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -r '[.validators[] | select(.status=="BOND_STATUS_BONDED")] | length')"
  echo "bonded_validators=$bonded_count"
  if (( bonded_count == 0 )); then
    echo "error: no bonded validators found; cannot rebalance" >&2
    exit 1
  fi
  if [[ "$POOL_DELEGATOR_MODE" == "contract" && "$del_count" == "0" ]]; then
    echo "error: contract delegator has zero delegations after seeding; rebalance cannot run" >&2
    exit 1
  fi

  if [[ "$SCENARIO" == "expansion" ]]; then
    if (( bonded_count < 5 )); then
      echo "error: expansion expects at least 5 bonded validators, got $bonded_count (use --nodes 5)" >&2
      exit 1
    fi

    if (( ${#EXPANSION_INITIAL_DELEGATED[@]} != 3 )); then
      echo "error: expansion seed did not produce 3 initial delegations (got ${#EXPANSION_INITIAL_DELEGATED[@]})" >&2
      exit 1
    fi

    local bonded_json
    bonded_json="$(evmd query staking validators --node "$NODE_RPC" -o json | jq -c '[.validators[] | select(.status=="BOND_STATUS_BONDED") | .operator_address] | unique')"
    local seeded_json
    seeded_json="$(printf '%s\n' "${EXPANSION_INITIAL_DELEGATED[@]}" | jq -R . | jq -s -c 'unique')"
    EXPANSION_MISSING_DSTS=()
    while IFS= read -r val; do
      [[ -z "$val" ]] && continue
      EXPANSION_MISSING_DSTS+=("$val")
    done < <(jq -n -r --argjson bonded "$bonded_json" --argjson delegated "$seeded_json" '($bonded - $delegated)[]')

    if (( ${#EXPANSION_MISSING_DSTS[@]} < 2 )); then
      echo "error: expansion expects at least 2 bonded validators outside the initial pool delegation set (got ${#EXPANSION_MISSING_DSTS[@]})" >&2
      exit 1
    fi

    EXPANSION_OBSERVED_DSTS_TEXT=""
    echo "scenario_check expansion: bonded=$bonded_count initial_seeded=${#EXPANSION_INITIAL_DELEGATED[@]} extra_targets=${#EXPANSION_MISSING_DSTS[@]}"
  fi

}

update_expansion_observed_dsts() {
  local pending_json="$1"
  if [[ "$SCENARIO" != "expansion" ]]; then
    return 0
  fi

  local dst target
  while IFS= read -r dst; do
    [[ -z "$dst" ]] && continue
    for target in "${EXPANSION_MISSING_DSTS[@]}"; do
      if [[ "$dst" == "$target" ]]; then
        if ! printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | grep -Fxq "$dst" 2>/dev/null; then
          if [[ -n "$EXPANSION_OBSERVED_DSTS_TEXT" ]]; then
            EXPANSION_OBSERVED_DSTS_TEXT="${EXPANSION_OBSERVED_DSTS_TEXT}"$'\n'"$dst"
          else
            EXPANSION_OBSERVED_DSTS_TEXT="$dst"
          fi
        fi
        break
      fi
    done
  done < <(echo "$pending_json" | jq -r '.redelegations[]?.dst_validator_address')
}

expansion_observed_count() {
  local count=0
  local target
  for target in "${EXPANSION_MISSING_DSTS[@]}"; do
    if printf '%s\n' "$EXPANSION_OBSERVED_DSTS_TEXT" | grep -Fxq "$target" 2>/dev/null; then
      count=$((count + 1))
    fi
  done
  echo "$count"
}

observe_and_monitor() {
  CURRENT_PHASE="observe_and_monitor"
  echo "==> Observing pending operations (scenario=$SCENARIO)"
  # Poll loop used for observation:
  # - collect pending queue state
  # - wait until any pending operations appear
  # - validate generic invariants
  for i in $(seq 1 "$POLL_SAMPLES"); do
    local height pending
    height="$(curl -sS --max-time 2 "$(tendermint_status_url)" | jq -r '.result.sync_info.latest_block_height')"
    local j
    j="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json)"
    update_expansion_observed_dsts "$j"
    pending="$(echo "$j" | jq -r '.redelegations | length')"
    if [[ "$WATCH_COMPACT" == "true" ]]; then
      echo "sample=$i phase=$CURRENT_PHASE height=$height pending_red=$pending scenario=$SCENARIO"
    else
      echo "sample=$i phase=$CURRENT_PHASE height=$height pending_red=$pending"
    fi
    if [[ "$SCENARIO" == "expansion" ]]; then
      local seen expected
      seen="$(expansion_observed_count)"
      expected="${#EXPANSION_MISSING_DSTS[@]}"
      echo "expansion_progress: observed_new_destinations=$seen/$expected"
    fi

    if (( pending > 0 )); then
      check_pending_invariants "$j" "$POOLREBALANCER_MAX_MOVE_PER_OP" "$POOLREBALANCER_MAX_OPS_PER_BLOCK"
      echo "info: pending operations observed; continuing monitor"
      if [[ "$KEEP_RUNNING" != "true" ]]; then
        exit 0
      fi
      CURRENT_PHASE="steady_monitor"
      echo "==> KEEP_RUNNING=true, continuing in monitor mode (Ctrl+C to stop)"
      while true; do
        local monitorHeight monitorRed
        monitorHeight="$(curl -sS --max-time 2 "$(tendermint_status_url)" | jq -r '.result.sync_info.latest_block_height')"
        monitorRed="$(evmd query poolrebalancer pending-redelegations --node "$NODE_RPC" -o json | jq -r '.redelegations | length')"
        if [[ "$WATCH_COMPACT" == "true" ]]; then
          echo "monitor phase=$CURRENT_PHASE height=$monitorHeight pending_red=$monitorRed scenario=$SCENARIO"
        else
          echo "monitor phase=$CURRENT_PHASE height=$monitorHeight pending_red=$monitorRed"
        fi
        sleep "$POLL_SLEEP_SECS"
      done
    fi
    sleep "$POLL_SLEEP_SECS"
  done

  echo "info: no pending operations observed within polling window" >&2
  echo "note: this can be expected when drift is below threshold or the system is already balanced" >&2
  exit 0
}

apply_scenario_defaults() {
  # Scenario defaults encode engineer-friendly test behavior.
  # They are applied only when the corresponding env var was not explicitly set.
  case "$SCENARIO" in
    # Canonical scenarios
    happy_path)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      ;;
    caps)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=1000000000000000000; fi
      if [[ "$USER_SET_POOL_SEED_DEPOSIT_AMOUNT" != "true" ]]; then POOL_SEED_DEPOSIT_AMOUNT=500000000000000000000; fi
      ;;
    threshold_boundary)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_THRESHOLD_BP" != "true" ]]; then POOLREBALANCER_THRESHOLD_BP=5000; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=2; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=100000000000000000000; fi
      ;;
    expansion)
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=5; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      if [[ "$USER_SET_MAX_OPS_PER_BLOCK" != "true" ]]; then POOLREBALANCER_MAX_OPS_PER_BLOCK=1; fi
      if [[ "$USER_SET_MAX_MOVE_PER_OP" != "true" ]]; then POOLREBALANCER_MAX_MOVE_PER_OP=10000000000000000000; fi
      if [[ "$USER_SET_IMBALANCE_MINOR_DELEGATION" != "true" ]]; then IMBALANCE_MINOR_DELEGATION=1000000000000000000000ogwei; fi
      ;;
    # Backward-compatible aliases
    baseline_3val)
      SCENARIO="happy_path"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      ;;
    max_target_gt_bonded_3val)
      SCENARIO="happy_path"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=3; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      ;;
    target_set_expansion_5val)
      SCENARIO="expansion"
      if [[ -z "$VALIDATOR_COUNT" ]]; then VALIDATOR_COUNT=5; fi
      if [[ "$USER_SET_MAX_TARGET_VALIDATORS" != "true" ]]; then POOLREBALANCER_MAX_TARGET_VALIDATORS=5; fi
      ;;
    *)
      echo "invalid SCENARIO: $SCENARIO" >&2
      echo "expected: happy_path|caps|threshold_boundary|expansion" >&2
      exit 1
      ;;
  esac
}

main() {
  trap on_interrupt INT TERM
  trap cleanup_on_exit EXIT
  PARSED_SUBCOMMAND=""
  if ! parse_cli_args "$@"; then
    usage
    exit 1
  fi

  if [[ "$PARSED_SUBCOMMAND" == "watch" ]]; then
    require_bin jq
    require_bin curl
    require_bin evmd
    # Match main() tuning so watch output aligns with seeded chains.
    apply_scenario_defaults
    case "${DEMO_PROFILE:-medium}" in
      slow)
        POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-1}"
        POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-10000000000000000000}"
        ;;
      medium) ;;
      fast)
        POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-10}"
        POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-0}"
        ;;
      *)
        echo "invalid DEMO_PROFILE: $DEMO_PROFILE (expected: slow|medium|fast)" >&2
        exit 1
        ;;
    esac
    log_watch_pool_delegator_setup_hint "watch"
    watch_rebalance_status
    exit 0
  fi
  if [[ "$PARSED_SUBCOMMAND" == "help" ]]; then
    usage
    exit 0
  fi
  # Lightweight entry: no genesis/validators — assumes devnet already running.
  if [[ "$PARSED_SUBCOMMAND" == "user_flow_multikey" ]]; then
    require_bin jq
    require_bin curl
    require_bin evmd
    require_bin cast
    apply_scenario_defaults
    if [[ ! "$VALIDATOR_COUNT" =~ ^[0-9]+$ ]] || (( VALIDATOR_COUNT < 1 )); then
      echo "invalid --nodes/VALIDATOR_COUNT: $VALIDATOR_COUNT (expected positive integer)" >&2
      exit 1
    fi
    run_user_flow_multikey_subcommand
    exit 0
  fi
  if [[ "$PARSED_SUBCOMMAND" == "community_pool_edge_cases" ]]; then
    require_bin jq
    require_bin curl
    require_bin evmd
    require_bin cast
    apply_scenario_defaults
    if [[ ! "$VALIDATOR_COUNT" =~ ^[0-9]+$ ]] || (( VALIDATOR_COUNT < 1 )); then
      echo "invalid --nodes/VALIDATOR_COUNT: $VALIDATOR_COUNT (expected positive integer)" >&2
      exit 1
    fi
    run_community_pool_edge_cases_subcommand
    exit 0
  fi

  require_bin jq
  require_bin curl
  require_bin evmd
  require_bin cast

  apply_scenario_defaults
  if [[ ! "$VALIDATOR_COUNT" =~ ^[0-9]+$ ]] || (( VALIDATOR_COUNT < 1 )); then
    echo "invalid --nodes/VALIDATOR_COUNT: $VALIDATOR_COUNT (expected positive integer)" >&2
    exit 1
  fi

  case "$DEMO_PROFILE" in
    slow)
      POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-1}"
      POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-10000000000000000000}" # 1e19
      ;;
    medium)
      # Defaults already set above.
      ;;
    fast)
      POOLREBALANCER_MAX_OPS_PER_BLOCK="${POOLREBALANCER_MAX_OPS_PER_BLOCK:-10}"
      POOLREBALANCER_MAX_MOVE_PER_OP="${POOLREBALANCER_MAX_MOVE_PER_OP:-0}" # no cap
      ;;
    *)
      echo "invalid DEMO_PROFILE: $DEMO_PROFILE (expected: slow|medium|fast)" >&2
      exit 1
      ;;
  esac

  # Execution flow:
  # 1) test chain setup and genesis patching
  # 2) contract deployment + runtime param wiring
  # 3) readiness and scenario seeding
  # 4) sanity checks and scenario-specific observers
  resolve_mnemonics
  setup_localnet
  configure_genesis_params
  start_validators
  wait_chain_ready
  configure_contract_pool_delegator
  verify_contract_pool_readiness
  seed_initial_imbalance
  run_sanity_checks
  observe_and_monitor
}

main "$@"

