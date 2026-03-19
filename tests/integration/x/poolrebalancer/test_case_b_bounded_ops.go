package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
)

// TestBoundedOpsPerBlock_MaxOpsIsRespected verifies that max_ops_per_block limits
// scheduling to a single operation in one EndBlock pass.
func (s *KeeperIntegrationTestSuite) TestBoundedOpsPerBlock_MaxOpsIsRespected() {
	// Keep scheduler aggressive; cap block work at one op.
	params := s.DefaultEnabledParams(
		0,   // rebalance_threshold_bp
		1,   // max_ops_per_block
		sdkmath.ZeroInt(), // max_move_per_op = 0 => no cap
		false,
	)

	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.T().Logf("bounded-ops: drift pushed to %s with maxOps=%d", src.OperatorAddress, params.MaxOpsPerBlock)

	s.Require().NoError(s.RunEndBlock())

	pending := s.PendingRedelegations()
	s.T().Logf("bounded-ops: pending redelegations=%d", len(pending))
	s.Require().NotEmpty(pending, "expected at least one pending redelegation")

	// With max_ops_per_block=1 and no pre-existing entries, we should queue exactly one op.
	// (addPendingRedelegation merges only when dst + completion match; with max_ops=1 it's a single move.)
	s.Require().Len(pending, 1, "expected exactly one queued pending redelegation")

	// Quick shape check.
	e := pending[0]
	s.Require().Equal(s.poolDel.String(), e.DelegatorAddress)
	s.Require().Equal(s.bondDenom, e.Amount.Denom)
}

