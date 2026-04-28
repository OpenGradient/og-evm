package keeper

import (
	"context"
	"fmt"

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
