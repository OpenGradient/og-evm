package poolrebalancer

import (
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestTransitiveSafety_BlockedWhileDstImmature verifies that redelegation from a
// source validator is blocked while an immature redelegation already targets it.
func (s *KeeperIntegrationTestSuite) TestTransitiveSafety_BlockedWhileDstImmature() {
	// Keep fallback off so this test only exercises redelegation blocking.
	params := s.DefaultEnabledParams(
		0, // threshold
		1, // max ops
		sdkmath.ZeroInt(),
		false,
	)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	yVal := s.validators[1]
	xSDKValAddr := s.MustValAddr(xVal.OperatorAddress)

	// Seed immature dst=xVal; any new src=xVal redelegation should be blocked.
	immatureCompletion := s.ctx.BlockTime().Add(s.unbondingSec)
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.OneInt()),
		CompletionTime:      immatureCompletion.UTC(),
	})

	// Make xVal overweight so it is a real source candidate.
	s.DelegateExtraToValidator(xVal)

	// Guard against vacuous pass: xVal must be overweight and some dst must need stake.
	deltas := s.ComputeCurrentDeltas()
	xDelta, ok := deltas[xVal.OperatorAddress]
	s.Require().True(ok, "expected xVal delta to exist")
	s.Require().True(xDelta.IsNegative(), "expected xVal to be overweight/source candidate")
	s.Require().True(s.HasPositiveDelta(deltas), "expected at least one underweight destination")
	s.T().Logf(
		"blocked-case setup: x=%s y=%s xDelta=%s hasDstNeedingStake=%t pendingBefore=%d",
		xVal.OperatorAddress, yVal.OperatorAddress, xDelta.String(), s.HasPositiveDelta(deltas), len(s.PendingRedelegations()),
	)

	s.Require().NoError(s.RunEndBlock())

	pending := s.PendingRedelegations()

	// Core invariant: while dst=xVal is immature, no pending move may use src=xVal.
	for _, e := range pending {
		s.Require().NotEqual(xVal.OperatorAddress, e.SrcValidatorAddress, "found pending redelegation with src=xVal while dst=xVal is immature")
	}

	// Seeded immature entry must still exist.
	seedFound := false
	for _, e := range pending {
		if e.SrcValidatorAddress == yVal.OperatorAddress && e.DstValidatorAddress == xVal.OperatorAddress {
			seedFound = true
			break
		}
	}
	s.Require().True(seedFound, "expected seeded immature redelegation into xVal to remain")

	// Immature condition should still hold at this point.
	s.Require().True(s.poolKeeper.HasImmatureRedelegationTo(s.ctx, s.poolDel, xSDKValAddr, s.bondDenom))
	s.T().Logf("blocked-case result: pendingAfter=%d (no src=%s moves)", len(pending), xVal.OperatorAddress)
}

// TestTransitiveSafety_UnblocksAfterDstMaturity verifies that once the immature
// destination entry matures, redelegation from that source can be scheduled again.
func (s *KeeperIntegrationTestSuite) TestTransitiveSafety_UnblocksAfterDstMaturity() {
	// Same starting setup as blocked case.
	params := s.DefaultEnabledParams(0, 1, sdkmath.ZeroInt(), false)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	yVal := s.validators[1]
	xSDKValAddr := s.MustValAddr(xVal.OperatorAddress)

	immatureCompletion := s.ctx.BlockTime().Add(s.unbondingSec)
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.OneInt()),
		CompletionTime:      immatureCompletion.UTC(),
	})
	s.DelegateExtraToValidator(xVal)

	// Guard against vacuous pass before the blocked run.
	deltas := s.ComputeCurrentDeltas()
	xDelta, ok := deltas[xVal.OperatorAddress]
	s.Require().True(ok, "expected xVal delta to exist")
	s.Require().True(xDelta.IsNegative(), "expected xVal to be overweight/source candidate")
	s.Require().True(s.HasPositiveDelta(deltas), "expected at least one underweight destination")
	s.T().Logf(
		"unblock-case setup: x=%s y=%s xDelta=%s hasDstNeedingStake=%t pendingBefore=%d",
		xVal.OperatorAddress, yVal.OperatorAddress, xDelta.String(), s.HasPositiveDelta(deltas), len(s.PendingRedelegations()),
	)

	// First pass: still blocked by immature dst=xVal.
	s.Require().NoError(s.RunEndBlock())
	s.Require().True(s.poolKeeper.HasImmatureRedelegationTo(s.ctx, s.poolDel, xSDKValAddr, s.bondDenom))

	// Move past completion so the seed can mature and get cleaned up.
	s.WithBlockTime(immatureCompletion.Add(1 * time.Second))
	s.Require().NoError(s.RunEndBlock())

	// Immature block should now be gone.
	s.Require().False(s.poolKeeper.HasImmatureRedelegationTo(s.ctx, s.poolDel, xSDKValAddr, s.bondDenom))

	// Once unblocked, scheduler should be free to pick src=xVal.
	pending := s.PendingRedelegations()
	srcFound := false
	for _, e := range pending {
		if e.SrcValidatorAddress == xVal.OperatorAddress {
			srcFound = true
			break
		}
	}
	s.Require().True(srcFound, "expected module to schedule a redelegation from xVal after maturity")
	s.T().Logf("unblock-case result: pendingAfter=%d src=%sScheduled=%t", len(pending), xVal.OperatorAddress, srcFound)
}
