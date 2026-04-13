package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestPreviousBlockSlash_PrioritizesRedelegation verifies that a validator slashed in block H
// is chosen as the source for the first rebalance op scheduled in block H+1.
func (s *KeeperIntegrationTestSuite) TestPreviousBlockSlash_PrioritizesRedelegation() {
	params := s.DefaultEnabledParams(
		0,
		1,
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	slashedVal := s.validators[0]
	otherOverweight := s.validators[1]
	s.DelegateExtraToValidator(slashedVal)
	s.DelegateExtraToValidator(otherOverweight)
	s.RecordSlashEvent(slashedVal)

	s.WithBlockHeight(s.ctx.BlockHeight() + 1)
	s.Require().NoError(s.RunBeginThenEndBlock())

	redels := s.PendingRedelegations()
	s.Require().Len(redels, 1, "max_ops_per_block=1 should queue exactly one redelegation")
	s.Require().Equal(slashedVal.OperatorAddress, redels[0].SrcValidatorAddress, "previous-block slashed validator should be the first source")
}

// TestPreviousBlockSlash_ExcludedFromDestinations verifies that the rebalance logic does not
// choose a slashed validator as a same-block destination.
func (s *KeeperIntegrationTestSuite) TestPreviousBlockSlash_ExcludedFromDestinations() {
	params := s.DefaultEnabledParams(
		0,
		2,
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	src := s.validators[0]
	slashedDst := s.validators[1]
	s.DelegateExtraToValidator(src)
	s.RecordSlashEvent(slashedDst)

	s.WithBlockHeight(s.ctx.BlockHeight() + 1)
	s.Require().NoError(s.RunBeginThenEndBlock())

	redels := s.PendingRedelegations()
	s.Require().NotEmpty(redels, "expected at least one pending redelegation")
	for _, r := range redels {
		s.Require().NotEqual(slashedDst.OperatorAddress, r.DstValidatorAddress, "slashed validator must not be used as destination")
	}
}

// TestPreviousBlockSlash_RespectsMaxOps verifies that slash-priority still obeys max_ops_per_block.
func (s *KeeperIntegrationTestSuite) TestPreviousBlockSlash_RespectsMaxOps() {
	params := s.DefaultEnabledParams(
		0,
		1,
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	slashedVal := s.validators[0]
	s.DelegateExtraToValidator(slashedVal)
	s.RecordSlashEvent(slashedVal)

	s.WithBlockHeight(s.ctx.BlockHeight() + 1)
	s.Require().NoError(s.RunBeginThenEndBlock())

	s.Require().Len(s.PendingRedelegations(), 1, "slash-priority must still honor max_ops_per_block")
}

// TestPreviousBlockSlash_PrioritizesUndelegationFallback verifies that when slash-priority
// redelegation is blocked, fallback undelegation prefers the slashed validator first.
func (s *KeeperIntegrationTestSuite) TestPreviousBlockSlash_PrioritizesUndelegationFallback() {
	params := s.DefaultEnabledParams(
		0,
		1,
		sdkmath.ZeroInt(),
		true,
	)
	s.EnableRebalancer(params)

	slashedVal := s.validators[0]
	blockingSrc := s.validators[1]

	// Seed an immature redelegation to slashedVal so redelegations out of slashedVal are blocked.
	immatureCompletion := s.ctx.BlockTime().Add(s.unbondingSec).UTC()
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: blockingSrc.OperatorAddress,
		DstValidatorAddress: slashedVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.OneInt()),
		CompletionTime:      immatureCompletion,
	})

	s.DelegateExtraToValidator(slashedVal)
	s.RecordSlashEvent(slashedVal)

	s.WithBlockHeight(s.ctx.BlockHeight() + 1)
	s.Require().NoError(s.RunBeginThenEndBlock())

	undels := s.PendingUndelegations()
	s.Require().Len(undels, 1, "expected one fallback undelegation")
	s.Require().Equal(slashedVal.OperatorAddress, undels[0].ValidatorAddress, "fallback should target slashed validator first")
}

// TestPreviousBlockSlash_NoSlashRegression verifies that existing scheduling behavior still works
// when no slash event was recorded in the previous block.
func (s *KeeperIntegrationTestSuite) TestPreviousBlockSlash_NoSlashRegression() {
	params := s.DefaultEnabledParams(
		0,
		1,
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)

	s.Require().NoError(s.RunBeginThenEndBlock())

	redels := s.PendingRedelegations()
	s.Require().Len(redels, 1, "expected normal scheduling without prior slash")
	s.Require().Equal(src.OperatorAddress, redels[0].SrcValidatorAddress)
}
