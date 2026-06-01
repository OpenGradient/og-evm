// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Permit.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC4626.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/math/Math.sol";

import "./precompiles/distribution/DistributionI.sol";
import "./precompiles/staking/StakingI.sol";

contract StakedBondVault is ERC4626, ERC20Permit, AccessControl, ReentrancyGuard {
    using Math for uint256;
    using SafeERC20 for IERC20;

    bytes32 public constant OPERATOR_ROLE = keccak256("OPERATOR_ROLE");
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");

    address public constant NATIVE_WERC20 = 0xD4949664cD82660AaE99bEdc034a0deA8A0bd517;
    uint256 public constant MAX_VALIDATORS = 32;
    uint16 public constant WEIGHT_SCALE = 10_000;

    struct ValidatorTarget {
        string operatorAddress;
        uint16 weight;
        uint256 delegated;
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
        int64 completionTime;
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
        uint256 settledAssets;
        uint256 claimedAssets;
        int64 maturityTime;
        UnbondingEntry[] unbondingEntries;
    }

    ValidatorTarget[] public validators;
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

    mapping(uint256 => WithdrawalRequest) public withdrawalRequests;
    mapping(uint64 => WithdrawalBatch) private withdrawalBatches;

    event ValidatorsUpdated(uint256 count);
    event WithdrawalRequested(uint256 indexed requestId, uint64 indexed batchId, address indexed owner, address receiver, uint256 shares);
    event WithdrawalBatchProcessed(uint64 indexed batchId, uint256 shares, uint256 estimatedAssets, int64 maturityTime);
    event WithdrawalBatchSettled(uint64 indexed batchId, uint256 settledAssets);
    event WithdrawalClaimed(uint256 indexed requestId, uint64 indexed batchId, address indexed receiver, uint256 assets);
    event Poked(address indexed caller, uint256 ops);

    modifier noNativeValue() {
        require(msg.value == 0, "unexpected value");
        _;
    }

    constructor(address admin, string memory name_, string memory symbol_)
        ERC20(name_, symbol_)
        ERC20Permit(name_)
        ERC4626(IERC20Metadata(NATIVE_WERC20))
    {
        require(admin != address(0), "admin zero");
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
        uint256 gross = IERC20(asset()).balanceOf(address(this)) + _liveDelegatedAssets() + totalWithdrawalUnbonding;
        if (gross <= totalReservedAssets) {
            return 0;
        }
        return gross - totalReservedAssets;
    }

    function liveDelegatedAssets() external view returns (uint256) {
        return _liveDelegatedAssets();
    }

    function syncDelegations() external payable noNativeValue nonReentrant returns (uint256) {
        return _syncDelegations();
    }

    function decimals() public view override(ERC20, ERC4626) returns (uint8) {
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
        revert("use depositNative");
    }

    function mint(uint256, address) public pure override returns (uint256) {
        revert("use depositNative");
    }

    function depositNative(address receiver) external payable nonReentrant returns (uint256 shares) {
        require(!depositsPaused, "deposits paused");
        require(receiver != address(0), "receiver zero");
        require(msg.value > 0, "zero assets");
        _harvest();

        uint256 assets = msg.value;
        uint256 supply = totalSupply();
        if (supply == 0) {
            shares = assets;
        } else {
            uint256 assetsAfter = totalAssets();
            uint256 assetsBefore = assetsAfter > assets ? assetsAfter - assets : 0;
            shares = assets.mulDiv(supply + 1, assetsBefore + 1, Math.Rounding.Down);
        }
        require(shares > 0, "zero shares");
        _mint(receiver, shares);
        emit Deposit(msg.sender, receiver, assets, shares);
    }

    function withdraw(uint256, address, address) public pure override returns (uint256) {
        revert("use requestRedeem");
    }

    function redeem(uint256, address, address) public pure override returns (uint256) {
        revert("use requestRedeem");
    }

    function requestRedeem(uint256 shares, address receiver, address owner) external payable noNativeValue nonReentrant returns (uint256 requestId) {
        require(!withdrawalsPaused, "withdrawals paused");
        require(shares > 0, "zero shares");
        require(receiver != address(0), "receiver zero");
        require(owner != address(0), "owner zero");
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
        require(request.owner != address(0), "unknown request");

        WithdrawalBatch storage batch = withdrawalBatches[request.batchId];
        require(batch.claimable, "not claimable");
        address payout = receiver == address(0) ? request.receiver : receiver;
        require(payout == request.receiver, "wrong receiver");

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
        require(!pokePaused, "poke paused");
        if (maxOps == 0) {
            return 0;
        }
        if (_settleMatureBatch()) {
            ops++;
        }
        if (ops < maxOps && _processCurrentBatch()) {
            ops++;
        }
        if (ops < maxOps && _harvest()) {
            ops++;
        }
        if (ops < maxOps && _stakeIdle()) {
            ops++;
        }
        pokeCount++;
        lastPokeOps = ops;
        emit Poked(msg.sender, ops);
    }

    function setPaused(bool deposits, bool withdrawals, bool scheduler) external payable noNativeValue onlyRole(PAUSER_ROLE) {
        depositsPaused = deposits;
        withdrawalsPaused = withdrawals;
        pokePaused = scheduler;
    }

    function setValidators(string[] calldata operatorAddresses, uint16[] calldata weights) external payable noNativeValue onlyRole(OPERATOR_ROLE) {
        require(operatorAddresses.length == weights.length, "length mismatch");
        require(operatorAddresses.length <= MAX_VALIDATORS, "too many validators");

        uint256 totalWeight;
        uint256 oldDelegated = _syncDelegations();
        delete validators;
        for (uint256 i = 0; i < operatorAddresses.length; i++) {
            require(bytes(operatorAddresses[i]).length != 0, "empty validator");
            require(weights[i] > 0, "zero weight");
            STAKING_CONTRACT.delegation(address(this), operatorAddresses[i]);
            bytes32 operatorHash = keccak256(bytes(operatorAddresses[i]));
            for (uint256 j = 0; j < i; j++) {
                require(operatorHash != keccak256(bytes(operatorAddresses[j])), "duplicate validator");
            }
            totalWeight += weights[i];
            validators.push(ValidatorTarget({
                operatorAddress: operatorAddresses[i],
                weight: weights[i],
                delegated: 0
            }));
        }
        require(totalWeight == WEIGHT_SCALE || operatorAddresses.length == 0, "bad weights");
        require(oldDelegated == 0, "delegations active");
        emit ValidatorsUpdated(operatorAddresses.length);
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
            int64 maturityTime
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
            batch.maturityTime
        );
    }

    function validatorCount() external view returns (uint256) {
        return validators.length;
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
        batch.open = false;
        batch.processed = true;
        batch.estimatedAssets = assetsOut;
        _burn(address(this), batch.totalShares);

        uint256 liquid = _min(_freeLiquid(), assetsOut);
        if (liquid != 0) {
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
        if (batch.unbondingAssets == 0) {
            batch.claimable = true;
            batch.settledAssets = batch.liquidAssets;
        }
        emit WithdrawalBatchProcessed(currentBatchId, batch.totalShares, assetsOut, maturity);

        currentBatchId += 1;
        withdrawalBatches[currentBatchId].open = true;
        return true;
    }

    function _settleMatureBatch() internal returns (bool) {
        for (uint64 batchId = nextSettlementBatchId; batchId < currentBatchId; batchId++) {
            WithdrawalBatch storage batch = withdrawalBatches[batchId];
            if (!batch.processed || batch.claimable || batch.unbondingAssets == 0) {
                nextSettlementBatchId = batchId + 1;
                continue;
            }
            if (block.timestamp < uint256(uint64(batch.maturityTime))) {
                nextSettlementBatchId = batchId;
                return false;
            }

            uint256 expected = batch.unbondingAssets;
            uint256 matured = expected;
            uint256 requiredLiquid = totalLiquidReserved + expected;
            uint256 balance = IERC20(asset()).balanceOf(address(this));
            if (balance < requiredLiquid) {
                uint256 shortfall = requiredLiquid - balance;
                matured = expected > shortfall ? expected - shortfall : 0;
            }

            totalWithdrawalUnbonding -= expected;
            totalReservedAssets = totalReservedAssets - expected + matured;
            totalLiquidReserved += matured;
            batch.settledAssets = batch.liquidAssets + matured;
            batch.claimable = true;
            nextSettlementBatchId = batchId + 1;
            emit WithdrawalBatchSettled(batchId, batch.settledAssets);
            return true;
        }
        return false;
    }

    function _undelegateForBatch(WithdrawalBatch storage batch, uint256 assetsNeeded) internal returns (int64 maturity) {
        require(validators.length != 0, "no validators");
        _syncDelegations();
        uint256 remaining = assetsNeeded;
        for (uint256 i = 0; i < validators.length && remaining != 0; i++) {
            ValidatorTarget storage target = validators[i];
            uint256 amount = _min(target.delegated, remaining);
            if (amount == 0) {
                continue;
            }
            int64 completion = STAKING_CONTRACT.undelegate(address(this), target.operatorAddress, amount);
            target.delegated -= amount;
            totalDelegated -= amount;
            totalWithdrawalUnbonding += amount;
            totalReservedAssets += amount;
            batch.unbondingAssets += amount;
            batch.unbondingEntries.push(UnbondingEntry({
                validatorAddress: target.operatorAddress,
                expectedAssets: amount,
                completionTime: completion
            }));
            if (completion > maturity) {
                maturity = completion;
            }
            remaining -= amount;
        }
        require(remaining == 0, "insufficient delegated assets");
    }

    function _stakeIdle() internal returns (bool) {
        if (totalSupply() == 0) {
            return false;
        }
        uint256 free = _freeLiquid();
        if (free == 0 || validators.length == 0) {
            return false;
        }
        _syncDelegations();
        uint256 remaining = free;
        for (uint256 i = 0; i < validators.length && remaining != 0; i++) {
            uint256 amount = i + 1 == validators.length
                ? remaining
                : free.mulDiv(validators[i].weight, WEIGHT_SCALE, Math.Rounding.Down);
            if (amount == 0) {
                continue;
            }
            bool success = STAKING_CONTRACT.delegate(address(this), validators[i].operatorAddress, amount);
            require(success, "delegate failed");
            validators[i].delegated += amount;
            totalDelegated += amount;
            remaining -= amount;
        }
        return true;
    }

    function _harvest() internal returns (bool) {
        if (validators.length == 0 || _syncDelegations() == 0) {
            return false;
        }
        return DISTRIBUTION_CONTRACT.claimRewards(address(this), uint32(validators.length));
    }

    function _syncDelegations() internal returns (uint256 syncedTotal) {
        for (uint256 i = 0; i < validators.length; i++) {
            (, Coin memory balance) = STAKING_CONTRACT.delegation(address(this), validators[i].operatorAddress);
            validators[i].delegated = balance.amount;
            syncedTotal += balance.amount;
        }
        totalDelegated = syncedTotal;
    }

    function _liveDelegatedAssets() internal view returns (uint256 assets_) {
        for (uint256 i = 0; i < validators.length; i++) {
            (, Coin memory balance) = STAKING_CONTRACT.delegation(address(this), validators[i].operatorAddress);
            assets_ += balance.amount;
        }
    }

    function _freeLiquid() internal view returns (uint256) {
        uint256 balance = IERC20(asset()).balanceOf(address(this));
        if (balance <= totalLiquidReserved) {
            return 0;
        }
        return balance - totalLiquidReserved;
    }

    function _min(uint256 a, uint256 b) private pure returns (uint256) {
        return a < b ? a : b;
    }
}
