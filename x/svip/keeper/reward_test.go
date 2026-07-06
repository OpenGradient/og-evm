package keeper_test

import (
	"testing"

	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
)

const oneYear = int64(31536000)

// mustDecayFactor is a test helper for the per-second decay factor.
func mustDecayFactor(t *testing.T, halfLife int64) sdkmath.LegacyDec {
	t.Helper()
	d, err := keeper.ComputeDecayFactor(halfLife)
	require.NoError(t, err)
	return d
}

func TestComputeDecayFactor(t *testing.T) {
	// Non-positive half-life is an error, not a panic.
	_, err := keeper.ComputeDecayFactor(0)
	require.Error(t, err)
	_, err = keeper.ComputeDecayFactor(-1)
	require.Error(t, err)

	// d must be in (0,1) and very close to 1 for a 1-year half-life.
	d := mustDecayFactor(t, oneYear)
	require.True(t, d.IsPositive())
	require.True(t, d.LT(sdkmath.LegacyOneDec()))
	require.True(t, d.GT(sdkmath.LegacyMustNewDecFromStr("0.9999")))

	// d^halfLife should equal ~0.5 (the defining property).
	half := d.Power(uint64(oneYear))
	require.InDelta(t, 0.5, half.MustFloat64(), 1e-6)
}

func TestCalculateBlockReward_Guards(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	pool := sdkmath.LegacyNewDec(1_000_000_000_000)

	cases := []struct {
		name  string
		s     sdkmath.LegacyDec
		d     sdkmath.LegacyDec
		delta int64
	}{
		{"zero scheduled remaining", sdkmath.LegacyZeroDec(), d, 5},
		{"zero delta", pool, d, 0},
		{"negative delta", pool, d, -5},
		{"decay factor >= 1", pool, sdkmath.LegacyOneDec(), 5},
		{"zero decay factor", pool, sdkmath.LegacyZeroDec(), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reward, newS := keeper.CalculateBlockReward(tc.s, tc.d, tc.delta)
			require.True(t, reward.IsZero(), "expected zero reward, got %s", reward)
			require.True(t, newS.Equal(tc.s), "scheduled remaining must be unchanged on a guarded call")
		})
	}
}

func TestCalculateBlockReward_Normal(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	s := sdkmath.LegacyNewDec(1_000_000_000_000_000)

	reward, newS := keeper.CalculateBlockReward(s, d, 5)
	require.True(t, reward.IsPositive(), "expected positive reward")
	require.True(t, newS.LT(s), "scheduled remaining must decrease")
	// Reward equals the exact curve decrease (truncated).
	require.Equal(t, s.Sub(newS).TruncateInt().String(), reward.String())
}

// TestCalculateBlockReward_HalfLifeDecay: after one half-life the per-block rate is ~50%.
func TestCalculateBlockReward_HalfLifeDecay(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	s0 := sdkmath.LegacyNewDec(1_000_000_000_000_000)

	r0, _ := keeper.CalculateBlockReward(s0, d, 5)

	// Advance the curve by exactly one half-life, which halves S: S = S * d^halfLife.
	sHalf := s0.Mul(d.Power(uint64(oneYear)))
	rHalf, _ := keeper.CalculateBlockReward(sHalf, d, 5)

	require.True(t, r0.IsPositive() && rHalf.IsPositive())
	ratio := rHalf.ToLegacyDec().Quo(r0.ToLegacyDec()).MustFloat64()
	require.InDelta(t, 0.5, ratio, 0.01, "per-block rate after one half-life should be ~50%%, got %f", ratio)
}

// TestCalculateBlockReward_Telescoping: N one-second steps leave S about where a single
// N-second step would.
func TestCalculateBlockReward_Telescoping(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	start := sdkmath.LegacyNewDec(1_000_000_000_000_000)

	const n = 600
	// Incremental: n blocks of 1 second.
	sInc := start
	for range n {
		_, sInc = keeper.CalculateBlockReward(sInc, d, 1)
	}
	// Single step of n seconds.
	_, sOne := keeper.CalculateBlockReward(start, d, n)

	rel := sInc.Sub(sOne).Abs().Quo(sOne).MustFloat64()
	require.Less(t, rel, 1e-9, "telescoping drift too large: %g", rel)
}

// TestCalculateBlockReward_VeryLargeElapsed: after 100 half-lives, reward truncates to zero.
func TestCalculateBlockReward_VeryLargeElapsed(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	pool := sdkmath.LegacyNewDec(1_000_000_000_000)

	// Decay 100 half-lives, then take a block.
	s := pool.Mul(d.Power(uint64(oneYear * 100)))
	reward, _ := keeper.CalculateBlockReward(s, d, 5)
	require.True(t, reward.IsZero(), "reward after 100 half-lives should be zero, got %s", reward)
}

// TestCalculateBlockReward_Monotonic: rewards decrease as the curve advances.
func TestCalculateBlockReward_Monotonic(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	s := sdkmath.LegacyNewDec(1_000_000_000_000_000)

	var prev sdkmath.Int
	for range 30 {
		var r sdkmath.Int
		r, s = keeper.CalculateBlockReward(s, d, oneYear/10)
		if !prev.IsNil() && prev.IsPositive() && r.IsPositive() {
			require.True(t, r.LTE(prev), "reward should decrease monotonically: got %s > prev %s", r, prev)
		}
		prev = r
	}
}

// TestCalculateBlockReward_HugePool: no panic / overflow for an enormous pool.
func TestCalculateBlockReward_HugePool(t *testing.T) {
	d := mustDecayFactor(t, oneYear)
	// 1e9 tokens * 1e18 = 1e27 base units.
	huge, ok := sdkmath.NewIntFromString("1000000000000000000000000000")
	require.True(t, ok)
	s := sdkmath.LegacyNewDecFromInt(huge)

	require.NotPanics(t, func() {
		reward, newS := keeper.CalculateBlockReward(s, d, 6)
		require.True(t, reward.IsPositive())
		require.True(t, reward.LT(huge))
		require.True(t, newS.LT(s))
	})
}
