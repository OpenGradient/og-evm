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
}

contract Stranger {
    function touch(CommunityPool pool, uint256 amount) external {
        pool.creditStakeableFromRebalance(amount);
    }
}

/// @dev Step 1 (plan): unit tests for `creditStakeableFromRebalance` — happy path, no-op, invariant, ACL.
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
        pool.syncTotalStaked(100 ether);
        require(pool.stakeablePrincipalLedger() == 0, "ledger0");
        pool.creditStakeableFromRebalance(60 ether);
        require(pool.stakeablePrincipalLedger() == 60 ether, "ledger60");
        require(pool.totalStaked() == 40 ether, "staked40");
    }

    function test_CreditStakeableFromRebalance_ownerCanCredit() public {
        bond.mint(address(pool), 50 ether);
        pool.syncTotalStaked(50 ether);
        pool.creditStakeableFromRebalance(50 ether);
        require(pool.stakeablePrincipalLedger() == 50 ether, "ledger50");
        require(pool.totalStaked() == 0, "staked0");
    }

    function test_CreditStakeableFromRebalance_automationCallerCanCredit() public {
        bond.mint(address(pool), 40 ether);
        pool.syncTotalStaked(40 ether);
        automation.credit(pool, 40 ether);
        require(pool.stakeablePrincipalLedger() == 40 ether, "ledger40");
    }

    function test_CreditStakeableFromRebalance_zeroAmount_noop() public {
        bond.mint(address(pool), 10 ether);
        pool.creditStakeableFromRebalance(0);
        require(pool.stakeablePrincipalLedger() == 0, "noop");
    }

    function test_CreditStakeableFromRebalance_decreasesTotalStaked_preservesPrincipalAssets() public {
        bond.mint(address(pool), 100 ether);
        pool.syncTotalStaked(100 ether);
        uint256 beforeAssets = pool.principalAssets();
        pool.creditStakeableFromRebalance(35 ether);
        require(pool.totalStaked() == 65 ether, "staked65");
        require(pool.stakeablePrincipalLedger() == 35 ether, "ledger35");
        require(pool.principalAssets() == beforeAssets, "assets");
    }

    function test_CreditStakeableFromRebalance_revertsIfAmountExceedsTotalStaked() public {
        bond.mint(address(pool), 50 ether);
        pool.syncTotalStaked(40 ether);
        try pool.creditStakeableFromRebalance(41 ether) {
            revert("expected revert totalStaked");
        } catch (bytes memory err) {
            require(err.length >= 4, "short err");
            bytes4 sel;
            assembly {
                sel := mload(add(err, 0x20))
            }
            require(sel == CommunityPool.InvalidAmount.selector, "wrong err");
        }
    }

    function test_CreditStakeableFromRebalance_revertsIfExceedsLiquidInvariant() public {
        bond.mint(address(pool), 10 ether);
        pool.syncTotalStaked(20 ether);
        // Must be called as owner (this test contract); a Stranger would hit `Unauthorized` first.
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
    }

    function test_CreditStakeableFromRebalance_revertsUnauthorized() public {
        bond.mint(address(pool), 10 ether);
        pool.syncTotalStaked(5 ether);
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
    }

    /// @dev Explicit boundary: `amount == totalStaked` must succeed (strict `>` check in contract).
    function test_CreditStakeableFromRebalance_amountEqualsTotalStaked_drainsStaked() public {
        bond.mint(address(pool), 100 ether);
        pool.syncTotalStaked(1);
        uint256 assetsBefore = pool.principalAssets();
        pool.creditStakeableFromRebalance(1);
        require(pool.stakeablePrincipalLedger() == 1, "ledger1");
        require(pool.totalStaked() == 0, "staked0");
        require(pool.principalAssets() == assetsBefore, "assets");
    }

    /// @dev Multiple credits in one test document cumulative ledger; Go integration focuses on E2E module path.
    function test_CreditStakeableFromRebalance_sequentialCredits_accumulateLedger() public {
        bond.mint(address(pool), 100 ether);
        pool.syncTotalStaked(100 ether);
        uint256 assets0 = pool.principalAssets();
        pool.creditStakeableFromRebalance(30 ether);
        pool.creditStakeableFromRebalance(25 ether);
        pool.creditStakeableFromRebalance(20 ether);
        require(pool.stakeablePrincipalLedger() == 75 ether, "ledger75");
        require(pool.totalStaked() == 25 ether, "staked25");
        require(pool.principalAssets() == assets0, "assets");
    }

    /// @dev Owner-only `syncTotalStaked` can bump accounting staked before another credit (slash / reconcile).
    function test_CreditStakeableFromRebalance_afterSyncTotalStaked_preservesPrincipalAssets() public {
        bond.mint(address(pool), 200 ether);
        pool.syncTotalStaked(50 ether);
        uint256 assets0 = pool.principalAssets();
        pool.creditStakeableFromRebalance(20 ether);
        require(pool.totalStaked() == 30 ether, "staked30");
        // Owner bumps on-chain reconciled stake upward; credit again uses new `totalStaked` cap.
        pool.syncTotalStaked(100 ether);
        require(pool.principalAssets() == assets0 + 70 ether, "assetsAfterSync");
        pool.creditStakeableFromRebalance(40 ether);
        require(pool.stakeablePrincipalLedger() == 60 ether, "ledger60");
        require(pool.totalStaked() == 60 ether, "staked60");
        require(pool.principalAssets() == assets0 + 70 ether, "assetsStable");
    }

    /// @dev Second call must respect *remaining* `totalStaked`, not the original headline amount.
    function test_CreditStakeableFromRebalance_secondCreditExceedingRemainingTotalStaked_reverts() public {
        bond.mint(address(pool), 100 ether);
        pool.syncTotalStaked(30 ether);
        pool.creditStakeableFromRebalance(25 ether);
        require(pool.totalStaked() == 5 ether, "staked5");
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
        require(pool.stakeablePrincipalLedger() == 25 ether, "ledger unchanged");
        require(pool.totalStaked() == 5 ether, "staked unchanged");
    }
}
