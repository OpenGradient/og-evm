package keeper

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ComputeExpectedBondedPrincipal sums bond tokens for del across validators in Bonded status
// (TokensFromSharesTruncated). Target for CommunityPool totalStaked.
func (k Keeper) ComputeExpectedBondedPrincipal(ctx context.Context, del sdk.AccAddress) (math.Int, error) {
	delegations, err := k.getAllDelegatorDelegations(ctx, del)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("get delegator delegations: %w", err)
	}
	total := math.ZeroInt()
	for _, d := range delegations {
		valAddr, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			return math.ZeroInt(), err
		}
		val, err := k.stakingKeeper.GetValidator(ctx, valAddr)
		if err != nil {
			return math.ZeroInt(), fmt.Errorf("get validator %s: %w", d.ValidatorAddress, err)
		}
		if val.Status != stakingtypes.Bonded {
			continue
		}
		tokensDec := val.TokensFromSharesTruncated(d.Shares)
		tokensInt := tokensDec.TruncateInt()
		if tokensInt.IsPositive() {
			total = total.Add(tokensInt)
		}
	}
	return total, nil
}

// ComputeExpectedPendingRebalancePrincipal sums staking UBD balances for immature module-queue
// triples (delegator|validator|completion), deduped — not all UBDs on the account.
func (k Keeper) ComputeExpectedPendingRebalancePrincipal(ctx context.Context, del sdk.AccAddress, blockTime time.Time) (math.Int, error) {
	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("bond denom: %w", err)
	}
	batches, err := k.loadImmatureUndelegationBatches(ctx, blockTime)
	if err != nil {
		return math.ZeroInt(), err
	}
	poolBech := del.String()
	type tripleKey struct {
		delegator      string
		validator      string
		completionTime time.Time
	}
	seen := make(map[tripleKey]struct{})
	sum := math.ZeroInt()
	for _, b := range batches {
		for _, e := range b.queued.Entries {
			if e.DelegatorAddress != poolBech || e.Balance.Denom != bondDenom {
				continue
			}
			key := tripleKey{
				delegator:      poolBech,
				validator:      e.ValidatorAddress,
				completionTime: normalizeCompletionTime(b.completionTime),
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			valAddr, err := sdk.ValAddressFromBech32(e.ValidatorAddress)
			if err != nil {
				return math.ZeroInt(), err
			}
			tripleBalance, err := k.sumStakingUnbondingEntriesMatchingCompletion(ctx, del, valAddr, key.completionTime)
			if err != nil {
				return math.ZeroInt(), fmt.Errorf(
					"pending rebalance ubd (%s,%s,%s): %w",
					key.delegator, key.validator, key.completionTime.Format(time.RFC3339Nano), err,
				)
			}
			sum = sum.Add(tripleBalance)
		}
	}
	return sum, nil
}

// ComputeExpectedCommunityPoolStakedBuckets returns values for reconcileStakedBuckets from staking
// and the immature module undelegation queue.
func (k Keeper) ComputeExpectedCommunityPoolStakedBuckets(ctx context.Context, del sdk.AccAddress, blockTime time.Time) (bonded math.Int, pending math.Int, err error) {
	bonded, err = k.ComputeExpectedBondedPrincipal(ctx, del)
	if err != nil {
		return math.ZeroInt(), math.ZeroInt(), err
	}
	pending, err = k.ComputeExpectedPendingRebalancePrincipal(ctx, del, blockTime)
	if err != nil {
		return math.ZeroInt(), math.ZeroInt(), err
	}
	return bonded, pending, nil
}
