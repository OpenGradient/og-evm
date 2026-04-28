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
// CommunityPool stake automation delegates through the staking precompile using the same bonded-validator
// query family, while rebalancing intentionally uses this top-power target set for drift correction.
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
	delegations, err := k.getAllDelegatorDelegations(ctx, del)
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

// filterTargetValidators excludes validators from same-block rebalance destinations.
// When a validator was slashed in the previous block, poolrebalancer avoids targeting it in the
// current block and recomputes equal-weight targets across the remaining candidates.
func filterTargetValidators(targetValidators []sdk.ValAddress, excluded map[string]struct{}) []sdk.ValAddress {
	if len(excluded) == 0 {
		return targetValidators
	}

	out := make([]sdk.ValAddress, 0, len(targetValidators))
	for _, val := range targetValidators {
		if _, skip := excluded[val.String()]; skip {
			continue
		}
		out = append(out, val)
	}
	return out
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

// PickBestRedelegation selects a single (src, dst, amount) move based on deltas.
// Ties are broken deterministically by (src,dst) ordering. If maxMove is non-zero, it caps the amount.
func (k Keeper) PickBestRedelegation(
	deltas map[string]math.Int,
	keys []string,
	blocked map[string]map[string]struct{},
	maxMove math.Int,
) (src string, dst string, amt math.Int, ok bool) {
	return k.pickBestRedelegationWithRestrictions(deltas, keys, blocked, maxMove, nil, nil)
}

// pickBestRedelegationWithRestrictions optionally constrains source and destination validators.
// Slash-priority scheduling uses this to force moves away from previously slashed validators before
// falling back to the generic drift-based picker.
func (k Keeper) pickBestRedelegationWithRestrictions(
	deltas map[string]math.Int,
	keys []string,
	blocked map[string]map[string]struct{},
	maxMove math.Int,
	allowedSrc map[string]struct{},
	excludedDst map[string]struct{},
) (src string, dst string, amt math.Int, ok bool) {
	bestAmt := math.ZeroInt()
	bestDstNeed := math.ZeroInt()
	bestSrc := ""
	bestDst := ""

	for _, s := range keys {
		if allowedSrc != nil {
			if _, ok := allowedSrc[s]; !ok {
				continue
			}
		}
		ds := deltas[s]
		if !ds.IsNegative() {
			continue
		}
		srcOver := ds.Abs()
		for _, d := range keys {
			if excludedDst != nil {
				if _, excluded := excludedDst[d]; excluded {
					continue
				}
			}
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

// ProcessRebalance compares current stake to target and applies up to MaxOpsPerBlock operations.
// It is intended to be called from EndBlock after pending queues are cleaned up.
//
// Slash-aware behavior:
// - previous-block slashed validators are excluded from same-block destinations/targets
// - redelegation priority first tries to move stake away from those validators
// - if all target validators were slashed in the previous block, rebalance cleanly no-ops
func (k Keeper) ProcessRebalance(ctx context.Context) error {
	// Fast-path exits: not configured, no targets, or nothing bonded.
	del, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if del.Empty() {
		return nil
	}
	slashedVals, err := k.getPreviousBlockSlashedValidatorsOrEmpty(ctx)
	if err != nil {
		return err
	}
	targetVals, err := k.GetTargetBondedValidators(ctx)
	if err != nil {
		return err
	}
	targetVals = filterTargetValidators(targetVals, slashedVals)
	if len(targetVals) == 0 {
		// Conservatively do nothing for this block when every same-block target was slashed
		// in the previous block.
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
	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return err
	}

	// Apply operations using redelegations only.
	blocked := make(map[string]map[string]struct{})
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
		srcKey, dstKey, amt, ok := "", "", math.ZeroInt(), false
		if len(slashedVals) > 0 {
			srcKey, dstKey, amt, ok = k.pickBestRedelegationWithRestrictions(deltas, keys, blocked, maxMove, slashedVals, slashedVals)
		}
		if !ok {
			srcKey, dstKey, amt, ok = k.PickBestRedelegation(deltas, keys, blocked, maxMove)
		}

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
		break
	}

	if opsDone > 0 {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeRebalanceSummary,
				sdk.NewAttribute(types.AttributeKeyDelegator, del.String()),
				sdk.NewAttribute(types.AttributeKeyOpsDone, strconv.FormatUint(uint64(opsDone), 10)),
			),
		)
	}

	return nil
}
