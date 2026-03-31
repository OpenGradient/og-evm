// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity >=0.8.17;

import "../precompiles/erc20/IERC20.sol";
import "../precompiles/staking/StakingI.sol" as staking;
import "../precompiles/distribution/DistributionI.sol" as distribution;

/// @title CommunityPool
/// @notice Pooled staking contract with internal ownership units.
/// @dev
/// - Users deposit `bondToken` and receive pool units (`unitsOf`) representing proportional ownership.
/// - Principal accounting separates:
///   (a) liquid principal available for staking/deposit pricing,
///   (b) staked principal tracked in `totalStaked`,
///   (c) pending unbonding reserve, and
///   (d) matured withdraw reserve reserved for claims.
/// - `totalStaked` is accounting-only and can drift from real chain state (e.g. slashing), so
///   owner can reconcile it via `syncTotalStaked`.
/// - Withdrawals are async and staked-only in this MVP: requests undelegate first, then users claim after maturity.
contract CommunityPool {
    /// @dev Native token contract used for deposits/withdrawals.
    IERC20 public immutable bondToken;
    /// @dev Fixed-point precision used for reward index math.
    uint256 public constant PRECISION = 1e18;

    address public owner;
    /// @dev Optional automation caller allowed to trigger periodic stake/harvest.
    address public automationCaller;
    /// @dev Total ownership units minted by the pool.
    uint256 public totalUnits;
    /// @dev Accounting value of delegated principal (not auto-reconciled with staking state).
    uint256 public totalStaked;
    /// @dev Accumulated rewards per ownership unit (scaled by PRECISION).
    uint256 public accRewardPerUnit;
    /// @dev Total liquid rewards reserved for reward claims.
    uint256 public rewardReserve;
    /// @dev Principal liquid explicitly tracked as stake-eligible balance.
    uint256 public stakeablePrincipalLedger;
    /// @dev Principal requested for withdraw and not yet moved into matured-withdraw reserve.
    uint256 public pendingWithdrawReserve;
    /// @dev Liquid principal reserved for matured-but-unclaimed withdraw requests.
    uint256 public maturedWithdrawReserve;
    /// @dev Monotonic identifier for withdraw requests.
    uint256 public nextWithdrawRequestId = 1;
    uint32 public maxRetrieve;
    uint32 public maxValidators;
    uint256 public minStakeAmount;

    /// @dev Units held per user. User ownership fraction = unitsOf[user] / totalUnits.
    mapping(address => uint256) public unitsOf;
    /// @dev User reward checkpoint for index accounting.
    mapping(address => uint256) public rewardDebt;
    /// @dev Async principal withdraw requests keyed by request id.
    mapping(uint256 => WithdrawRequest) public withdrawRequests;

    /// @dev Minimal reentrancy guard state (0=not entered, 1=entered).
    uint256 private _entered;

    struct WithdrawRequest {
        address owner;
        uint256 amountOut;
        uint64 maturityTime;
        bool reserveMoved;
        bool claimed;
    }

    error Unauthorized();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidUnits();
    error InvalidConfig();
    error InsufficientLiquid(uint256 requested, uint256 available);
    error TokenTransferFailed();
    error TokenTransferFromFailed();
    error HarvestFailed();
    error ZeroMintedUnits();
    error RequestAlreadyClaimed();
    error RequestNotMatured(uint64 maturityTime, uint64 currentTime);
    error InvalidRequest();
    error InvalidCompletionTime(int64 completionTime, uint64 currentTime);
    error UnexpectedUndelegatedAmount(uint256 requested, uint256 undelegated);
    error RewardReserveInvariantViolation(uint256 rewardReserve, uint256 liquidBalance);
    error LiquidReserveInvariantViolation(uint256 reservedAmount, uint256 liquidBalance);
    error StakeablePrincipalInvariantViolation(uint256 accountedLiquid, uint256 liquidBalance);

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event AutomationCallerUpdated(address indexed previousCaller, address indexed newCaller);
    event ConfigUpdated(uint32 maxRetrieve, uint32 maxValidators, uint256 minStakeAmount);
    event Deposit(address indexed user, uint256 amount, uint256 mintedUnits, uint256 totalUnitsAfter);
    event Stake(uint256 liquidBefore, uint256 delegatedAmount, uint256 validatorsCount, uint256 totalStakedAfter);
    event Harvest(uint256 liquidBefore, uint256 liquidAfter, uint256 harvestedAmount);
    event RewardIndexUpdated(uint256 harvestedAmount, uint256 accRewardPerUnit, uint256 rewardReserve);
    event RewardsClaimed(address indexed user, uint256 amount);
    event WithdrawRequested(
        address indexed user,
        uint256 indexed requestId,
        uint256 units,
        uint256 amountOut,
        uint64 maturityTime
    );
    event WithdrawClaimed(address indexed user, uint256 indexed requestId, uint256 amountOut);
    event WithdrawReserveMoved(
        uint256 indexed requestId,
        uint256 amountOut,
        uint256 pendingWithdrawReserveAfter,
        uint256 maturedWithdrawReserveAfter
    );
    event TotalStakedSynced(uint256 previousTotalStaked, uint256 newTotalStaked);

    modifier onlyOwner() {
        if (msg.sender != owner) {
            revert Unauthorized();
        }
        _;
    }

    modifier onlyAutomationOrOwner() {
        if (msg.sender != owner && msg.sender != automationCaller) {
            revert Unauthorized();
        }
        _;
    }

    modifier nonReentrant() {
        require(_entered == 0, "reentrancy");
        _entered = 1;
        _;
        _entered = 0;
    }

    constructor(
        address bondToken_,
        uint32 maxRetrieve_,
        uint32 maxValidators_,
        uint256 minStakeAmount_,
        address owner_
    ) {
        if (bondToken_ == address(0) || owner_ == address(0)) {
            revert InvalidAddress();
        }
        if (maxValidators_ == 0) {
            revert InvalidConfig();
        }

        bondToken = IERC20(bondToken_);
        maxRetrieve = maxRetrieve_;
        maxValidators = maxValidators_;
        minStakeAmount = minStakeAmount_;
        owner = owner_;
        automationCaller = owner_;
    }

    /// @notice Transfers owner privileges to a new address.
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) {
            revert InvalidAddress();
        }

        address previousOwner = owner;
        owner = newOwner;
        emit OwnershipTransferred(previousOwner, newOwner);
    }

    /// @notice Sets the automation caller allowed to run stake/harvest besides owner.
    function setAutomationCaller(address newAutomationCaller) external onlyOwner {
        if (newAutomationCaller == address(0)) {
            revert InvalidAddress();
        }
        address previousCaller = automationCaller;
        automationCaller = newAutomationCaller;
        emit AutomationCallerUpdated(previousCaller, newAutomationCaller);
    }

    /// @notice Updates operational parameters used by stake/harvest.
    /// @param newMaxRetrieve Max validator rewards to claim per harvest call.
    /// @param newMaxValidators Max bonded validators to target in one stake call.
    /// @param newMinStakeAmount Minimum liquid threshold required to run `stake`.
    function setConfig(
        uint32 newMaxRetrieve,
        uint32 newMaxValidators,
        uint256 newMinStakeAmount
    ) external onlyOwner {
        if (newMaxValidators == 0) {
            revert InvalidConfig();
        }

        maxRetrieve = newMaxRetrieve;
        maxValidators = newMaxValidators;
        minStakeAmount = newMinStakeAmount;
        emit ConfigUpdated(newMaxRetrieve, newMaxValidators, newMinStakeAmount);
    }

    /// @notice Manual reconciliation hook for staking accounting drift.
    /// @dev Intended for operational correction after slashing/reconciliation.
    function syncTotalStaked(uint256 newTotalStaked) external onlyOwner {
        uint256 previous = totalStaked;
        totalStaked = newTotalStaked;
        emit TotalStakedSynced(previous, newTotalStaked);
    }

    /// @notice Current liquid token balance owned by the contract.
    function liquidBalance() public view returns (uint256) {
        return bondToken.balanceOf(address(this));
    }

    /// @notice Current liquid principal available for stake/deposit pricing.
    /// @dev Ledger-driven value; independent from raw balance deltas.
    function principalLiquid() public view returns (uint256) {
        return stakeablePrincipalLedger;
    }

    /// @notice Total principal assets used for ownership pricing.
    /// @dev In strict staked-withdraw mode this tracks liquid principal plus currently staked principal.
    function principalAssets() public view returns (uint256) {
        return principalLiquid() + totalStaked;
    }

    /// @notice Total principal currently committed to pending or matured async withdraw requests.
    function totalWithdrawCommitments() external view returns (uint256) {
        return pendingWithdrawReserve + maturedWithdrawReserve;
    }

    /// @notice Returns 1e18-scaled token value per ownership unit.
    function pricePerUnit() external view returns (uint256) {
        if (totalUnits == 0) {
            return 1e18;
        }
        return (principalAssets() * 1e18) / totalUnits;
    }

    /// @notice Deposits tokens and mints proportional pool units.
    /// @dev
    /// - First deposit mints 1:1 units.
    /// - Later deposits mint: floor(amount * totalUnits / principalAssets).
    /// - Floor rounding avoids over-minting; tiny deposits that would mint 0 units revert.
    function deposit(uint256 amount) external nonReentrant returns (uint256 mintedUnits) {
        if (amount == 0) {
            revert InvalidAmount();
        }

        _claimPendingRewards(msg.sender);

        uint256 assetsBefore = principalAssets();
        if (totalUnits == 0 || assetsBefore == 0) {
            mintedUnits = amount;
        } else {
            mintedUnits = (amount * totalUnits) / assetsBefore;
        }

        if (mintedUnits == 0) {
            revert ZeroMintedUnits();
        }

        if (!bondToken.transferFrom(msg.sender, address(this), amount)) {
            revert TokenTransferFromFailed();
        }

        stakeablePrincipalLedger += amount;
        unitsOf[msg.sender] += mintedUnits;
        totalUnits += mintedUnits;
        rewardDebt[msg.sender] = (unitsOf[msg.sender] * accRewardPerUnit) / PRECISION;
        _assertReserveInvariant();

        emit Deposit(msg.sender, amount, mintedUnits, totalUnits);
    }

    /// @notice Requests an async staked-principal withdrawal by burning ownership units now.
    /// @dev
    /// - Withdrawal sizing is based only on `totalStaked` (strict unbonding-only model).
    /// - Final payout happens via `claimWithdraw` after maturity.
    /// - Undelegation source validators are selected internally by staking precompile.
    function withdraw(uint256 userUnits) external nonReentrant returns (uint256 requestId) {
        if (userUnits == 0) {
            revert InvalidUnits();
        }

        _claimPendingRewards(msg.sender);

        uint256 userBalanceUnits = unitsOf[msg.sender];
        if (userUnits > userBalanceUnits || totalUnits == 0) {
            revert InvalidUnits();
        }

        uint256 amountOut = (userUnits * totalStaked) / totalUnits;
        if (amountOut == 0) {
            revert InvalidAmount();
        }
        uint64 currentTime = uint64(block.timestamp);
        uint256 undelegatedAmount;
        int64 completionTime;
        (undelegatedAmount,, completionTime) = staking.STAKING_CONTRACT.undelegateFromBondedValidators(
            address(this),
            amountOut,
            maxValidators
        );
        if (undelegatedAmount != amountOut) {
            revert UnexpectedUndelegatedAmount(amountOut, undelegatedAmount);
        }
        if (completionTime <= 0 || uint64(completionTime) < currentTime) {
            revert InvalidCompletionTime(completionTime, currentTime);
        }
        uint64 maturityTime = uint64(completionTime);

        unitsOf[msg.sender] = userBalanceUnits - userUnits;
        totalUnits -= userUnits;
        totalStaked -= amountOut;
        pendingWithdrawReserve += amountOut;
        rewardDebt[msg.sender] = (unitsOf[msg.sender] * accRewardPerUnit) / PRECISION;
        _assertReserveInvariant();

        requestId = nextWithdrawRequestId++;
        withdrawRequests[requestId] = WithdrawRequest({
            owner: msg.sender,
            amountOut: amountOut,
            maturityTime: maturityTime,
            reserveMoved: false,
            claimed: false
        });

        emit WithdrawRequested(msg.sender, requestId, userUnits, amountOut, maturityTime);
    }

    /// @notice Claims a matured async withdrawal request.
    /// @dev
    /// - On first successful matured claim path, request amount is moved from
    ///   `pendingWithdrawReserve` to `maturedWithdrawReserve`.
    /// - Payout consumes `maturedWithdrawReserve` and transfers principal to request owner.
    function claimWithdraw(uint256 requestId) external nonReentrant returns (uint256 amountOut) {
        WithdrawRequest storage request = withdrawRequests[requestId];
        if (request.owner == address(0)) {
            revert InvalidRequest();
        }
        if (request.owner != msg.sender) {
            revert Unauthorized();
        }
        if (request.claimed) {
            revert RequestAlreadyClaimed();
        }

        uint64 currentTime = uint64(block.timestamp);
        if (currentTime < request.maturityTime) {
            revert RequestNotMatured(request.maturityTime, currentTime);
        }

        if (!request.reserveMoved) {
            if (request.amountOut > pendingWithdrawReserve) {
                revert InsufficientLiquid(request.amountOut, pendingWithdrawReserve);
            }
            pendingWithdrawReserve -= request.amountOut;
            maturedWithdrawReserve += request.amountOut;
            request.reserveMoved = true;
            emit WithdrawReserveMoved(requestId, request.amountOut, pendingWithdrawReserve, maturedWithdrawReserve);
        }

        request.claimed = true;
        amountOut = request.amountOut;
        if (amountOut > maturedWithdrawReserve) {
            revert InsufficientLiquid(amountOut, maturedWithdrawReserve);
        }
        uint256 liquidPrincipalBefore = liquidBalance() - rewardReserve;
        if (amountOut > liquidPrincipalBefore) {
            revert InsufficientLiquid(amountOut, liquidPrincipalBefore);
        }
        maturedWithdrawReserve -= amountOut;
        if (!bondToken.transfer(msg.sender, amountOut)) {
            revert TokenTransferFailed();
        }
        _assertReserveInvariant();

        emit WithdrawClaimed(msg.sender, requestId, amountOut);
    }

    /// @notice Delegates available principal liquid to bonded validators via staking precompile.
    /// @dev Callable by owner or automation caller; uses one precompile call for bonded-set selection and equal split.
    function stake() external nonReentrant onlyAutomationOrOwner returns (uint256 delegatedAmount) {
        uint256 liquidBefore = stakeablePrincipalLedger;
        if (liquidBefore < minStakeAmount) {
            return 0;
        }
        uint32 validatorsCount;
        (delegatedAmount, validatorsCount) = staking.STAKING_CONTRACT.delegateToBondedValidators(
            address(this),
            liquidBefore,
            maxValidators
        );

        stakeablePrincipalLedger -= delegatedAmount;
        totalStaked += delegatedAmount;
        _assertReserveInvariant();
        emit Stake(liquidBefore, delegatedAmount, uint256(validatorsCount), totalStaked);
    }

    /// @notice Claims staking rewards to this contract's liquid balance.
    /// @dev Callable by owner or automation caller; does not modify `totalStaked` because rewards are liquid yield, not principal.
    function harvest() external nonReentrant onlyAutomationOrOwner returns (uint256 harvestedAmount) {
        uint256 liquidBefore = liquidBalance();
        bool success = distribution.DISTRIBUTION_CONTRACT.claimRewards(
            address(this),
            maxRetrieve
        );
        if (!success) {
            revert HarvestFailed();
        }

        uint256 liquidAfter = liquidBalance();
        harvestedAmount = liquidAfter > liquidBefore ? liquidAfter - liquidBefore : 0;
        if (harvestedAmount > 0) {
            rewardReserve += harvestedAmount;
            if (totalUnits > 0) {
                accRewardPerUnit += (harvestedAmount * PRECISION) / totalUnits;
            }
            emit RewardIndexUpdated(harvestedAmount, accRewardPerUnit, rewardReserve);
        }
        _assertReserveInvariant();
        emit Harvest(liquidBefore, liquidAfter, harvestedAmount);
    }

    /// @notice Claims caller's accrued rewards from the reward reserve.
    /// @dev Uses reward index accounting and does not trigger distribution precompile calls.
    function claimRewards() external nonReentrant returns (uint256 claimedAmount) {
        claimedAmount = _claimPendingRewards(msg.sender);
    }

    function _claimPendingRewards(address user) internal returns (uint256 claimedAmount) {
        uint256 accumulated = (unitsOf[user] * accRewardPerUnit) / PRECISION;
        uint256 debt = rewardDebt[user];
        rewardDebt[user] = accumulated;
        if (accumulated <= debt) {
            return 0;
        }

        claimedAmount = accumulated - debt;
        rewardReserve -= claimedAmount;
        if (!bondToken.transfer(user, claimedAmount)) {
            revert TokenTransferFailed();
        }
        _assertReserveInvariant();

        emit RewardsClaimed(user, claimedAmount);
    }

    function _assertReserveInvariant() internal view {
        uint256 liquid = liquidBalance();
        if (rewardReserve > liquid) {
            revert RewardReserveInvariantViolation(rewardReserve, liquid);
        }
        // Only liquid reserves are constrained by current liquid balance.
        // Pending withdraw reserve is intentionally excluded because it is not yet
        // moved to the matured (liquid, claim-ready) reserve.
        uint256 reserved = rewardReserve + maturedWithdrawReserve;
        if (reserved > liquid) {
            revert LiquidReserveInvariantViolation(reserved, liquid);
        }
        uint256 accountedLiquid = stakeablePrincipalLedger + rewardReserve + maturedWithdrawReserve;
        if (accountedLiquid > liquid) {
            revert StakeablePrincipalInvariantViolation(accountedLiquid, liquid);
        }
    }

}

