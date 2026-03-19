package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
)

// TestThresholdBehavior_HighThresholdPreventsScheduling verifies that a very high
// threshold suppresses scheduling even when drift exists.
func (s *KeeperIntegrationTestSuite) TestThresholdBehavior_HighThresholdPreventsScheduling() {
	// Keep threshold effectively at "do nothing" level.
	params := s.DefaultEnabledParams(
		10000, // rebalance_threshold_bp
		1,      // max_ops_per_block
		sdkmath.ZeroInt(),
		false, // disable undelegate fallback in this test
	)
	s.EnableRebalancer(params)

	// Add drift; with this threshold we still expect a no-op.
	src := s.validators[0]
	s.DelegateExtraToValidator(src)

	s.Require().NoError(s.RunEndBlock())

	red := s.PendingRedelegations()
	und := s.PendingUndelegations()

	s.Require().Len(red, 0, "expected no pending redelegations under high threshold")
	s.Require().Len(und, 0, "expected no pending undelegations under high threshold")

}

// TestThresholdBehavior_BoundaryPair_NoOpThenSchedules verifies boundary behavior:
// same drift is ignored at high threshold and scheduled after threshold is lowered.
func (s *KeeperIntegrationTestSuite) TestThresholdBehavior_BoundaryPair_NoOpThenSchedules() {
	// Same drift, two threshold values.
	high := s.DefaultEnabledParams(
		10000, // threshold == total stake (effectively suppresses scheduling)
		1,
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(high)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.T().Logf(
		"drift injected on %s (bp=%d), pending before: redelegations=%d undelegations=%d",
		src.OperatorAddress, high.RebalanceThresholdBp, len(s.PendingRedelegations()), len(s.PendingUndelegations()),
	)

	s.Require().NoError(s.RunEndBlock())
	s.Require().Len(s.PendingRedelegations(), 0, "expected no scheduling under high threshold")
	s.Require().Len(s.PendingUndelegations(), 0, "expected no fallback scheduling under high threshold")
	s.T().Logf(
		"high-threshold pass stayed idle: redelegations=%d undelegations=%d",
		len(s.PendingRedelegations()), len(s.PendingUndelegations()),
	)

	// Lower threshold without changing the drift; scheduler should now engage.
	low := high
	low.RebalanceThresholdBp = 0
	s.EnableRebalancer(low)

	s.Require().NoError(s.RunEndBlock())
	s.Require().NotEmpty(s.PendingRedelegations(), "expected scheduling after lowering threshold")
	s.T().Logf(
		"after lowering to bp=%d: redelegations=%d undelegations=%d",
		low.RebalanceThresholdBp, len(s.PendingRedelegations()), len(s.PendingUndelegations()),
	)
}

