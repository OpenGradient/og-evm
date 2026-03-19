package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/testutil/integration/evm/utils"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestUndelegateFallback_FillsPendingUndelegationsWhenRedelegationBlocked verifies
// fallback behavior: undelegations are scheduled when eligible redelegation is blocked.
func (s *KeeperIntegrationTestSuite) TestUndelegateFallback_FillsPendingUndelegationsWhenRedelegationBlocked() {
	// Turn fallback on; this test checks the undelegate path when redelegation is blocked.
	params := s.DefaultEnabledParams(
		0,               // threshold
		1,               // max ops
		sdkmath.ZeroInt(), // max move per op = 0 => no cap
		true,            // use undelegate fallback
	)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	yVal := s.validators[1]

	// Seed immature dst=xVal so src=xVal redelegations are blocked.
	immatureCompletion := s.ctx.BlockTime().Add(s.unbondingSec).UTC()
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.OneInt()),
		CompletionTime:      immatureCompletion,
	})

	// Push extra stake to xVal so it is the natural source candidate.
	s.DelegateExtraToValidator(xVal)

	// Guard rails: xVal must be overweight and there must be at least one deficit destination.
	deltasBefore := s.ComputeCurrentDeltas()
	xDelta, ok := deltasBefore[xVal.OperatorAddress]
	s.Require().True(ok, "expected xVal delta to exist")
	s.Require().True(xDelta.IsNegative(), "expected xVal to be overweight/source candidate")
	s.Require().True(s.HasPositiveDelta(deltasBefore), "expected at least one underweight destination")
	overweightBefore := s.OverweightValidatorSet(deltasBefore)
	s.T().Logf(
		"fallback setup: x=%s y=%s xDelta=%s overweightSet=%d pendingBefore(red=%d,und=%d)",
		xVal.OperatorAddress, yVal.OperatorAddress, xDelta.String(), len(overweightBefore), len(s.PendingRedelegations()), len(s.PendingUndelegations()),
	)

	s.Require().NoError(s.RunEndBlock())

	undelegations := s.PendingUndelegations()
	s.Require().NotEmpty(undelegations, "expected pending undelegations to be scheduled by fallback")
	events := s.ctx.EventManager().Events().ToABCIEvents()
	s.Require().True(utils.ContainsEventType(events, poolrebalancertypes.EventTypeUndelegationStarted))
	s.Require().True(utils.ContainsEventType(events, poolrebalancertypes.EventTypeRebalanceSummary))

	// Fallback undelegations should come from currently overweight validators.
	for _, u := range undelegations {
		_, exists := overweightBefore[u.ValidatorAddress]
		s.Require().True(
			exists,
			"fallback undelegation validator %s was not overweight before EndBlock",
			u.ValidatorAddress,
		)
	}

	// Even with fallback, immature dst=xVal must still block src=xVal redelegations.
	for _, r := range s.PendingRedelegations() {
		s.Require().NotEqual(xVal.OperatorAddress, r.SrcValidatorAddress)
	}
	s.T().Logf(
		"fallback result: pendingAfter(red=%d,und=%d)",
		len(s.PendingRedelegations()), len(undelegations),
	)
}

