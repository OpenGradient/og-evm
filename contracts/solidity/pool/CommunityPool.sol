// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity >=0.8.17;

import "../precompiles/erc20/IERC20.sol";
import "../precompiles/staking/StakingI.sol" as staking;
import "../precompiles/distribution/DistributionI.sol" as distribution;
import "../precompiles/bech32/Bech32I.sol";

/// @title CommunityPool
/// @notice Pooled staking contract with internal ownership units.
/// @dev
/// - Users deposit `bondToken` and receive pool units (`unitsOf`) representing proportional ownership.
/// - Pool assets are tracked as: liquid token balance + `totalStaked` accounting value.
/// - `totalStaked` is accounting-only and can drift from real chain state (e.g. slashing), so
///   owner can reconcile it via `syncTotalStaked`.
/// - Withdrawals are liquid-only in this MVP: if liquid funds are insufficient, withdrawal reverts.
contract CommunityPool {
    string private constant BONDED_STATUS = "BOND_STATUS_BONDED";

    /// @dev Native token contract used for deposits/withdrawals.
    IERC20 public immutable bondToken;

    address public owner;
    /// @dev Total ownership units minted by the pool.
    uint256 public totalUnits;
    /// @dev Accounting value of delegated principal (not auto-reconciled with staking state).
    uint256 public totalStaked;
    uint32 public maxRetrieve;
    uint32 public maxValidators;
    uint256 public minStakeAmount;
    string public validatorPrefix;

    /// @dev Units held per user. User ownership fraction = unitsOf[user] / totalUnits.
    mapping(address => uint256) public unitsOf;

    /// @dev Minimal reentrancy guard state (0=not entered, 1=entered).
    uint256 private _entered;

    error Unauthorized();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidUnits();
    error InvalidConfig();
    error NoValidators();
    error InsufficientLiquid(uint256 requested, uint256 available);
    error TokenTransferFailed();
    error TokenTransferFromFailed();
    error DelegateFailed(string validator, uint256 amount);
    error HarvestFailed();
    error ZeroMintedUnits();

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event ConfigUpdated(uint32 maxRetrieve, uint32 maxValidators, uint256 minStakeAmount);
    event Deposit(address indexed user, uint256 amount, uint256 mintedUnits, uint256 totalUnitsAfter);
    event Withdraw(address indexed user, uint256 burnedUnits, uint256 amountOut, uint256 totalUnitsAfter);
    event StakeDelegated(string validator, uint256 amount);
    event Stake(uint256 liquidBefore, uint256 delegatedAmount, uint256 validatorsCount, uint256 totalStakedAfter);
    event Harvest(uint256 liquidBefore, uint256 liquidAfter, uint256 harvestedAmount);
    event TotalStakedSynced(uint256 previousTotalStaked, uint256 newTotalStaked);
    event ValidatorPrefixUpdated(string previousPrefix, string newPrefix);

    modifier onlyOwner() {
        if (msg.sender != owner) {
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
        address owner_,
        string memory validatorPrefix_
    ) {
        if (bondToken_ == address(0) || owner_ == address(0)) {
            revert InvalidAddress();
        }
        if (maxValidators_ == 0) {
            revert InvalidConfig();
        }
        if (bytes(validatorPrefix_).length == 0) {
            revert InvalidConfig();
        }

        bondToken = IERC20(bondToken_);
        maxRetrieve = maxRetrieve_;
        maxValidators = maxValidators_;
        minStakeAmount = minStakeAmount_;
        owner = owner_;
        validatorPrefix = validatorPrefix_;
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

    /// @notice Updates the validator bech32 prefix used by stake conversion.
    function setValidatorPrefix(string calldata newPrefix) external onlyOwner {
        if (bytes(newPrefix).length == 0) {
            revert InvalidConfig();
        }
        string memory previous = validatorPrefix;
        validatorPrefix = newPrefix;
        emit ValidatorPrefixUpdated(previous, newPrefix);
    }

    /// @notice Current liquid token balance owned by the contract.
    function liquidBalance() public view returns (uint256) {
        return bondToken.balanceOf(address(this));
    }

    /// @notice Total pool assets used for share pricing.
    /// @dev This excludes unclaimed rewards until `harvest` is called.
    function poolAssets() public view returns (uint256) {
        return liquidBalance() + totalStaked;
    }

    /// @notice Returns 1e18-scaled token value per ownership unit.
    function pricePerUnit() external view returns (uint256) {
        if (totalUnits == 0) {
            return 1e18;
        }
        return (poolAssets() * 1e18) / totalUnits;
    }

    /// @notice Deposits tokens and mints proportional pool units.
    /// @dev
    /// - First deposit mints 1:1 units.
    /// - Later deposits mint: floor(amount * totalUnits / poolAssets).
    /// - Floor rounding avoids over-minting; tiny deposits that would mint 0 units revert.
    function deposit(uint256 amount) external nonReentrant returns (uint256 mintedUnits) {
        if (amount == 0) {
            revert InvalidAmount();
        }

        uint256 assetsBefore = poolAssets();
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

        unitsOf[msg.sender] += mintedUnits;
        totalUnits += mintedUnits;

        emit Deposit(msg.sender, amount, mintedUnits, totalUnits);
    }

    /// @notice Burns user units and withdraws proportional assets from liquid balance.
    /// @dev Reverts with `InsufficientLiquid` if the proportional claim exceeds liquid funds.
    function withdraw(uint256 userUnits) external nonReentrant returns (uint256 amountOut) {
        if (userUnits == 0) {
            revert InvalidUnits();
        }

        uint256 userBalanceUnits = unitsOf[msg.sender];
        if (userUnits > userBalanceUnits || totalUnits == 0) {
            revert InvalidUnits();
        }

        amountOut = (userUnits * poolAssets()) / totalUnits;
        uint256 liquid = liquidBalance();
        if (amountOut > liquid) {
            revert InsufficientLiquid(amountOut, liquid);
        }

        unitsOf[msg.sender] = userBalanceUnits - userUnits;
        totalUnits -= userUnits;

        if (!bondToken.transfer(msg.sender, amountOut)) {
            revert TokenTransferFailed();
        }

        emit Withdraw(msg.sender, userUnits, amountOut, totalUnits);
    }

    /// @notice Delegates available liquid to bonded validators discovered on-chain.
    /// @dev
    /// - Uses staking precompile `validators(BOND_STATUS_BONDED, pageRequest)`.
    /// - Splits liquid evenly, and assigns remainder (+1) to first validators deterministically.
    /// - Increases `totalStaked` by delegated amount as accounting update.
    function stake() external nonReentrant returns (uint256 delegatedAmount) {
        uint256 liquidBefore = liquidBalance();
        if (liquidBefore < minStakeAmount) {
            return 0;
        }

        string[] memory validators = _getBondedValidators(maxValidators);
        uint256 validatorCount = validators.length;
        uint256 perValidator = liquidBefore / validatorCount;
        uint256 remainder = liquidBefore % validatorCount;

        for (uint256 i = 0; i < validatorCount; i++) {
            uint256 amount = perValidator;
            if (i < remainder) {
                amount += 1;
            }
            if (amount == 0) {
                continue;
            }

            address validatorHex = _parseHexAddress(validators[i]);
            string memory validatorBech32 = BECH32_CONTRACT.hexToBech32(
                validatorHex,
                validatorPrefix
            );

            bool success = staking.STAKING_CONTRACT.delegate(
                address(this),
                validatorBech32,
                amount
            );
            if (!success) {
                revert DelegateFailed(validatorBech32, amount);
            }

            delegatedAmount += amount;
            emit StakeDelegated(validatorBech32, amount);
        }

        totalStaked += delegatedAmount;
        emit Stake(liquidBefore, delegatedAmount, validatorCount, totalStaked);
    }

    /// @notice Claims staking rewards to this contract's liquid balance.
    /// @dev Does not modify `totalStaked` because rewards are liquid yield, not principal.
    function harvest() external nonReentrant returns (uint256 harvestedAmount) {
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
        emit Harvest(liquidBefore, liquidAfter, harvestedAmount);
    }

    /// @dev Reads bonded validators from staking precompile with pagination.
    /// Stops when cap is reached or there is no next page.
    function _getBondedValidators(uint32 cap) internal view returns (string[] memory out) {
        if (cap == 0) {
            revert InvalidConfig();
        }

        string[] memory tmp = new string[](cap);
        uint256 count = 0;
        bytes memory pageKey;

        while (count < cap) {
            staking.PageRequest memory page = staking.PageRequest({
                key: pageKey,
                offset: 0,
                limit: uint64(cap - count),
                countTotal: false,
                reverse: false
            });

            (
                staking.Validator[] memory validatorsPage,
                staking.PageResponse memory pageResponse
            ) = staking.STAKING_CONTRACT.validators(BONDED_STATUS, page);

            uint256 pageLen = validatorsPage.length;
            if (pageLen == 0) {
                break;
            }

            for (uint256 i = 0; i < pageLen && count < cap; i++) {
                tmp[count] = validatorsPage[i].operatorAddress;
                count++;
            }

            if (pageResponse.nextKey.length == 0) {
                break;
            }
            pageKey = pageResponse.nextKey;
        }

        if (count == 0) {
            revert NoValidators();
        }

        out = new string[](count);
        for (uint256 i = 0; i < count; i++) {
            out[i] = tmp[i];
        }
    }

    function _parseHexAddress(string memory value) internal pure returns (address out) {
        bytes memory str = bytes(value);
        if (str.length != 42 || str[0] != "0" || (str[1] != "x" && str[1] != "X")) {
            revert InvalidAddress();
        }

        uint160 result = 0;
        for (uint256 i = 2; i < 42; i++) {
            uint8 c = uint8(str[i]);
            uint8 nibble;
            if (c >= 48 && c <= 57) {
                nibble = c - 48;
            } else if (c >= 65 && c <= 70) {
                nibble = c - 55;
            } else if (c >= 97 && c <= 102) {
                nibble = c - 87;
            } else {
                revert InvalidAddress();
            }
            result = (result << 4) | uint160(nibble);
        }
        out = address(result);
    }
}

