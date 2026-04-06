package keeper

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetTargetBondedValidators returns the top bonded validators by power.
// The result size is capped by the module param MaxTargetValidators and preserves staking's power ordering.
func (k Keeper) GetTargetBondedValidators(ctx context.Context) ([]sdk.ValAddress, error) {
	maxN, err := k.GetMaxTargetValidators(ctx)
	if err != nil {
		return nil, err
	}
	if maxN == 0 {
		return nil, fmt.Errorf("MaxTargetValidators must be > 0")
	}

	vals, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return nil, err
	}

	n := int(maxN)
	if n > len(vals) {
		n = len(vals)
	}

	out := make([]sdk.ValAddress, 0, n)
	for i := 0; i < n; i++ {
		valAddr, err := sdk.ValAddressFromBech32(vals[i].OperatorAddress)
		if err != nil {
			return nil, err
		}
		out = append(out, valAddr)
	}
	return out, nil
}

// GetDelegatorStakeByValidator returns the delegator's bonded stake per validator (in tokens, truncated).
// The returned map is keyed by validator operator address (bech32), plus the total across all validators.
func (k Keeper) GetDelegatorStakeByValidator(ctx context.Context, del sdk.AccAddress) (map[string]math.Int, math.Int, error) {
	delegations, err := k.stakingKeeper.GetDelegatorDelegations(ctx, del, ^uint16(0))
	if err != nil {
		return nil, math.ZeroInt(), err
	}

	stakeByValidator := make(map[string]math.Int, len(delegations))
	total := math.ZeroInt()

	for _, d := range delegations {
		valAddr, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			return nil, math.ZeroInt(), err
		}

		val, err := k.stakingKeeper.GetValidator(ctx, valAddr)
		if err != nil {
			return nil, math.ZeroInt(), err
		}

		// Convert shares -> tokens and truncate to integer tokens.
		tokensDec := val.TokensFromSharesTruncated(d.Shares)
		tokensInt := tokensDec.TruncateInt()
		if tokensInt.IsZero() {
			continue
		}

		key := valAddr.String()
		prev, ok := stakeByValidator[key]
		if ok {
			stakeByValidator[key] = prev.Add(tokensInt)
		} else {
			stakeByValidator[key] = tokensInt
		}
		total = total.Add(tokensInt)
	}

	return stakeByValidator, total, nil
}

// EqualWeightTarget computes an equal-weight target distribution across the given validator set.
// Any remainder from integer division is assigned deterministically to the first validators.
func (k Keeper) EqualWeightTarget(totalStake math.Int, targetValidators []sdk.ValAddress) (map[string]math.Int, error) {
	n := len(targetValidators)
	if n == 0 {
		return nil, fmt.Errorf("target validators list is empty")
	}
	if totalStake.IsNegative() {
		return nil, fmt.Errorf("total stake cannot be negative")
	}

	nInt := math.NewInt(int64(n))
	base := totalStake.Quo(nInt)
	remainderCount := totalStake.Mod(nInt).Int64()

	out := make(map[string]math.Int, n)
	for i, val := range targetValidators {
		amt := base
		if int64(i) < remainderCount {
			amt = amt.Add(math.OneInt())
		}
		out[val.String()] = amt
	}
	return out, nil
}

// ComputeDeltas returns target-current per validator and applies the rebalance threshold.
// Deltas within the threshold are treated as zero.
func (k Keeper) ComputeDeltas(target, current map[string]math.Int, totalStake math.Int, bp uint32) (map[string]math.Int, error) {
	threshold := totalStake.Mul(math.NewInt(int64(bp))).Quo(math.NewInt(10_000))

	allKeys := make(map[string]struct{})
	for key := range target {
		allKeys[key] = struct{}{}
	}
	for key := range current {
		allKeys[key] = struct{}{}
	}

	deltas := make(map[string]math.Int, len(allKeys))
	for key := range allKeys {
		t := target[key]
		if t.IsNil() {
			t = math.ZeroInt()
		}
		c := current[key]
		if c.IsNil() {
			c = math.ZeroInt()
		}
		delta := t.Sub(c)
		if delta.Abs().LT(threshold) {
			delta = math.ZeroInt()
		}
		deltas[key] = delta
	}
	return deltas, nil
}

