package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/query"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const delegatorDelegationPageLimit uint64 = 200

func (k Keeper) getAllDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress) ([]stakingtypes.Delegation, error) {
	if k.stakingQuerier == nil {
		return nil, fmt.Errorf("staking querier is not configured")
	}

	delegatorAddr := delegator.String()
	var (
		out     []stakingtypes.Delegation
		nextKey []byte
	)

	for {
		res, err := k.stakingQuerier.DelegatorDelegations(ctx, &stakingtypes.QueryDelegatorDelegationsRequest{
			DelegatorAddr: delegatorAddr,
			Pagination: &query.PageRequest{
				Key:   nextKey,
				Limit: delegatorDelegationPageLimit,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("delegator delegations page query for %s: %w", delegatorAddr, err)
		}
		for _, dr := range res.DelegationResponses {
			out = append(out, dr.Delegation)
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		nextKey = res.Pagination.NextKey
	}

	return out, nil
}
