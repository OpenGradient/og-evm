package keeper

import (
	"fmt"
	"math"

	sdkmath "cosmossdk.io/math"
)

// CalculateBlockReward returns how many tokens (in base denom units) to
// distribute for this block using exponential decay.
//
// poolAtActivation — token balance when SVIP was activated (base denom)
// totalElapsedSec  — seconds since SVIP activation
// blockDeltaSec    — seconds since the previous block
//
// Exponential decay formula:
//
//	R₀ = (ln2 / half_life) × poolAtActivation
//	rate(t) = R₀ × e^(-λ × t)
//	blockReward = rate(t) × blockDelta
func CalculateBlockReward(
	halfLifeSeconds int64,
	poolAtActivation sdkmath.Int,
	totalElapsedSec float64,
	blockDeltaSec float64,
) sdkmath.Int {
	if poolAtActivation.IsZero() || blockDeltaSec <= 0 || totalElapsedSec < 0 {
		return sdkmath.ZeroInt()
	}

	halfLifeSec := float64(halfLifeSeconds)
	if halfLifeSec <= 0 {
		return sdkmath.ZeroInt()
	}

	lambda := math.Log(2) / halfLifeSec
	poolFloat := poolAtActivation.ToLegacyDec().MustFloat64()
	initialRate := lambda * poolFloat                              // tokens per second at t=0
	currentRate := initialRate * math.Exp(-lambda*totalElapsedSec) // tokens/sec now
	blockReward := currentRate * blockDeltaSec

	if blockReward <= 0 {
		return sdkmath.ZeroInt()
	}

	// Convert to integer (truncate — we never over-distribute)
	rewardStr := fmt.Sprintf("%.0f", blockReward)
	reward, ok := sdkmath.NewIntFromString(rewardStr)
	if !ok {
		return sdkmath.ZeroInt()
	}
	return reward
}