func minInt(a, b math.Int) math.Int {
	if a.LT(b) {
		return a
	}
	return b
}

func (k Keeper) emitRedelegationFailureEvent(ctx context.Context, del sdk.AccAddress, srcVal, dstVal sdk.ValAddress, coin sdk.Coin, reason string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRedelegationFailed,
			sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
			sdk.NewAttribute(types.AttributeKeySrcValidator, srcVal.String()),
			sdk.NewAttribute(types.AttributeKeyDstValidator, dstVal.String()),
			sdk.NewAttribute(types.AttributeKeyAmount, coin.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyDenom, coin.Denom),
			sdk.NewAttribute(types.AttributeKeyReason, reason),
		),
	)
}

func (k Keeper) emitUndelegationFailureEvent(ctx context.Context, del sdk.AccAddress, val sdk.ValAddress, coin sdk.Coin, reason string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeUndelegationFailed,
			sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
			sdk.NewAttribute(types.AttributeKeyValidator, val.String()),
			sdk.NewAttribute(types.AttributeKeyAmount, coin.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyDenom, coin.Denom),
			sdk.NewAttribute(types.AttributeKeyReason, reason),
		),
	)
}

// PickBestRedelegation selects a single (src, dst, amount) move based on deltas.
// Ties are broken deterministically by (src,dst) ordering. If maxMove is non-zero, it caps the amount.
func (k Keeper) PickBestRedelegation(
	deltas map[string]math.Int,
	keys []string,
	blocked map[string]map[string]struct{},
	maxMove math.Int,
) (src string, dst string, amt math.Int, ok bool) {
	bestAmt := math.ZeroInt()
	bestDstNeed := math.ZeroInt()
	bestSrc := ""
	bestDst := ""

	for _, s := range keys {
		ds := deltas[s]
		if !ds.IsNegative() {
			continue
		}
		srcOver := ds.Abs()
		for _, d := range keys {
			dd := deltas[d]
			if !dd.IsPositive() {
				continue
			}
			if m, exists := blocked[s]; exists {
				if _, isBlocked := m[d]; isBlocked {
					continue
				}
			}
			move := minInt(srcOver, dd)
			if !maxMove.IsZero() {
				move = minInt(move, maxMove)
			}
			if move.IsZero() {
				continue
			}
			// Prefer larger moves.
			// If move ties (common when capped), prefer destination with larger deficit.
			// Final tie-break stays deterministic on (src,dst).
			if move.GT(bestAmt) ||
				(move.Equal(bestAmt) && (dd.GT(bestDstNeed) ||
					(dd.Equal(bestDstNeed) && (s < bestSrc || (s == bestSrc && d < bestDst))))) {
				bestAmt = move
				bestDstNeed = dd
				bestSrc = s
				bestDst = d
			}
		}
	}

	if bestAmt.IsZero() {
		return "", "", math.ZeroInt(), false
	}
	return bestSrc, bestDst, bestAmt, true
}

// PickResidualUndelegation selects a single undelegation as a fallback when redelegation isn't possible.
// It targets the most overweight validator among deltas, skipping any keys in skipVals (e.g. sources that
// already failed undelegation this block). Amount is capped by MaxMovePerOp (if set).
func (k Keeper) PickResidualUndelegation(ctx context.Context, deltas map[string]math.Int, skipVals map[string]struct{}) (val string, amt math.Int, ok bool, err error) {
	maxMove, err := k.GetMaxMovePerOp(ctx)
	if err != nil {
		return "", math.ZeroInt(), false, err
	}

	bestVal := ""
	bestOver := math.ZeroInt()

	keys := make([]string, 0, len(deltas))
	for k := range deltas {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if skipVals != nil {
			if _, skip := skipVals[k]; skip {
				continue
			}
		}
		d := deltas[k]
		if !d.IsNegative() {
			continue
		}
		over := d.Abs()
		if over.GT(bestOver) || (over.Equal(bestOver) && (bestVal == "" || k < bestVal)) {
			bestOver = over
			bestVal = k
		}
	}

	if bestVal == "" || bestOver.IsZero() {
		return "", math.ZeroInt(), false, nil
	}

	move := bestOver
	if !maxMove.IsZero() {
		move = minInt(move, maxMove)
	}
	if move.IsZero() {
		return "", math.ZeroInt(), false, nil
	}

	return bestVal, move, true, nil
}

