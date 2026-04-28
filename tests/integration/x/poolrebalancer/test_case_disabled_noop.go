package poolrebalancer

import (
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestDisabledNoOp_NoPendingQueues verifies that an empty pool delegator address
// disables rebalancing and leaves pending queues untouched.
func (s *KeeperIntegrationTestSuite) TestDisabledNoOp_NoPendingQueues() {
	ctx := s.network.GetContext()

	// Explicitly disable by clearing pool delegator address.
	s.EnableRebalancer(poolrebalancertypes.DefaultParams()) // baseline
	p := s.DisabledParams()
	s.EnableRebalancer(p)
	s.T().Logf("disabled-case: pool delegator=%q", p.PoolDelegatorAddress)

	s.Require().NoError(s.RunBeginThenEndBlock())

	red := s.PendingRedelegations()
	s.T().Logf("disabled-case: pending after EndBlock redelegations=%d", len(red))
	s.Require().Len(red, 0)

	// Sanity: ensure we did not accidentally enable it.
	params, err := s.poolKeeper.GetParams(ctx)
	s.Require().NoError(err)
	s.Require().Empty(params.PoolDelegatorAddress)
}
