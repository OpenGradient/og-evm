package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// PendingRedelegations returns all pending redelegations, failing test on query error.
func (s *KeeperIntegrationTestSuite) PendingRedelegations() []poolrebalancertypes.PendingRedelegation {
	out, err := s.poolKeeper.GetAllPendingRedelegations(s.ctx)
	s.Require().NoError(err)
	return out
}

// PendingUndelegations returns all pending undelegations, failing test on query error.
func (s *KeeperIntegrationTestSuite) PendingUndelegations() []poolrebalancertypes.PendingUndelegation {
	out, err := s.poolKeeper.GetAllPendingUndelegations(s.ctx)
	s.Require().NoError(err)
	return out
}

// DelegateExtraToValidator creates deterministic drift by adding extra stake on one validator.
// Amount selection tries to be large enough to survive truncation in stake math.
func (s *KeeperIntegrationTestSuite) DelegateExtraToValidator(val stakingtypes.Validator) {
	// Rebalancer math uses truncated token amounts; too-small moves can disappear.
	free := s.network.App.GetBankKeeper().GetBalance(s.ctx, s.poolDel, s.bondDenom).Amount
	s.Require().True(free.IsPositive(), "pool delegator free balance must be > 0")

	stakeByVal, _, err := s.poolKeeper.GetDelegatorStakeByValidator(s.ctx, s.poolDel)
	s.Require().NoError(err)
	base := stakeByVal[val.OperatorAddress]
	s.Require().True(base.IsPositive(), "expected base stake for chosen validator")

	// Use roughly existing stake as drift target, bounded by free balance.
	extra := base
	if free.LT(extra) {
		extra = free
	}

	s.Require().True(extra.IsPositive(), "drift delegation amount must be > 0")

	_, err = s.network.App.GetStakingKeeper().Delegate(
		s.ctx,
		s.poolDel,
		extra,
		stakingtypes.Unspecified,
		val,
		true,
	)
	s.Require().NoError(err)
}

// SeedPendingRedelegation inserts a pending redelegation fixture entry.
func (s *KeeperIntegrationTestSuite) SeedPendingRedelegation(entry poolrebalancertypes.PendingRedelegation) {
	s.Require().NoError(s.poolKeeper.SetPendingRedelegation(s.ctx, entry))
}

// SeedPendingUndelegation inserts a pending undelegation fixture entry.
func (s *KeeperIntegrationTestSuite) SeedPendingUndelegation(entry poolrebalancertypes.PendingUndelegation) {
	s.Require().NoError(s.poolKeeper.SetPendingUndelegation(s.ctx, entry))
}

// ComputeCurrentDeltas mirrors ProcessRebalance inputs and returns target-current deltas.
func (s *KeeperIntegrationTestSuite) ComputeCurrentDeltas() map[string]sdkmath.Int {
	targetVals, err := s.poolKeeper.GetTargetBondedValidators(s.ctx)
	s.Require().NoError(err)
	s.Require().NotEmpty(targetVals)

	current, total, err := s.poolKeeper.GetDelegatorStakeByValidator(s.ctx, s.poolDel)
	s.Require().NoError(err)
	s.Require().True(total.IsPositive())

	target, err := s.poolKeeper.EqualWeightTarget(total, targetVals)
	s.Require().NoError(err)

	params, err := s.poolKeeper.GetParams(s.ctx)
	s.Require().NoError(err)

	deltas, err := s.poolKeeper.ComputeDeltas(target, current, total, params.RebalanceThresholdBp)
	s.Require().NoError(err)
	return deltas
}

// HasPositiveDelta reports whether any validator is currently underweight.
func (s *KeeperIntegrationTestSuite) HasPositiveDelta(deltas map[string]sdkmath.Int) bool {
	for _, d := range deltas {
		if d.IsPositive() {
			return true
		}
	}
	return false
}

// OverweightValidatorSet builds a quick lookup of validators with negative deltas.
func (s *KeeperIntegrationTestSuite) OverweightValidatorSet(deltas map[string]sdkmath.Int) map[string]struct{} {
	out := make(map[string]struct{})
	for val, d := range deltas {
		if d.IsNegative() {
			out[val] = struct{}{}
		}
	}
	return out
}

// MustValAddr parses valoper bech32 and fails the test on invalid input.
func (s *KeeperIntegrationTestSuite) MustValAddr(bech32 string) sdk.ValAddress {
	val, err := sdk.ValAddressFromBech32(bech32)
	s.Require().NoError(err)
	return val
}

