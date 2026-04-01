package poolrebalancer

import (
	"time"

	sdkmath "cosmossdk.io/math"
)

// maxAbsDelta returns max(|delta|) across all validators.
func maxAbsDelta(deltas map[string]sdkmath.Int) sdkmath.Int {
	max := sdkmath.ZeroInt()
	for _, d := range deltas {
		abs := d.Abs()
		if abs.GT(max) {
			max = abs
		}
	}
	return max
}

// TestLongHorizonConvergence_RedelegationOnly verifies that repeated EndBlock passes
// with periodic maturity windows reduce drift to a small tolerance using redelegations only.
func (s *KeeperIntegrationTestSuite) TestLongHorizonConvergence_RedelegationOnly() {
	params := s.DefaultEnabledParams(
		0, // threshold: schedule on any drift
		1, // force gradual per-pass progress to exercise long-horizon behavior
		sdkmath.NewInt(100000000000000000), // cap per-op movement to require multiple iterations
		false, // redelegation-only mode
	)
	s.EnableRebalancer(params)

	// Create deterministic overweight drift on one source validator.
	src := s.validators[0]
	s.DelegateExtraToValidator(src)

	const (
		maxIters          = 60
		maturityJumpEvery = 5
		// Keep practical tolerance to absorb truncation/rounding residue.
		convergenceTolerance = int64(10)
		// Guard against vacuous success: start from a non-trivial drift.
		minInitialDrift = int64(1000)
		// Ensure this is a long-horizon behavior test, not a one-iteration pass.
		minItersBeforeConverged = 3
	)
	tol := sdkmath.NewInt(convergenceTolerance)
	minStart := sdkmath.NewInt(minInitialDrift)

	initialDeltas := s.ComputeCurrentDeltas()
	initialMaxAbs := maxAbsDelta(initialDeltas)
	s.Require().True(initialMaxAbs.IsPositive(), "expected initial non-zero drift")
	s.Require().True(
		initialMaxAbs.GTE(minStart),
		"expected non-trivial initial drift; got %s want >= %s",
		initialMaxAbs.String(),
		minStart.String(),
	)

	converged := false
	convergedAt := 0
	sawProgress := false
	for i := 1; i <= maxIters; i++ {
		s.Require().NoError(s.RunEndBlock())

		// Periodically move past unbonding window so queued ops can mature and cleanup can proceed.
		if i%maturityJumpEvery == 0 {
			s.WithBlockTime(s.ctx.BlockTime().Add(s.unbondingSec + time.Second))
		}

		deltas := s.ComputeCurrentDeltas()
		curMaxAbs := maxAbsDelta(deltas)
		pendingRed := len(s.PendingRedelegations())
		pendingUnd := len(s.PendingUndelegations())
		s.T().Logf(
			"convergence iter=%d maxAbsDelta=%s tol=%s pending(red=%d,und=%d)",
			i, curMaxAbs.String(), tol.String(), pendingRed, pendingUnd,
		)

		if curMaxAbs.LT(initialMaxAbs) {
			sawProgress = true
		}

		if curMaxAbs.LTE(tol) {
			converged = true
			convergedAt = i
			break
		}
	}

	s.Require().True(
		converged,
		"expected convergence within %d iterations (initial maxAbs=%s, tolerance=%s)",
		maxIters,
		initialMaxAbs.String(),
		tol.String(),
	)
	s.Require().GreaterOrEqual(
		convergedAt,
		minItersBeforeConverged,
		"converged too quickly at iter=%d; expected long-horizon behavior (>= %d iters)",
		convergedAt,
		minItersBeforeConverged,
	)
	s.Require().True(
		sawProgress,
		"expected at least one measurable improvement from initial maxAbsDelta=%s",
		initialMaxAbs.String(),
	)

	// Final maturity pass to ensure no stale queue buildup remains.
	s.WithBlockTime(s.ctx.BlockTime().Add(s.unbondingSec + time.Second))
	s.Require().NoError(s.RunEndBlock())
	s.Require().Empty(s.PendingUndelegations(), "undelegation queue should remain empty in redelegation-only mode")
}
