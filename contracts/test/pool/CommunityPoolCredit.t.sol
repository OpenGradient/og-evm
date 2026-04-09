// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.20;

// Unit tests for CommunityPool.creditStakeableFromRebalance (MockBond + real contract).
// CI may not invoke Foundry; run locally after installing contracts/ deps. Map @openzeppelin for solc:
//
//   cd contracts && npm ci && forge test --root . --contracts solidity/pool \
//     -R '@openzeppelin/contracts/=node_modules/@openzeppelin/contracts/' \
//     --use 0.8.20 --evm-version paris --match-contract CommunityPoolCreditTest

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

import {CommunityPool} from "../../solidity/pool/CommunityPool.sol";

contract MockBond is ERC20 {
    constructor() ERC20("Bond", "BOND") {}

    function mint(address to, uint256 value) external {
        _mint(to, value);
    }
}

/// @dev Invoked by tests so `msg.sender` to `CommunityPool` is the configured `automationCaller`.
contract AutomationProxy {
    function credit(CommunityPool pool, uint256 amount) external {
        pool.creditStakeableFromRebalance(amount);
    }

    function reconcile(CommunityPool pool, uint256 bonded, uint256 pending) external {
        pool.reconcileStakedBuckets(bonded, pending);
    }
}

contract Stranger {
    function touch(CommunityPool pool, uint256 amount) external {
        pool.creditStakeableFromRebalance(amount);
    }

    function reconcile(CommunityPool pool, uint256 bonded, uint256 pending) external {
        pool.reconcileStakedBuckets(bonded, pending);
    }
}