// ProcessRebalance compares current stake to target and applies up to MaxOpsPerBlock operations.
// It is intended to be called from EndBlock after pending queues are cleaned up.
func (k Keeper) ProcessRebalance(ctx context.Context) error {
	// Fast-path exits: not configured, no targets, or nothing bonded.
	del, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if del.Empty() {
		return nil
	}
	targetVals, err := k.GetTargetBondedValidators(ctx)
	if err != nil {
		return err
	}
	if len(targetVals) == 0 {
		return nil
	}
	stakeByValidator, total, err := k.GetDelegatorStakeByValidator(ctx, del)
	if err != nil {
		return err
	}
	if total.IsZero() {
		return nil
	}

	// Load params once for this rebalance pass.
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Compute equal-weight targets and deltas (threshold applied inside ComputeDeltas).
	target, err := k.EqualWeightTarget(total, targetVals)
	if err != nil {
		return err
	}
	deltas, err := k.ComputeDeltas(target, stakeByValidator, total, params.RebalanceThresholdBp)
	if err != nil {
		return err
	}

	// Nothing exceeds the threshold.
	allZero := true
	for _, d := range deltas {
		if !d.IsZero() {
			allZero = false
			break
		}
	}
	if allZero {
		return nil
	}

	// Apply params to the operation loop.
	maxOps := params.MaxOpsPerBlock
	useUndel := params.UseUndelegateFallback
	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return err
	}

	// Apply operations (redelegate first, then optional undelegate fallback).
	blocked := make(map[string]map[string]struct{})
	undelSkipped := make(map[string]struct{})
	keys := make([]string, 0, len(deltas))
	for key := range deltas {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	maxMove := params.MaxMovePerOp
	if maxMove.IsNil() {
		maxMove = math.ZeroInt()
	}

	var opsDone uint32
	for opsDone < maxOps {
		srcKey, dstKey, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)

		if ok {
			srcVal, err := sdk.ValAddressFromBech32(srcKey)
			if err != nil {
				return err
			}
			dstVal, err := sdk.ValAddressFromBech32(dstKey)
			if err != nil {
				return err
			}
			coin := sdk.NewCoin(bondDenom, amt)

			if k.CanBeginRedelegation(ctx, del, srcVal, dstVal, coin) {
				if _, err := k.BeginTrackedRedelegation(ctx, del, srcVal, dstVal, coin); err == nil {
					deltas[srcKey] = deltas[srcKey].Add(amt)
					deltas[dstKey] = deltas[dstKey].Sub(amt)
					opsDone++
					continue
				} else {
					k.emitRedelegationFailureEvent(ctx, del, srcVal, dstVal, coin, err.Error())
				}
			}

			if blocked[srcKey] == nil {
				blocked[srcKey] = make(map[string]struct{})
			}
			blocked[srcKey][dstKey] = struct{}{}
			continue
		}

		if !useUndel {
			break
		}

		valKey, undelAmt, ok, err := k.PickResidualUndelegation(ctx, deltas, undelSkipped)
		if err != nil {
			return err
		}
		if !ok {
			break
		}

		valAddr, err := sdk.ValAddressFromBech32(valKey)
		if err != nil {
			return err
		}
		coin := sdk.NewCoin(bondDenom, undelAmt)
		if _, _, err := k.BeginTrackedUndelegation(ctx, del, valAddr, coin); err != nil {
			k.emitUndelegationFailureEvent(ctx, del, valAddr, coin, err.Error())
			undelSkipped[valKey] = struct{}{}
			continue
		}
		deltas[valKey] = deltas[valKey].Add(undelAmt)
		opsDone++
	}

	if opsDone > 0 {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeRebalanceSummary,
				sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
				sdk.NewAttribute(types.AttributeKeyOpsDone, strconv.FormatUint(uint64(opsDone), 10)),
				sdk.NewAttribute(types.AttributeKeyUseFallback, strconv.FormatBool(useUndel)),
			),
		)
	}

	return nil
}
