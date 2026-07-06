package keeper

import (
	sdkmath "cosmossdk.io/math"
)

// CalculateBlockReward computes this block's reward with discrete geometric decay in
// fixed-point arithmetic, so the result is identical on every node.
//
// The curve is tracked as a scheduled remaining pool S. Given the per-second decay factor
// d (precomputed as 0.5^(1/halfLife)) and a delta of dt seconds:
//
//	S_new  = S * d^dt
//	reward = S - S_new = S * (1 - d^dt)
//
// Because the steps multiply, N blocks decay S by d^(sum of deltas) exactly the same as one
// block of the summed delta would, so there is no dependence on absolute time and no
// per-block requantization. The reward truncates to integer base units so we never
// over-distribute. Returns the reward and the new scheduled remaining.
func CalculateBlockReward(
	scheduledRemaining sdkmath.LegacyDec,
	decayFactor sdkmath.LegacyDec,
	blockDeltaSec int64,
) (reward sdkmath.Int, newScheduledRemaining sdkmath.LegacyDec) {
	// No decay for a non-positive delta, missing/degenerate state, or a factor >= 1.
	if blockDeltaSec <= 0 ||
		scheduledRemaining.IsNil() || !scheduledRemaining.IsPositive() ||
		decayFactor.IsNil() || !decayFactor.IsPositive() || decayFactor.GTE(sdkmath.LegacyOneDec()) {
		if scheduledRemaining.IsNil() {
			scheduledRemaining = sdkmath.LegacyZeroDec()
		}
		return sdkmath.ZeroInt(), scheduledRemaining
	}

	// d^dt via LegacyDec.Power (exponentiation by squaring on big.Int), identical on every node.
	factor := decayFactor.Power(uint64(blockDeltaSec))
	newScheduledRemaining = scheduledRemaining.Mul(factor)
	reward = scheduledRemaining.Sub(newScheduledRemaining).TruncateInt()
	return reward, newScheduledRemaining
}
