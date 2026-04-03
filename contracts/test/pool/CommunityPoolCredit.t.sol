// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.20;

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
}
