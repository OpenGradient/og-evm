package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/evm/testutil/integration/evm/utils"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestSchedulingA_DriftCreatesPendingRedelegations verifies that measurable drift
// produces at least one pending redelegation for the pool delegator.
func (s *KeeperIntegrationTestSuite) TestSchedulingA_DriftCreatesPendingRedelegations() {
	// Any drift should schedule with bp=0.
	params := s.DefaultEnabledParams(
		0,  // rebalance_threshold_bp
		1,  // max_ops_per_block
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.T().Logf("scheduling-case: drift pushed to %s", src.OperatorAddress)

	s.Require().NoError(s.RunEndBlock())
	pending := s.PendingRedelegations()
	s.T().Logf("scheduling-case: pending redelegations=%d", len(pending))

	events := s.ctx.EventManager().Events().ToABCIEvents()
	s.Require().True(utils.ContainsEventType(events, poolrebalancertypes.EventTypeRedelegationStarted))
	s.Require().True(utils.ContainsEventType(events, poolrebalancertypes.EventTypeRebalanceSummary))

	s.Require().NotEmpty(pending, "expected at least one pending redelegation")

	// Spot-check one entry shape.
	ctx := s.network.GetContext()
	found := false
	for _, e := range pending {
		if e.DelegatorAddress == s.poolDel.String() {
			s.Require().Equal(s.bondDenom, e.Amount.Denom)
			s.Require().True(e.CompletionTime.After(ctx.BlockTime()))
			found = true
			break
		}
	}
	s.Require().True(found, "expected pool delegator entries in pending redelegations")

	s.Require().GreaterOrEqual(len(pending), 1)
}

// TestSchedulingA_ReducesSourceOverweightInStakingState verifies a successful scheduling
// pass reduces overweight stake on the drifted source validator in staking state.
func (s *KeeperIntegrationTestSuite) TestSchedulingA_ReducesSourceOverweightInStakingState() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.ZeroInt(), false)
	s.EnableRebalancer(params)

	src := s.validators[0]
	srcAddr := src.OperatorAddress
	s.DelegateExtraToValidator(src)

	before, _, err := s.poolKeeper.GetDelegatorStakeByValidator(s.ctx, s.poolDel)
	s.Require().NoError(err)
	beforeSrc := before[srcAddr]
	s.Require().True(beforeSrc.IsPositive(), "expected positive source stake before scheduling")

	s.Require().NoError(s.RunEndBlock())

	after, _, err := s.poolKeeper.GetDelegatorStakeByValidator(s.ctx, s.poolDel)
	s.Require().NoError(err)
	afterSrc := after[srcAddr]
	s.Require().True(
		afterSrc.LT(beforeSrc),
		"expected source stake to decrease after one rebalance op; before=%s after=%s",
		beforeSrc.String(),
		afterSrc.String(),
	)
}

