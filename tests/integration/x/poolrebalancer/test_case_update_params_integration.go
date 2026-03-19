package poolrebalancer

import (
	"bytes"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestUpdateParams_RejectsInvalidAuthority verifies that MsgUpdateParams enforces
// module authority and rejects unauthorized callers.
func (s *KeeperIntegrationTestSuite) TestUpdateParams_RejectsInvalidAuthority() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.ZeroInt(), false)

	msg := &poolrebalancertypes.MsgUpdateParams{
		Authority: sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String(),
		Params:    params,
	}

	_, err := s.poolKeeper.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "invalid authority")
	s.T().Logf("update-params auth-check: invalid authority rejected as expected")
}

// TestUpdateParams_ValidAuthorityChangesSchedulingBehavior verifies that a valid
// params update changes runtime scheduling behavior in the same test setup.
func (s *KeeperIntegrationTestSuite) TestUpdateParams_ValidAuthorityChangesSchedulingBehavior() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// Reuse the same drift across both phases.
	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.T().Logf("update-params flow: drift pushed to %s", src.OperatorAddress)

	// Phase 1: high threshold, expect no scheduling.
	high := s.DefaultEnabledParams(
		10000, // threshold suppresses all scheduling
		1,
		sdkmath.ZeroInt(),
		false,
	)
	_, err := s.poolKeeper.UpdateParams(s.ctx, &poolrebalancertypes.MsgUpdateParams{
		Authority: authority,
		Params:    high,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.RunEndBlock())
	s.Require().Len(s.PendingRedelegations(), 0, "expected no scheduling under high threshold")
	s.Require().Len(s.PendingUndelegations(), 0, "expected no fallback scheduling under high threshold")
	s.T().Logf("update-params flow: high threshold kept queues empty")

	// Phase 2: lower threshold, same drift should now schedule.
	low := high
	low.RebalanceThresholdBp = 0
	_, err = s.poolKeeper.UpdateParams(s.ctx, &poolrebalancertypes.MsgUpdateParams{
		Authority: authority,
		Params:    low,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.RunEndBlock())
	s.Require().NotEmpty(s.PendingRedelegations(), "expected scheduling after lowering threshold")
	s.T().Logf("update-params flow: low threshold scheduled %d redelegations", len(s.PendingRedelegations()))
}

