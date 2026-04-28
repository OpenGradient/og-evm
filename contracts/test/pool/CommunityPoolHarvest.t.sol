// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.20;

// Harvest reward-index behavior; distribution precompile mocked with vm.etch.
// Run from repo:
//
//   cd contracts && npm ci && forge test --match-contract CommunityPoolHarvestTest

import {Test} from "forge-std/Test.sol";
import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

import {CommunityPool} from "../../solidity/pool/CommunityPool.sol";

address constant DISTRIBUTION_PRECOMPILE_HARVEST = address(uint160(0x801));

contract MockBondHarvest is ERC20 {
    constructor() ERC20("Bond", "BOND") {}

    function mint(address to, uint256 value) external {
        _mint(to, value);
    }
}

contract MockDistributionHarvest {
    MockBondHarvest internal immutable bond;
    uint256 internal immutable rewardAmount;

    constructor(MockBondHarvest bond_, uint256 rewardAmount_) {
        bond = bond_;
        rewardAmount = rewardAmount_;
    }

    function claimRewards(address delegatorAddress, uint32) external returns (bool success) {
        if (rewardAmount > 0) {
            bond.mint(delegatorAddress, rewardAmount);
        }
        return true;
    }
}

contract CommunityPoolHarvestTest is Test {
    MockBondHarvest internal bond;
    CommunityPool internal pool;
    address internal alice = address(0xA11CE);
    address internal bob = address(0xB0B);

    function setUp() public {
        bond = new MockBondHarvest();
        pool = new CommunityPool(address(bond), 10, 5, 1 ether, address(this));
    }

    function _mockDistributionReward(uint256 rewardAmount) internal {
        MockDistributionHarvest distribution = new MockDistributionHarvest(bond, rewardAmount);
        vm.etch(DISTRIBUTION_PRECOMPILE_HARVEST, address(distribution).code);
    }

    function test_Harvest_revertsWhenPoolEmpty() public {
        _mockDistributionReward(10 ether);

        vm.expectRevert(CommunityPool.EmptyPool.selector);
        pool.harvest();

        assertEq(bond.balanceOf(address(pool)), 0);
        assertEq(pool.rewardReserve(), 0);
        assertEq(pool.accRewardPerUnit(), 0);
    }

    function test_Harvest_updatesReserveAndRewardIndexWhenPoolHasUnits() public {
        bond.mint(address(this), 100 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(100 ether);

        _mockDistributionReward(7 ether);

        uint256 harvestedAmount = pool.harvest();

        assertEq(harvestedAmount, 7 ether);
        assertEq(pool.rewardReserve(), 7 ether);
        assertEq(pool.accRewardPerUnit(), 0.07 ether);

        uint256 claimedAmount = pool.claimRewards();
        assertEq(claimedAmount, 7 ether);
        assertEq(pool.rewardReserve(), 0);
        assertEq(bond.balanceOf(address(this)), 7 ether);
    }

    function test_Harvest_zeroRewardLeavesReserveAndIndexUnchanged() public {
        bond.mint(address(this), 100 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(100 ether);

        _mockDistributionReward(0);

        uint256 indexBefore = pool.accRewardPerUnit();
        uint256 harvestedAmount = pool.harvest();

        assertEq(harvestedAmount, 0);
        assertEq(pool.rewardReserve(), 0);
        assertEq(pool.accRewardPerUnit(), indexBefore);
    }

}
