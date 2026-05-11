package keeper_test

import (
	"math"
	"testing"

	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
)

func TestCalculateBlockReward(t *testing.T) {
	halfLife := int64(31536000) // 1 year in seconds
	pool := sdkmath.NewInt(1_000_000_000_000)

	testCases := []struct {
		name            string
		halfLifeSeconds int64
		poolAtActivation sdkmath.Int
		totalElapsedSec float64
		blockDeltaSec   float64
		expectZero      bool
	}{
		{
			name:            "zero pool balance",
			halfLifeSeconds: halfLife,
			poolAtActivation: sdkmath.ZeroInt(),
			totalElapsedSec: 100,
			blockDeltaSec:   5,
			expectZero:      true,
		},
		{
			name:            "zero block delta",
			halfLifeSeconds: halfLife,
			poolAtActivation: pool,
			totalElapsedSec: 100,
			blockDeltaSec:   0,
			expectZero:      true,
		},
		{
			name:            "negative block delta",
			halfLifeSeconds: halfLife,
			poolAtActivation: pool,
			totalElapsedSec: 100,
			blockDeltaSec:   -5,
			expectZero:      true,
		},
		{
			name:            "negative elapsed",
			halfLifeSeconds: halfLife,
			poolAtActivation: pool,
			totalElapsedSec: -100,
			blockDeltaSec:   5,
			expectZero:      true,
		},
		{
			name:            "zero half_life",
			halfLifeSeconds: 0,
			poolAtActivation: pool,
			totalElapsedSec: 100,
			blockDeltaSec:   5,
			expectZero:      true,
		},
		{
			name:            "negative half_life",
			halfLifeSeconds: -1,
			poolAtActivation: pool,
			totalElapsedSec: 100,
			blockDeltaSec:   5,
			expectZero:      true,
		},
		{
			name:            "normal case - first block",
			halfLifeSeconds: halfLife,
			poolAtActivation: pool,
			totalElapsedSec: 5,
			blockDeltaSec:   5,
			expectZero:      false,
		},
		{
			name:            "normal case - mid life",
			halfLifeSeconds: halfLife,
			poolAtActivation: pool,
			totalElapsedSec: 1000,
			blockDeltaSec:   5,
			expectZero:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reward := keeper.CalculateBlockReward(
				tc.halfLifeSeconds,
				tc.poolAtActivation,
				tc.totalElapsedSec,
				tc.blockDeltaSec,
			)
			if tc.expectZero {
				require.True(t, reward.IsZero(), "expected zero reward, got %s", reward)
			} else {
				require.True(t, reward.IsPositive(), "expected positive reward, got %s", reward)
			}
		})
	}
}

func TestCalculateBlockReward_HalfLifeDecay(t *testing.T) {
	halfLife := int64(31536000)
	pool := sdkmath.NewInt(1_000_000_000_000_000) // large pool for precision
	blockDelta := float64(5)

	// Reward at t=0
	r0 := keeper.CalculateBlockReward(halfLife, pool, blockDelta, blockDelta)
	require.True(t, r0.IsPositive())

	// Reward at t=halfLife
	rHalf := keeper.CalculateBlockReward(halfLife, pool, float64(halfLife), blockDelta)
	require.True(t, rHalf.IsPositive())

	// After one half-life, the rate should be ~half of the initial rate.
	// Allow 5% tolerance for truncation.
	r0Float := r0.ToLegacyDec().MustFloat64()
	rHalfFloat := rHalf.ToLegacyDec().MustFloat64()
	ratio := rHalfFloat / r0Float
	require.InDelta(t, 0.5, ratio, 0.05, "reward after one half-life should be ~50%% of initial, got ratio %f", ratio)
}

func TestCalculateBlockReward_VeryLargeElapsed(t *testing.T) {
	halfLife := int64(31536000)
	pool := sdkmath.NewInt(1_000_000_000_000)

	// After 100 half-lives, reward should be effectively zero
	elapsed := float64(halfLife) * 100
	reward := keeper.CalculateBlockReward(halfLife, pool, elapsed, 5)
	require.True(t, reward.IsZero(), "reward after 100 half-lives should be zero, got %s", reward)
}

func TestCalculateBlockReward_Monotonic(t *testing.T) {
	halfLife := int64(31536000)
	pool := sdkmath.NewInt(1_000_000_000_000_000)
	blockDelta := float64(5)

	// Rewards should decrease monotonically over time
	var prev sdkmath.Int
	for elapsed := float64(0); elapsed < float64(halfLife)*3; elapsed += float64(halfLife) / 10 {
		r := keeper.CalculateBlockReward(halfLife, pool, math.Max(elapsed, blockDelta), blockDelta)
		if !prev.IsNil() && prev.IsPositive() && r.IsPositive() {
			require.True(t, r.LTE(prev), "reward should decrease: at elapsed=%f got %s > prev %s", elapsed, r, prev)
		}
		prev = r
	}
}
