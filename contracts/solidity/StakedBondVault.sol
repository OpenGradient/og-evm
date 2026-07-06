// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC4626.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/math/Math.sol";

import "./precompiles/distribution/DistributionI.sol";
import "./precompiles/staking/StakingI.sol";
import "./precompiles/gov/IGov.sol";

contract StakedBondVault is ERC4626, AccessControl, ReentrancyGuard {
    using Math for uint256;
    using SafeERC20 for IERC20;

    bytes32 public constant OPERATOR_ROLE = keccak256("OPERATOR_ROLE");
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");

    uint256 public constant MIN_OPERATION_ASSETS = 1e15;
    uint256 public constant MAX_VALIDATORS = 32;
    // DEFAULT_TARGET_VALIDATOR_COUNT takes the full active set (up to MAX_VALIDATORS) so stake
    // spreads across every bonded validator instead of piling onto a top-N subset.
    uint256 public constant DEFAULT_TARGET_VALIDATOR_COUNT = MAX_VALIDATORS;
    uint256 public constant VALIDATOR_SCAN_PAGE_LIMIT = 32;
    uint256 public constant SCORE_SCALE = 1e18;
    // MAX_UNBONDING_ENTRIES mirrors the staking module's per-(delegator,validator) MaxEntries.
    // Governance must not set the staking MaxEntries below this, or a batch's undelegation can
    // under-fill until entries mature. The undelegate call is wrapped in try/catch, so it
    // degrades rather than reverting if that happens.
    uint256 public constant MAX_UNBONDING_ENTRIES = 7;
    uint256 public constant BPS_DENOM = 10000;
    // MAX_DELEGATION_BPS_PER_VALIDATOR caps how much of NAV can sit with any single validator,
    // keeping each below the one-third Byzantine threshold. When only a few validators exist the
    // effective cap is raised to at least an equal share so funds are never stranded.
    uint256 public constant MAX_DELEGATION_BPS_PER_VALIDATOR = 3300;
    // MAX_SLASH_BUFFER_BPS bounds the operator-configurable slash-exposure buffer.
    uint256 public constant MAX_SLASH_BUFFER_BPS = 5000;
    // MAX_ABSTAIN_BATCH bounds the number of proposals voted on in one voteAbstain call.
    uint256 private constant MAX_ABSTAIN_BATCH = 32;

    uint8 private constant STEP_SETTLE = 1;
    uint8 private constant STEP_PROCESS = 2;
    uint8 private constant STEP_RESUME = 3;
    uint8 private constant STEP_HARVEST = 4;
    uint8 private constant STEP_REFRESH_VALIDATORS = 5;
    uint8 private constant STEP_REBALANCE = 6;
    uint8 private constant STEP_STAKE = 7;
    // POKE_STEP_COUNT is how many pipeline steps a full poke runs. MaxOps must be at least this
    // for every step to get a turn (checked chain-side in EVMSchedulerParams.Validate). Keep it
    // in sync with the STEP_* constants above.
    uint8 public constant POKE_STEP_COUNT = 7;

    struct ValidatorTarget {
        string operatorAddress;
        uint256 score;
        uint256 delegated;
        bool selected;
    }

    struct WithdrawalRequest {
        address owner;
        address receiver;
        uint64 batchId;
        uint256 shares;
    }

    struct UnbondingEntry {
        string validatorAddress;
        uint256 expectedAssets;
        int64 creationHeight;
        int64 completionTime;
        uint64 unbondingId;
        // settled marks an entry whose matured proceeds have already been moved into the
        // batch's maturedAssets bucket, so a later sync won't count it twice.
        bool settled;
    }

    struct WithdrawalBatch {
        bool open;
        bool processed;
        bool claimable;
        uint64 requestCount;
        uint64 claimedCount;
        uint256 totalShares;
        uint256 estimatedAssets;
        uint256 liquidAssets;
        uint256 unbondingAssets;
        // maturedAssets accumulates this batch's own realized (post-slash) unbonding proceeds
        // as its entries complete, so settlement draws from a per-batch bucket instead of the
        // shared unaccounted-liquid pool. That keeps one batch from spending another's proceeds.
        uint256 maturedAssets;
        // pendingUndelegation is principal still waiting to be undelegated because the
        // validators were at their per-validator unbonding-entry cap. _resumeUndelegations
        // drains it across later pokes.
        uint256 pendingUndelegation;
        uint256 settledAssets;
        uint256 claimedAssets;
        int64 maturityTime;
        uint64 firstMatureBlock;
        UnbondingEntry[] unbondingEntries;
    }

    ValidatorTarget[] public validators;
    ValidatorTarget[] private validatorScanCandidates;
    bytes private validatorScanNextKey;

    uint8 public targetValidatorCount = uint8(DEFAULT_TARGET_VALIDATOR_COUNT);
    // slashBufferBps is the fraction of NAV kept idle (un-delegated) to limit slash exposure.
    // The operator sets it; it defaults to 0, and production should give it a non-zero value
    // (e.g. 500 for 5%).
    uint256 public slashBufferBps;
    uint256 public totalIdleLiquid;
    uint256 public totalDelegated;
    uint256 public totalWithdrawalUnbonding;
    uint256 public totalReservedAssets;
    uint256 public totalLiquidReserved;
    uint64 public currentBatchId;
    uint64 public nextSettlementBatchId;
    uint256 public nextRequestId = 1;
    bool public depositsPaused;
    bool public withdrawalsPaused;
    bool public pokePaused;
    uint256 public pokeCount;
    uint256 public lastPokeOps;
    // _lastUndelegationBlock is the block in which the vault last created an unbonding entry.
    // Undelegation is limited to one batch per block, and this lives in storage so the limit
    // holds across separate txs in the same block: poke() and settle() are both permissionless
    // and the scheduler also pokes in EndBlock. If two undelegations hit the same validator in
    // one block, the staking module merges them into a single unbonding entry, which breaks
    // per-batch slash attribution (double-counting and reserve underflow).
    uint256 private _lastUndelegationBlock;

    mapping(uint256 => WithdrawalRequest) public withdrawalRequests;
    mapping(uint64 => WithdrawalBatch) private withdrawalBatches;

    event ValidatorPolicyUpdated(uint8 targetValidatorCount);
    event SlashBufferUpdated(uint256 bps);
    event ValidatorSelectionUpdated(uint256 count);
    event ValidatorRebalanced(string srcValidator, string dstValidator, uint256 amount, int64 completionTime);
    event WithdrawalRequested(uint256 indexed requestId, uint64 indexed batchId, address indexed owner, address receiver, uint256 shares);
    event WithdrawalBatchProcessed(uint64 indexed batchId, uint256 shares, uint256 estimatedAssets, int64 maturityTime);
    event WithdrawalBatchMatured(uint64 indexed batchId, uint64 firstMatureBlock);
    event WithdrawalBatchSettled(uint64 indexed batchId, uint256 settledAssets);
    event WithdrawalClaimed(uint256 indexed requestId, uint64 indexed batchId, address indexed receiver, uint256 assets);
    event PokeStepFailed(uint8 indexed step, bytes reason);
    event Poked(address indexed caller, uint256 ops);

    modifier noNativeValue() {
        require(msg.value == 0, "VALUE");
        _;
    }

    modifier onlySelf() {
        require(msg.sender == address(this), "SELF");
        _;
    }

    // werc20 is the chain's native ERC20 (the WERC20 precompile), passed in per environment
    // instead of hardcoded: production and local use 0xEeee...EEeE, tests use whatever address
    // their genesis registers. We check it has code and 18 decimals so a wrong or unregistered
    // address reverts at deploy time rather than leaving a broken vault.
    constructor(address admin, address werc20, string memory name_, string memory symbol_)
        ERC20(name_, symbol_)
        ERC4626(IERC20Metadata(werc20))
    {
        require(admin != address(0), "ADMIN");
        require(werc20.code.length > 0, "ASSET_CODE");
        require(IERC20Metadata(werc20).decimals() == 18, "ASSET_DECIMALS");
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(OPERATOR_ROLE, admin);
        _grantRole(PAUSER_ROLE, admin);
        currentBatchId = 1;
        nextSettlementBatchId = 1;
        withdrawalBatches[currentBatchId].open = true;
    }

    function totalAssets() public view override returns (uint256) {
        if (totalSupply() == 0) {
            return 0;
        }
        return totalIdleLiquid + _liveDelegatedAssets();
    }

    function liveDelegatedAssets() external view returns (uint256) {
        return _liveDelegatedAssets();
    }

    function syncDelegations() external payable noNativeValue nonReentrant returns (uint256) {
        return _syncDelegations();
    }

    function decimals() public view override(ERC4626) returns (uint8) {
        return ERC4626.decimals();
    }

    function maxDeposit(address) public pure override returns (uint256) {
        return 0;
    }

    function maxMint(address) public pure override returns (uint256) {
        return 0;
    }

    function maxWithdraw(address) public pure override returns (uint256) {
        return 0;
    }

    function maxRedeem(address) public pure override returns (uint256) {
        return 0;
    }

    function deposit(uint256, address) public pure override returns (uint256) {
        revert("DEPOSIT");
    }

    function mint(uint256, address) public pure override returns (uint256) {
        revert("DEPOSIT");
    }

    function depositNative(address receiver) external payable nonReentrant returns (uint256 shares) {
        require(!depositsPaused, "D_PAUSED");
        require(receiver != address(0), "RECEIVER");
        require(msg.value >= MIN_OPERATION_ASSETS, "MIN");
        _harvest();

        uint256 assets = msg.value;
        uint256 supply = totalSupply();
        uint256 assetsBefore = totalAssets();
        if (supply == 0) {
            shares = assets;
        } else {
            require(assetsBefore != 0, "INSOLVENT");
            shares = assets.mulDiv(supply + 1, assetsBefore + 1, Math.Rounding.Down);
        }
        require(shares > 0, "SHARES");
        totalIdleLiquid += assets;
        _mint(receiver, shares);
        emit Deposit(msg.sender, receiver, assets, shares);
    }

    function withdraw(uint256, address, address) public pure override returns (uint256) {
        revert("REDEEM");
    }

    function redeem(uint256, address, address) public pure override returns (uint256) {
        revert("REDEEM");
    }

    function requestRedeem(uint256 shares, address receiver, address owner) external payable noNativeValue nonReentrant returns (uint256 requestId) {
        require(!withdrawalsPaused, "W_PAUSED");
        require(shares > 0, "SHARES");
        require(receiver != address(0), "RECEIVER");
        require(owner != address(0), "OWNER");
        require(convertToAssets(shares) >= MIN_OPERATION_ASSETS, "MIN");
        if (msg.sender != owner) {
            _spendAllowance(owner, msg.sender, shares);
        }

        _transfer(owner, address(this), shares);
        requestId = nextRequestId++;
        WithdrawalBatch storage batch = _openBatch();
        withdrawalRequests[requestId] = WithdrawalRequest({
            owner: owner,
            receiver: receiver,
            batchId: currentBatchId,
            shares: shares
        });
        batch.totalShares += shares;
        batch.requestCount += 1;
        emit WithdrawalRequested(requestId, currentBatchId, owner, receiver, shares);
    }

    function claimRedeem(uint256 requestId, address receiver) external payable noNativeValue nonReentrant returns (uint256 assetsOut) {
        WithdrawalRequest memory request = withdrawalRequests[requestId];
        require(request.owner != address(0), "REQUEST");

        WithdrawalBatch storage batch = withdrawalBatches[request.batchId];
        require(batch.claimable, "CLAIMABLE");
        address payout = receiver == address(0) ? request.receiver : receiver;
        require(payout == request.receiver, "PAYOUT");

        if (batch.claimedCount + 1 == batch.requestCount) {
            assetsOut = batch.settledAssets - batch.claimedAssets;
        } else {
            assetsOut = request.shares.mulDiv(batch.settledAssets, batch.totalShares, Math.Rounding.Down);
        }
        batch.claimedCount += 1;
        batch.claimedAssets += assetsOut;
        totalReservedAssets -= assetsOut;
        totalLiquidReserved -= assetsOut;
        delete withdrawalRequests[requestId];
        IERC20(asset()).safeTransfer(payout, assetsOut);
        emit WithdrawalClaimed(requestId, request.batchId, payout, assetsOut);

        if (batch.claimedCount == batch.requestCount) {
            delete withdrawalBatches[request.batchId];
        }
    }

    function poke(uint256 maxOps) external payable noNativeValue nonReentrant returns (uint256 ops) {
        require(!pokePaused, "P_PAUSED");
        if (maxOps == 0) {
            return 0;
        }
        ops = _tryPokeStep(STEP_SETTLE, maxOps, ops);
        ops = _tryPokeStep(STEP_PROCESS, maxOps, ops);
        ops = _tryPokeStep(STEP_RESUME, maxOps, ops);
        ops = _tryPokeStep(STEP_HARVEST, maxOps, ops);
        ops = _tryPokeStep(STEP_REFRESH_VALIDATORS, maxOps, ops);
        ops = _tryPokeStep(STEP_REBALANCE, maxOps, ops);
        ops = _tryPokeStep(STEP_STAKE, maxOps, ops);
        pokeCount++;
        lastPokeOps = ops;
        emit Poked(msg.sender, ops);
    }

    // settle runs the withdrawal settle/process/resume steps regardless of pokePaused, so
    // pausing the scheduler can never leave pending withdrawal claims stuck. Permissionless
    // and bounded by maxOps.
    function settle(uint256 maxOps) external payable noNativeValue nonReentrant returns (uint256 ops) {
        if (maxOps == 0) {
            return 0;
        }
        ops = _tryPokeStep(STEP_SETTLE, maxOps, ops);
        ops = _tryPokeStep(STEP_PROCESS, maxOps, ops);
        ops = _tryPokeStep(STEP_RESUME, maxOps, ops);
    }

    function pokeStep(uint8 step) external onlySelf returns (bool) {
        if (step == STEP_SETTLE) {
            return _settleMatureBatch();
        }
        if (step == STEP_PROCESS) {
            return _processCurrentBatch();
        }
        if (step == STEP_RESUME) {
            return _resumeUndelegations();
        }
        if (step == STEP_HARVEST) {
            return _harvest();
        }
        if (step == STEP_REFRESH_VALIDATORS) {
            return _refreshValidatorSelection();
        }
        if (step == STEP_REBALANCE) {
            return _rebalanceValidators();
        }
        if (step == STEP_STAKE) {
            return _stakeIdle();
        }
        return false;
    }

    function setPaused(bool deposits, bool withdrawals, bool scheduler) external payable noNativeValue onlyRole(PAUSER_ROLE) {
        depositsPaused = deposits;
        withdrawalsPaused = withdrawals;
        pokePaused = scheduler;
    }

    function setValidatorPolicy(uint8 count) external payable noNativeValue onlyRole(OPERATOR_ROLE) {
        require(count > 0 && count <= MAX_VALIDATORS, "COUNT");
        targetValidatorCount = count;
        delete validatorScanCandidates;
        delete validatorScanNextKey;
        emit ValidatorPolicyUpdated(count);
    }

    // setSlashBufferBps sets the fraction of NAV kept un-delegated as a slash buffer, capped at
    // MAX_SLASH_BUFFER_BPS. It defaults to 0; production should set it non-zero.
    function setSlashBufferBps(uint256 bps) external payable noNativeValue onlyRole(OPERATOR_ROLE) {
        require(bps <= MAX_SLASH_BUFFER_BPS, "BUFFER");
        slashBufferBps = bps;
        emit SlashBufferUpdated(bps);
    }

    // voteAbstain casts an abstain vote from the vault on each supplied governance proposal.
    // The vault's bonded deposits count toward its validators' voting power, and the standard
    // tally casts a non-voting delegator's power the way its validator votes. So the vault has
    // to abstain explicitly, or the pooled deposits would swing governance outcomes. The list of
    // open proposal ids comes from an off-chain keeper to keep the contract under the EIP-170
    // code-size limit; re-voting abstain during the voting period is a no-op. The abstaining
    // stake still counts toward quorum, which is a known trade-off.
    function voteAbstain(uint64[] calldata proposalIds) external payable noNativeValue onlyRole(OPERATOR_ROLE) {
        require(proposalIds.length <= MAX_ABSTAIN_BATCH, "BATCH");
        for (uint256 i = 0; i < proposalIds.length; i++) {
            try GOV_CONTRACT.vote(address(this), proposalIds[i], VoteOption.Abstain, "") {} catch {}
        }
    }

    function withdrawalBatch(uint64 batchId)
        external
        view
        returns (
            bool open,
            bool processed,
            bool claimable,
            uint64 requestCount,
            uint64 claimedCount,
            uint256 totalShares_,
            uint256 estimatedAssets,
            uint256 liquidAssets,
            uint256 unbondingAssets,
            uint256 settledAssets,
            uint256 claimedAssets,
            int64 maturityTime,
            uint64 firstMatureBlock
        )
    {
        WithdrawalBatch storage batch = withdrawalBatches[batchId];
        return (
            batch.open,
            batch.processed,
            batch.claimable,
            batch.requestCount,
            batch.claimedCount,
            batch.totalShares,
            batch.estimatedAssets,
            batch.liquidAssets,
            batch.unbondingAssets,
            batch.settledAssets,
            batch.claimedAssets,
            batch.maturityTime,
            batch.firstMatureBlock
        );
    }

    function validatorCount() external view returns (uint256) {
        return validators.length;
    }

    function selectedValidatorCount() external view returns (uint256 count) {
        for (uint256 i = 0; i < validators.length; i++) {
            if (validators[i].selected) {
                count++;
            }
        }
    }

    function _tryPokeStep(uint8 step, uint256 maxOps, uint256 ops) private returns (uint256) {
        if (ops >= maxOps) {
            return ops;
        }
        try this.pokeStep(step) returns (bool didWork) {
            if (didWork) {
                return ops + 1;
            }
        } catch (bytes memory reason) {
            emit PokeStepFailed(step, reason);
        }
        return ops;
    }

    function _openBatch() internal returns (WithdrawalBatch storage batch) {
        batch = withdrawalBatches[currentBatchId];
        if (!batch.open || batch.processed) {
            currentBatchId += 1;
            batch = withdrawalBatches[currentBatchId];
            batch.open = true;
        }
    }

    function _processCurrentBatch() internal returns (bool) {
        WithdrawalBatch storage batch = withdrawalBatches[currentBatchId];
        if (!batch.open || batch.processed || batch.totalShares == 0) {
            return false;
        }

        _harvest();
        uint256 assetsOut = convertToAssets(batch.totalShares);
        if (assetsOut == 0) {
            return false;
        }
        batch.open = false;
        batch.processed = true;
        batch.estimatedAssets = assetsOut;
        _burn(address(this), batch.totalShares);

        uint256 liquid = _min(totalIdleLiquid, assetsOut);
        if (liquid != 0) {
            totalIdleLiquid -= liquid;
            totalReservedAssets += liquid;
            totalLiquidReserved += liquid;
            batch.liquidAssets = liquid;
        }

        uint256 remaining = assetsOut - liquid;
        int64 maturity;
        if (remaining != 0) {
            maturity = _undelegateForBatch(batch, remaining);
        }
        batch.maturityTime = maturity;
        // A batch is claimable right away only if it's fully liquid: nothing unbonding and
        // nothing left to undelegate. Otherwise it settles later in _settleMatureBatch.
        if (batch.unbondingAssets == 0 && batch.pendingUndelegation == 0) {
            batch.claimable = true;
            batch.settledAssets = batch.liquidAssets;
        }
        emit WithdrawalBatchProcessed(currentBatchId, batch.totalShares, assetsOut, maturity);

        currentBatchId += 1;
        withdrawalBatches[currentBatchId].open = true;
        return true;
    }

    function _settleMatureBatch() internal returns (bool) {
        // One batch never blocks another. A batch that isn't ready yet (still unbonding, still
        // has pending undelegation, or waiting on the maturity confirmation) is skipped rather
        // than returned on, so it can't freeze claims for later batches. The settlement cursor
        // only moves past the leading run of already-claimable batches, so earlier unsettled
        // batches get re-scanned next time.
        uint64 cursor = nextSettlementBatchId;
        bool cursorAdvancing = true;
        for (uint64 batchId = nextSettlementBatchId; batchId < currentBatchId; batchId++) {
            WithdrawalBatch storage batch = withdrawalBatches[batchId];
            if (!batch.processed || batch.claimable) {
                if (cursorAdvancing) {
                    cursor = batchId + 1;
                }
                continue;
            }
            cursorAdvancing = false;

            // Wait until this batch's full principal is unbonding (nothing left to resume).
            if (batch.pendingUndelegation != 0) {
                continue;
            }
            // Wait until the latest unbonding entry has matured...
            if (block.timestamp < uint256(uint64(batch.maturityTime))) {
                continue;
            }
            // ...then require a one-block confirmation before settling.
            if (batch.firstMatureBlock == 0) {
                batch.firstMatureBlock = uint64(block.number);
                emit WithdrawalBatchMatured(batchId, batch.firstMatureBlock);
                nextSettlementBatchId = cursor;
                return true;
            }
            if (block.number <= batch.firstMatureBlock) {
                continue;
            }

            // Move each entry's realized (post-slash) proceeds into this batch's own
            // maturedAssets bucket, and finalize only once every entry has completed.
            //
            // Known corner case: a slash that lands during the unbonding window is only picked
            // up here, at maturity, by which point the entry has already left the staking queue,
            // so maturedAssets can still hold the pre-slash amount. The min(maturedAssets,
            // _unaccountedLiquid()) clamp below limits a single settling batch to what actually
            // arrived, but when several batches or harvest proceeds share the unaccounted pool
            // the loss can still spread across them. A full fix would ratchet each entry while
            // it's still pending, before maturity; that's deferred because it means reconciling
            // how matured native proceeds re-enter the WERC20-denominated accounting.
            bool complete = _syncBatchUnbonding(batch);
            if (!complete) {
                continue;
            }

            // Draw only this batch's matured proceeds, clamped to what has actually arrived in
            // the contract. A fully-slashed batch settles at its liquid portion so users can
            // still claim it rather than having it stranded.
            uint256 want = batch.maturedAssets;
            uint256 matured = _min(want, _unaccountedLiquid());
            if (matured < want) {
                // Past maturity the funds should already be here; if they aren't, drop the
                // shortfall from reserves so the accounting invariant stays exact.
                totalReservedAssets -= (want - matured);
            }
            totalLiquidReserved += matured;
            batch.maturedAssets = 0;
            batch.settledAssets = batch.liquidAssets + matured;
            batch.claimable = true;
            nextSettlementBatchId = cursor;
            emit WithdrawalBatchSettled(batchId, batch.settledAssets);
            return true;
        }
        nextSettlementBatchId = cursor;
        return false;
    }

    // _undelegateForBatch queues undelegations to cover assetsNeeded, one per validator. It
    // never reverts on a shortfall (that would freeze the whole batch). Any principal it can't
    // undelegate right now, because the validators are at their per-validator unbonding-entry
    // cap, goes into batch.pendingUndelegation for _resumeUndelegations to drain over later
    // pokes. If no delegation is left anywhere, the remainder can't be met, so the batch settles
    // for less (redeemers share the shortfall) instead of getting stuck.
    function _undelegateForBatch(WithdrawalBatch storage batch, uint256 assetsNeeded) internal returns (int64 maturity) {
        require(validators.length != 0, "VALIDATORS");
        // Limit undelegation to one batch per block. If the vault already created an unbonding
        // entry this block, defer the rest via pendingUndelegation so two batches never
        // undelegate the same validator in the same block; the staking module would merge those
        // into one entry and break per-batch slash attribution.
        if (_lastUndelegationBlock == block.number) {
            batch.pendingUndelegation = assetsNeeded;
            return batch.maturityTime;
        }
        _syncDelegations();
        uint256 remaining = assetsNeeded;
        bool anyDelegationLeft = false;
        for (uint256 i = 0; i < validators.length && remaining != 0; i++) {
            ValidatorTarget storage target = validators[i];
            if (target.delegated == 0) {
                continue;
            }
            anyDelegationLeft = true;
            if (_unbondingEntryCount(target.operatorAddress) >= MAX_UNBONDING_ENTRIES) {
                continue;
            }
            uint256 amount = _min(target.delegated, remaining);
            try STAKING_CONTRACT.undelegate(address(this), target.operatorAddress, amount) returns (int64 completion) {
                target.delegated -= amount;
                totalDelegated -= amount;
                totalWithdrawalUnbonding += amount;
                totalReservedAssets += amount;
                batch.unbondingAssets += amount;
                batch.unbondingEntries.push(_latestUnbondingEntry(target.operatorAddress, amount, completion));
                if (completion > maturity) {
                    maturity = completion;
                }
                remaining -= amount;
                _lastUndelegationBlock = block.number;
            } catch {
                continue;
            }
        }
        // Keep the remainder for a retry only if there's still delegation that could be
        // undelegated once entries free up; otherwise clear it, since it can't be met.
        batch.pendingUndelegation = (remaining != 0 && anyDelegationLeft) ? remaining : 0;
    }

    // _resumeUndelegations drains one batch's outstanding pendingUndelegation per poke (and at
    // most one undelegating batch per block, see _lastUndelegationBlock), extending the batch's
    // maturity to cover the newly queued entries.
    function _resumeUndelegations() internal returns (bool) {
        if (_lastUndelegationBlock == block.number) {
            return false;
        }
        for (uint64 batchId = nextSettlementBatchId; batchId < currentBatchId; batchId++) {
            WithdrawalBatch storage batch = withdrawalBatches[batchId];
            if (!batch.processed || batch.claimable || batch.pendingUndelegation == 0) {
                continue;
            }
            uint256 need = batch.pendingUndelegation;
            int64 maturity = _undelegateForBatch(batch, need);
            if (maturity > batch.maturityTime) {
                batch.maturityTime = maturity;
            }
            return true;
        }
        return false;
    }

    function _refreshValidatorSelection() internal returns (bool) {
        PageRequest memory request = PageRequest({
            key: validatorScanNextKey,
            offset: 0,
            limit: uint64(VALIDATOR_SCAN_PAGE_LIMIT),
            countTotal: false,
            reverse: false
        });
        (Validator[] memory pageValidators, PageResponse memory page) = STAKING_CONTRACT.validators("BOND_STATUS_BONDED", request);
        for (uint256 i = 0; i < pageValidators.length; i++) {
            Validator memory validator = pageValidators[i];
            if (validator.jailed || validator.status != BondStatus.Bonded || validator.tokens == 0) {
                continue;
            }
            // Weight every eligible validator equally, so stake spreads across the whole active
            // set instead of piling onto the highest-stake or lowest-commission ones. _stakeIdle
            // caps concentration per validator on top of this.
            _insertScanCandidate(validator.operatorAddress, SCORE_SCALE);
        }
        validatorScanNextKey = page.nextKey;
        if (page.nextKey.length == 0) {
            _applyValidatorSelection();
        }
        return pageValidators.length != 0 || page.nextKey.length == 0;
    }

    function _rebalanceValidators() internal returns (bool) {
        if (validators.length < 2) {
            return false;
        }
        _syncDelegations();
        (uint256 totalScore, uint256 selectedDelegated) = _selectedScoreAndDelegation();
        if (totalScore == 0 || selectedDelegated > totalDelegated) {
            return false;
        }

        (bool hasSource, uint256 sourceIndex, uint256 sourceSurplus) = _rebalanceSource(totalScore);
        (bool hasDest, uint256 destIndex, uint256 destDeficit) = _rebalanceDestination(totalScore);
        if (!hasSource || !hasDest || sourceIndex == destIndex) {
            return false;
        }
        uint256 amount = _min(sourceSurplus, destDeficit);
        if (amount < MIN_OPERATION_ASSETS) {
            return false;
        }

        ValidatorTarget storage src = validators[sourceIndex];
        ValidatorTarget storage dst = validators[destIndex];
        try STAKING_CONTRACT.redelegate(address(this), src.operatorAddress, dst.operatorAddress, amount) returns (int64 completion) {
            src.delegated -= amount;
            dst.delegated += amount;
            emit ValidatorRebalanced(src.operatorAddress, dst.operatorAddress, amount, completion);
            return true;
        } catch {
            return false;
        }
    }

    function _stakeIdle() internal returns (bool) {
        if (totalSupply() == 0 || totalIdleLiquid < MIN_OPERATION_ASSETS) {
            return false;
        }
        _syncDelegations();

        uint256 nav = totalDelegated + totalIdleLiquid;
        // Hold back the slash buffer from what we're willing to stake. Zero by default.
        uint256 stakeable = totalIdleLiquid;
        if (slashBufferBps != 0) {
            uint256 buffer = nav.mulDiv(slashBufferBps, BPS_DENOM, Math.Rounding.Up);
            if (totalIdleLiquid <= buffer) {
                return false;
            }
            stakeable = totalIdleLiquid - buffer;
        }

        uint256 totalScore;
        uint256 numSelected;
        for (uint256 i = 0; i < validators.length; i++) {
            if (validators[i].selected) {
                totalScore += validators[i].score;
                numSelected++;
            }
        }
        if (totalScore == 0) {
            return false;
        }

        (bool ok, uint256 destIndex, uint256 deficit) = _stakeDestination(totalScore, totalDelegated + stakeable);
        if (!ok) {
            return false;
        }
        uint256 amount = _min(stakeable, deficit == 0 ? stakeable : deficit);
        if (amount == 0) {
            return false;
        }

        // Apply the per-validator delegation cap. It's raised to at least an equal share
        // (ceil of 1/numSelected) so it won't strand funds when few validators are available;
        // with many validators it settles at MAX_DELEGATION_BPS_PER_VALIDATOR.
        ValidatorTarget storage dst = validators[destIndex];
        uint256 equalShareBps = (BPS_DENOM + numSelected - 1) / numSelected;
        uint256 capBps = equalShareBps > MAX_DELEGATION_BPS_PER_VALIDATOR ? equalShareBps : MAX_DELEGATION_BPS_PER_VALIDATOR;
        uint256 cap = nav.mulDiv(capBps, BPS_DENOM, Math.Rounding.Down);
        if (dst.delegated >= cap) {
            return false;
        }
        amount = _min(amount, cap - dst.delegated);
        if (amount == 0) {
            return false;
        }

        try STAKING_CONTRACT.delegate(address(this), dst.operatorAddress, amount) returns (bool success) {
            if (!success) {
                return false;
            }
            totalIdleLiquid -= amount;
            dst.delegated += amount;
            totalDelegated += amount;
            return true;
        } catch {
            return false;
        }
    }

    function _harvest() internal returns (bool) {
        if (validators.length == 0 || _syncDelegations() == 0) {
            return false;
        }
        uint256 beforeBalance = IERC20(asset()).balanceOf(address(this));
        try DISTRIBUTION_CONTRACT.claimRewards(address(this), uint32(validators.length)) returns (bool success) {
            if (!success) {
                return false;
            }
            uint256 afterBalance = IERC20(asset()).balanceOf(address(this));
            if (afterBalance > beforeBalance) {
                totalIdleLiquid += afterBalance - beforeBalance;
                return true;
            }
        } catch {
            return false;
        }
        return false;
    }

    function _insertScanCandidate(string memory operatorAddress, uint256 score) internal {
        if (score == 0) {
            return;
        }
        for (uint256 i = 0; i < validatorScanCandidates.length; i++) {
            if (_sameString(validatorScanCandidates[i].operatorAddress, operatorAddress)) {
                if (score > validatorScanCandidates[i].score) {
                    validatorScanCandidates[i].score = score;
                }
                return;
            }
        }
        if (validatorScanCandidates.length < targetValidatorCount) {
            validatorScanCandidates.push(ValidatorTarget({
                operatorAddress: operatorAddress,
                score: score,
                delegated: 0,
                selected: true
            }));
            return;
        }
        uint256 minIndex;
        uint256 minScore = validatorScanCandidates[0].score;
        for (uint256 i = 1; i < validatorScanCandidates.length; i++) {
            if (validatorScanCandidates[i].score < minScore) {
                minScore = validatorScanCandidates[i].score;
                minIndex = i;
            }
        }
        if (score > minScore) {
            validatorScanCandidates[minIndex] = ValidatorTarget({
                operatorAddress: operatorAddress,
                score: score,
                delegated: 0,
                selected: true
            });
        }
    }

    function _applyValidatorSelection() internal {
        _syncDelegations();
        for (uint256 i = 0; i < validators.length; i++) {
            validators[i].selected = false;
            validators[i].score = 0;
        }
        for (uint256 i = 0; i < validatorScanCandidates.length; i++) {
            (bool found, uint256 index) = _findValidator(validatorScanCandidates[i].operatorAddress);
            if (found) {
                validators[index].selected = true;
                validators[index].score = validatorScanCandidates[i].score;
            } else if (validators.length < MAX_VALIDATORS) {
                validators.push(validatorScanCandidates[i]);
            }
        }
        _pruneUnusedValidators();
        delete validatorScanCandidates;
        emit ValidatorSelectionUpdated(_selectedValidatorCount());
    }

    function _selectedScoreAndDelegation() internal view returns (uint256 totalScore, uint256 selectedDelegated) {
        for (uint256 i = 0; i < validators.length; i++) {
            if (validators[i].selected) {
                totalScore += validators[i].score;
                selectedDelegated += validators[i].delegated;
            }
        }
    }

    function _rebalanceSource(uint256 totalScore) internal view returns (bool, uint256, uint256) {
        for (uint256 i = 0; i < validators.length; i++) {
            if (!validators[i].selected && validators[i].delegated != 0) {
                return (true, i, validators[i].delegated);
            }
        }
        bool found;
        uint256 index;
        uint256 surplus;
        for (uint256 i = 0; i < validators.length; i++) {
            if (!validators[i].selected) {
                continue;
            }
            uint256 target = totalDelegated.mulDiv(validators[i].score, totalScore, Math.Rounding.Down);
            if (validators[i].delegated > target + MIN_OPERATION_ASSETS && validators[i].delegated - target > surplus) {
                found = true;
                index = i;
                surplus = validators[i].delegated - target;
            }
        }
        return (found, index, surplus);
    }

    function _rebalanceDestination(uint256 totalScore) internal view returns (bool, uint256, uint256) {
        bool found;
        uint256 index;
        uint256 deficit;
        for (uint256 i = 0; i < validators.length; i++) {
            if (!validators[i].selected) {
                continue;
            }
            uint256 target = totalDelegated.mulDiv(validators[i].score, totalScore, Math.Rounding.Down);
            if (target > validators[i].delegated && target - validators[i].delegated > deficit) {
                found = true;
                index = i;
                deficit = target - validators[i].delegated;
            }
        }
        return (found, index, deficit);
    }

    function _stakeDestination(uint256 totalScore, uint256 totalAfterStake) internal view returns (bool, uint256, uint256) {
        bool found;
        uint256 index;
        uint256 deficit;
        for (uint256 i = 0; i < validators.length; i++) {
            if (!validators[i].selected) {
                continue;
            }
            uint256 target = totalAfterStake.mulDiv(validators[i].score, totalScore, Math.Rounding.Down);
            if (target > validators[i].delegated && target - validators[i].delegated > deficit) {
                found = true;
                index = i;
                deficit = target - validators[i].delegated;
            }
        }
        if (!found) {
            for (uint256 i = 0; i < validators.length; i++) {
                if (validators[i].selected) {
                    return (true, i, 0);
                }
            }
        }
        return (found, index, deficit);
    }

    function _latestUnbondingEntry(string memory validatorAddress, uint256 amount, int64 completion) internal view returns (UnbondingEntry memory entry) {
        entry = UnbondingEntry({
            validatorAddress: validatorAddress,
            expectedAssets: amount,
            creationHeight: 0,
            completionTime: completion,
            unbondingId: 0,
            settled: false
        });
        try STAKING_CONTRACT.unbondingDelegation(address(this), validatorAddress) returns (UnbondingDelegationOutput memory output) {
            for (uint256 i = 0; i < output.entries.length; i++) {
                UnbondingDelegationEntry memory candidate = output.entries[i];
                if (candidate.completionTime == completion && candidate.unbondingId >= entry.unbondingId) {
                    entry.expectedAssets = candidate.balance;
                    entry.creationHeight = candidate.creationHeight;
                    entry.unbondingId = candidate.unbondingId;
                }
            }
        } catch {}
    }

    // _syncBatchUnbonding reconciles a batch's unbonding entries against the staking module,
    // moving each entry's realized (post-slash) proceeds into the batch's own maturedAssets
    // bucket as it completes. Returns true only once every entry has been resolved.
    function _syncBatchUnbonding(WithdrawalBatch storage batch) internal returns (bool complete) {
        complete = true;
        for (uint256 i = 0; i < batch.unbondingEntries.length; i++) {
            UnbondingEntry storage entry = batch.unbondingEntries[i];
            if (entry.settled) {
                continue;
            }
            (bool pending, uint256 currentBalance) = _pendingEntryBalance(entry);
            if (pending) {
                // Still unbonding: if a slash reduced the balance, ratchet the expected amount
                // down and take the loss out of this batch and the global reserves, so it
                // doesn't spread onto other batches.
                if (currentBalance < entry.expectedAssets) {
                    uint256 slashLoss = entry.expectedAssets - currentBalance;
                    entry.expectedAssets = currentBalance;
                    batch.unbondingAssets -= slashLoss;
                    totalWithdrawalUnbonding -= slashLoss;
                    totalReservedAssets -= slashLoss;
                }
                complete = false;
            } else if (uint256(uint64(entry.completionTime)) <= block.timestamp) {
                // Gone from the unbonding queue after maturity: the proceeds have reached the
                // vault. Move this entry's realized amount into the batch's matured bucket.
                uint256 realized = entry.expectedAssets;
                entry.settled = true;
                batch.unbondingAssets -= realized;
                batch.maturedAssets += realized;
                totalWithdrawalUnbonding -= realized;
                // totalReservedAssets doesn't change: the amount stays reserved for this batch,
                // it's just reclassified from unbonding to matured.
            } else {
                // Gone but not yet mature, so this is a transient lookup miss, not a completion.
                // Treat it as still pending so it's neither credited nor lost.
                complete = false;
            }
        }
    }

    function _pendingEntryBalance(UnbondingEntry storage entry) internal view returns (bool pending, uint256 balance) {
        try STAKING_CONTRACT.unbondingDelegation(address(this), entry.validatorAddress) returns (UnbondingDelegationOutput memory output) {
            for (uint256 i = 0; i < output.entries.length; i++) {
                UnbondingDelegationEntry memory candidate = output.entries[i];
                bool idMatch = entry.unbondingId != 0 && candidate.unbondingId == entry.unbondingId;
                bool heightMatch = entry.unbondingId == 0
                    && candidate.creationHeight == entry.creationHeight
                    && candidate.completionTime == entry.completionTime;
                if (idMatch || heightMatch) {
                    return (true, candidate.balance);
                }
            }
        } catch {}
        return (false, 0);
    }

    function _unbondingEntryCount(string memory validatorAddress) internal view returns (uint256) {
        try STAKING_CONTRACT.unbondingDelegation(address(this), validatorAddress) returns (UnbondingDelegationOutput memory output) {
            return output.entries.length;
        } catch {
            return 0;
        }
    }

    function _syncDelegations() internal returns (uint256 syncedTotal) {
        for (uint256 i = 0; i < validators.length; i++) {
            try STAKING_CONTRACT.delegation(address(this), validators[i].operatorAddress) returns (uint256, Coin memory balance) {
                validators[i].delegated = balance.amount;
                syncedTotal += balance.amount;
            } catch {
                validators[i].delegated = 0;
            }
        }
        totalDelegated = syncedTotal;
    }

    function _liveDelegatedAssets() internal view returns (uint256 assets_) {
        for (uint256 i = 0; i < validators.length; i++) {
            try STAKING_CONTRACT.delegation(address(this), validators[i].operatorAddress) returns (uint256, Coin memory balance) {
                assets_ += balance.amount;
            } catch {}
        }
    }

    function _unaccountedLiquid() internal view returns (uint256) {
        uint256 balance = IERC20(asset()).balanceOf(address(this));
        uint256 accounted = totalIdleLiquid + totalLiquidReserved;
        if (balance <= accounted) {
            return 0;
        }
        return balance - accounted;
    }

    function _findValidator(string memory operatorAddress) internal view returns (bool, uint256) {
        for (uint256 i = 0; i < validators.length; i++) {
            if (_sameString(validators[i].operatorAddress, operatorAddress)) {
                return (true, i);
            }
        }
        return (false, 0);
    }

    function _pruneUnusedValidators() internal {
        uint256 i;
        while (i < validators.length) {
            if (!validators[i].selected && validators[i].delegated == 0) {
                validators[i] = validators[validators.length - 1];
                validators.pop();
            } else {
                i++;
            }
        }
    }

    function _selectedValidatorCount() internal view returns (uint256 count) {
        for (uint256 i = 0; i < validators.length; i++) {
            if (validators[i].selected) {
                count++;
            }
        }
    }

    function _sameString(string memory a, string memory b) private pure returns (bool) {
        return keccak256(bytes(a)) == keccak256(bytes(b));
    }

    function _min(uint256 a, uint256 b) private pure returns (uint256) {
        return a < b ? a : b;
    }
}