/// @dev creditStakeableFromRebalance draws from pendingRebalanceUnbondReserve; totalStaked unchanged.
contract CommunityPoolCreditTest {
    MockBond internal bond;
    CommunityPool internal pool;
    AutomationProxy internal automation;

    function setUp() public {
        bond = new MockBond();
        pool = new CommunityPool(address(bond), 10, 5, 1 ether, address(this));
        automation = new AutomationProxy();
        pool.setAutomationCaller(address(automation));
    }

    function test_CreditStakeableFromRebalance_increasesLedgerWhenLiquidCovers() public {
        bond.mint(address(pool), 100 ether);
        automation.reconcile(pool, 0, 100 ether);
        require(pool.stakeablePrincipalLedger() == 0, "ledger0");
        pool.creditStakeableFromRebalance(60 ether);
        require(pool.stakeablePrincipalLedger() == 60 ether, "ledger60");
        require(pool.totalStaked() == 0, "staked0");
        require(pool.pendingRebalanceUnbondReserve() == 40 ether, "pending40");
    }

    function test_CreditStakeableFromRebalance_ownerCanCredit() public {
        bond.mint(address(pool), 50 ether);
        automation.reconcile(pool, 0, 50 ether);
        pool.creditStakeableFromRebalance(50 ether);
        require(pool.stakeablePrincipalLedger() == 50 ether, "ledger50");
        require(pool.pendingRebalanceUnbondReserve() == 0, "pending0");
    }

    function test_CreditStakeableFromRebalance_automationCallerCanCredit() public {
        bond.mint(address(pool), 40 ether);
        automation.reconcile(pool, 0, 40 ether);
        automation.credit(pool, 40 ether);
        require(pool.stakeablePrincipalLedger() == 40 ether, "ledger40");
    }

    function test_CreditStakeableFromRebalance_zeroAmount_noop() public {
        bond.mint(address(pool), 10 ether);
        pool.creditStakeableFromRebalance(0);
        require(pool.stakeablePrincipalLedger() == 0, "noop");
    }

    function test_CreditStakeableFromRebalance_decreasesPending_preservesPrincipalAssets() public {
        bond.mint(address(pool), 100 ether);
        automation.reconcile(pool, 65 ether, 35 ether);
        uint256 beforeAssets = pool.principalAssets();
        pool.creditStakeableFromRebalance(35 ether);
        require(pool.totalStaked() == 65 ether, "staked65");
        require(pool.stakeablePrincipalLedger() == 35 ether, "ledger35");
        require(pool.pendingRebalanceUnbondReserve() == 0, "pending0");
        require(pool.principalAssets() == beforeAssets, "assets");
    }

    function test_CreditStakeableFromRebalance_revertsIfAmountExceedsPendingReserve() public {
        bond.mint(address(pool), 50 ether);
        automation.reconcile(pool, 40 ether, 40 ether);
        uint256 stakedBefore = pool.totalStaked();
        uint256 pendingBefore = pool.pendingRebalanceUnbondReserve();
        uint256 ledgerBefore = pool.stakeablePrincipalLedger();
        uint256 assetsBefore = pool.principalAssets();
        try pool.creditStakeableFromRebalance(41 ether) {
            revert("expected revert pending");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.InvalidAmount.selector, "wrong err");
        }
        require(pool.totalStaked() == stakedBefore, "staked unchanged on revert");
        require(pool.pendingRebalanceUnbondReserve() == pendingBefore, "pending unchanged on revert");
        require(pool.stakeablePrincipalLedger() == ledgerBefore, "ledger unchanged on revert");
        require(pool.principalAssets() == assetsBefore, "principalAssets unchanged on revert");
    }

    function test_CreditStakeableFromRebalance_revertsIfExceedsLiquidInvariant() public {
        bond.mint(address(pool), 10 ether);
        automation.reconcile(pool, 0, 20 ether);
        uint256 stakedBefore = pool.totalStaked();
        uint256 pendingBefore = pool.pendingRebalanceUnbondReserve();
        uint256 ledgerBefore = pool.stakeablePrincipalLedger();
        uint256 assetsBefore = pool.principalAssets();
        try pool.creditStakeableFromRebalance(11 ether) {
            revert("expected revert invariant");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(
                sel == CommunityPool.StakeablePrincipalInvariantViolation.selector,
                "wrong err"
            );
        }
        require(pool.totalStaked() == stakedBefore, "staked unchanged on revert");
        require(pool.pendingRebalanceUnbondReserve() == pendingBefore, "pending unchanged on revert");
        require(pool.stakeablePrincipalLedger() == ledgerBefore, "ledger unchanged on revert");
        require(pool.principalAssets() == assetsBefore, "principalAssets unchanged on revert");
    }

    function test_CreditStakeableFromRebalance_revertsUnauthorized() public {
        bond.mint(address(pool), 10 ether);
        automation.reconcile(pool, 0, 5 ether);
        uint256 stakedBefore = pool.totalStaked();
        uint256 pendingBefore = pool.pendingRebalanceUnbondReserve();
        uint256 ledgerBefore = pool.stakeablePrincipalLedger();
        uint256 assetsBefore = pool.principalAssets();
        Stranger s = new Stranger();
        try s.touch(pool, 1 ether) {
            revert("expected revert auth");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.Unauthorized.selector, "wrong err");
        }
        require(pool.totalStaked() == stakedBefore, "staked unchanged on revert");
        require(pool.pendingRebalanceUnbondReserve() == pendingBefore, "pending unchanged on revert");
        require(pool.stakeablePrincipalLedger() == ledgerBefore, "ledger unchanged on revert");
        require(pool.principalAssets() == assetsBefore, "principalAssets unchanged on revert");
    }

    /// @dev Explicit boundary: `amount == pendingRebalanceUnbondReserve` must succeed (strict `>` check in contract).
    function test_CreditStakeableFromRebalance_amountEqualsPending_drainsPending() public {
        bond.mint(address(pool), 100 ether);
        automation.reconcile(pool, 0, 1);
        uint256 assetsBefore = pool.principalAssets();
        pool.creditStakeableFromRebalance(1);
        require(pool.stakeablePrincipalLedger() == 1, "ledger1");
        require(pool.pendingRebalanceUnbondReserve() == 0, "pending0");
        require(pool.principalAssets() == assetsBefore, "assets");
    }

    function test_CreditStakeableFromRebalance_sequentialCredits_accumulateLedger() public {
        bond.mint(address(pool), 100 ether);
        automation.reconcile(pool, 25 ether, 75 ether);
        uint256 assets0 = pool.principalAssets();
        pool.creditStakeableFromRebalance(30 ether);
        pool.creditStakeableFromRebalance(25 ether);
        pool.creditStakeableFromRebalance(20 ether);
        require(pool.stakeablePrincipalLedger() == 75 ether, "ledger75");
        require(pool.totalStaked() == 25 ether, "staked25");
        require(pool.pendingRebalanceUnbondReserve() == 0, "pending0");
        require(pool.principalAssets() == assets0, "assets");
    }

    /// @dev After `reconcileStakedBuckets` bumps pending reserve, further credits use the new cap; `principalAssets` tracks the reconcile.
    function test_CreditStakeableFromRebalance_afterReconcileBuckets_preservesPrincipalAssets() public {
        bond.mint(address(pool), 200 ether);
        automation.reconcile(pool, 0, 100 ether);
        uint256 assets0 = pool.principalAssets();
        pool.creditStakeableFromRebalance(20 ether);
        require(pool.pendingRebalanceUnbondReserve() == 80 ether, "pending80");
        require(pool.stakeablePrincipalLedger() == 20 ether, "ledger20");
        // Operator raises pending-only reserve by 70 (e.g. staking truth catch-up); bonded stays 0.
        automation.reconcile(pool, 0, 150 ether);
        uint256 assetsAfterBump = pool.principalAssets();
        require(assetsAfterBump == assets0 + 70 ether, "assetsAfterReconcile");
        pool.creditStakeableFromRebalance(40 ether);
        require(pool.stakeablePrincipalLedger() == 60 ether, "ledger60");
        require(pool.pendingRebalanceUnbondReserve() == 110 ether, "pending110");
        require(pool.principalAssets() == assetsAfterBump, "assetsStable");
    }

    function test_CreditStakeableFromRebalance_secondCreditExceedingRemainingPending_reverts() public {
        bond.mint(address(pool), 100 ether);
        automation.reconcile(pool, 30 ether, 30 ether);
        pool.creditStakeableFromRebalance(25 ether);
        require(pool.pendingRebalanceUnbondReserve() == 5 ether, "pending5");
        uint256 stakedBefore = pool.totalStaked();
        uint256 assetsBefore = pool.principalAssets();
        try pool.creditStakeableFromRebalance(6 ether) {
            revert("expected revert second credit");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.InvalidAmount.selector, "wrong err");
        }
        require(pool.totalStaked() == stakedBefore, "staked unchanged on revert");
        require(pool.principalAssets() == assetsBefore, "principalAssets unchanged on revert");
        require(pool.stakeablePrincipalLedger() == 25 ether, "ledger unchanged");
        require(pool.pendingRebalanceUnbondReserve() == 5 ether, "pending unchanged");
    }

    function test_ReconcileStakedBuckets_onlyAutomationCaller() public {
        try pool.reconcileStakedBuckets(1, 2) {
            revert("expected owner reconcile revert");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.Unauthorized.selector, "wrong err");
        }
        automation.reconcile(pool, 9 ether, 11 ether);
        require(pool.totalStaked() == 9 ether && pool.pendingRebalanceUnbondReserve() == 11 ether, "automation ok");
        Stranger s = new Stranger();
        try s.reconcile(pool, 3, 4) {
            revert("expected revert auth reconcile");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.Unauthorized.selector, "wrong err");
        }
    }

    function test_ReconcileStakedBuckets_automationCallerCanReconcile() public {
        automation.reconcile(pool, 10 ether, 20 ether);
        require(pool.totalStaked() == 10 ether, "staked");
        require(pool.pendingRebalanceUnbondReserve() == 20 ether, "pending");
    }

    /// @dev Large pendingRebalanceUnbondReserve does not break liquid invariant on deposit.
    function test_LargePendingRebalanceReserve_depositStillPassesLiquidInvariant() public {
        bond.mint(address(this), 50 ether);
        bond.approve(address(pool), type(uint256).max);
        pool.deposit(10 ether);
        automation.reconcile(pool, 0, 1_000_000 ether);
        pool.deposit(5 ether);
        require(pool.pendingRebalanceUnbondReserve() == 1_000_000 ether, "pending");
        require(pool.stakeablePrincipalLedger() == 15 ether, "ledger");
    }

    /// @dev principalAssets includes pending reserve; second deposit mints fewer units (staking not mocked).
    function test_PrincipalAssets_secondDeposit_mintingUsesPendingInDenominator() public {
        bond.mint(address(this), 300 ether);
        bond.approve(address(pool), type(uint256).max);
        uint256 u1 = pool.deposit(100 ether);
        require(u1 == 100 ether, "first units");
        automation.reconcile(pool, 50 ether, 50 ether);
        require(pool.principalAssets() == 200 ether, "principal200");
        uint256 u2 = pool.deposit(100 ether);
        require(u2 == 50 ether, "second units");
        require(pool.totalUnits() == 150 ether, "totalUnits");
        require(pool.pendingRebalanceUnbondReserve() == 50 ether, "pendingUnchanged");
        require(pool.totalStaked() == 50 ether, "staked");
    }
}
