// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.20;

// Withdraw/stake interaction with pendingRebalanceUnbondReserve; staking precompile mocked (vm.mockCall).
// Run from repo:
//
//   cd contracts && npm ci && forge test --match-contract CommunityPoolWithdrawStakeTest

import {Test} from "forge-std/Test.sol";
import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

import {CommunityPool} from "../../solidity/pool/CommunityPool.sol";
import {StakingI} from "../../solidity/precompiles/staking/StakingI.sol";

address constant STAKING_PRECOMPILE = address(uint160(0x800));

contract AutomationProxyWS {
    function reconcile(CommunityPool pool, uint256 bonded, uint256 pending) external {
        pool.reconcileStakedBuckets(bonded, pending);
    }
}

contract MockBondWS is ERC20 {
    constructor() ERC20("Bond", "BOND") {}

    function mint(address to, uint256 value) external {
        _mint(to, value);
    }
}

contract CommunityPoolWithdrawStakeTest is Test {
    MockBondWS internal bond;
    CommunityPool internal pool;
    AutomationProxyWS internal automation;

    function setUp() public {
        bond = new MockBondWS();
        pool = new CommunityPool(address(bond), 10, 5, 1 ether, address(this));
        automation = new AutomationProxyWS();
        pool.setAutomationCaller(address(automation));
    }

    function _mockUndelegate(address poolAddr, uint256 amountOut, uint32 maxValidators, int64 maturityTime) internal {
        bytes memory callData = abi.encodeCall(
            StakingI.undelegateFromBondedValidators,
            (poolAddr, amountOut, maxValidators)
        );
        bytes memory ret = abi.encode(amountOut, uint32(1), maturityTime);
        vm.mockCall(STAKING_PRECOMPILE, callData, ret);
    }

    function _mockDelegate(
        address poolAddr,
        uint256 liquidAmount,
        uint32 maxValidators,
        uint256 delegatedAmount,
        uint32 validatorsUsed
    ) internal {
        bytes memory callData = abi.encodeCall(
            StakingI.delegateToBondedValidators,
            (poolAddr, liquidAmount, maxValidators)
        );
        bytes memory ret = abi.encode(delegatedAmount, validatorsUsed);
        vm.mockCall(STAKING_PRECOMPILE, callData, ret);
    }

    function test_Withdraw_doesNotChangePendingRebalanceUnbondReserve() public {
        bond.mint(address(this), 300 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(200 ether);
        bond.mint(address(pool), 500 ether);
        automation.reconcile(pool, 100 ether, 88 ether);
        uint256 pendingBefore = pool.pendingRebalanceUnbondReserve();

        uint256 withdrawUnits = 100 ether;
        uint256 amountOut = (withdrawUnits * pool.totalStaked()) / pool.totalUnits();
        assertEq(amountOut, 50 ether);

        _mockUndelegate(address(pool), amountOut, pool.maxValidators(), int64(uint64(block.timestamp + 86_400)));

        pool.withdraw(withdrawUnits);

        assertEq(pool.pendingRebalanceUnbondReserve(), pendingBefore);
        assertEq(pool.pendingRebalanceUnbondReserve(), 88 ether);
        assertEq(pool.totalStaked(), 50 ether);
    }

    function test_Withdraw_revertsOnFullExitWhenPendingRebalanceExists() public {
        bond.mint(address(this), 200 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(200 ether);
        bond.mint(address(pool), 500 ether);
        automation.reconcile(pool, 100 ether, 88 ether);

        uint256 fullUnits = pool.totalUnits();
        vm.expectRevert(
            abi.encodeWithSelector(
                CommunityPool.FullExitLeavesNonStakedPrincipal.selector,
                200 ether,
                88 ether
            )
        );
        pool.withdraw(fullUnits);
    }

    function test_Withdraw_allowsFullExitWhenNoNonStakedPrincipalRemains() public {
        bond.mint(address(this), 200 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(200 ether);
        bond.mint(address(pool), 500 ether);
        automation.reconcile(pool, 100 ether, 0);

        _mockDelegate(address(pool), 200 ether, pool.maxValidators(), 200 ether, 2);
        pool.stake();

        uint256 fullUnits = pool.totalUnits();
        uint256 amountOut = (fullUnits * pool.totalStaked()) / pool.totalUnits();
        _mockUndelegate(
            address(pool),
            amountOut,
            pool.maxValidators(),
            int64(uint64(block.timestamp + 86_400))
        );
        pool.withdraw(fullUnits);

        assertEq(pool.totalUnits(), 0);
        assertEq(pool.stakeablePrincipalLedger(), 0);
        assertEq(pool.pendingRebalanceUnbondReserve(), 0);
    }

    function test_Stake_movesStakeableToBonded_only_notPendingReserve() public {
        bond.mint(address(this), 300 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(100 ether);
        assertEq(pool.stakeablePrincipalLedger(), 100 ether);

        _mockDelegate(address(pool), 100 ether, pool.maxValidators(), 100 ether, 3);
        pool.stake();

        assertEq(pool.totalStaked(), 100 ether);
        assertEq(pool.stakeablePrincipalLedger(), 0);
        assertEq(pool.pendingRebalanceUnbondReserve(), 0);

        automation.reconcile(pool, 100 ether, 70 ether);
        assertEq(pool.pendingRebalanceUnbondReserve(), 70 ether);

        bond.mint(address(this), 20 ether);
        pool.deposit(20 ether);

        _mockDelegate(address(pool), 20 ether, pool.maxValidators(), 20 ether, 1);
        pool.stake();

        assertEq(pool.pendingRebalanceUnbondReserve(), 70 ether);
        assertEq(pool.totalStaked(), 120 ether);
        assertEq(pool.stakeablePrincipalLedger(), 0);
    }

    function test_Stake_whenTotalUnitsZero_noopsWithEmptyLedger() public {
        assertEq(pool.totalUnits(), 0);
        assertEq(pool.stakeablePrincipalLedger(), 0);
        assertEq(pool.totalStaked(), 0);

        uint256 delegatedAmount = pool.stake();

        assertEq(delegatedAmount, 0);
        assertEq(pool.totalUnits(), 0);
        assertEq(pool.stakeablePrincipalLedger(), 0);
        assertEq(pool.totalStaked(), 0);
    }
}
