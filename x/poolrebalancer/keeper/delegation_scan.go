package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const delegatorDelegationScanLimit = ^uint16(0)

func (k Keeper) getAllDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress) ([]stakingtypes.Delegation, error) {
	return k.getDelegatorDelegationsWithLimit(ctx, delegator, delegatorDelegationScanLimit)
}

func (k Keeper) getDelegatorDelegationsWithLimit(ctx context.Context, delegator sdk.AccAddress, limit uint16) ([]stakingtypes.Delegation, error) {
	delegations, err := k.stakingKeeper.GetDelegatorDelegations(ctx, delegator, limit)
	if err != nil {
		return nil, err
	}
	if len(delegations) >= int(limit) {
		return nil, fmt.Errorf(
			"delegation scan reached maxRetrieve=%d for delegator %s; refusing to use a possibly truncated staking view",
			limit,
			delegator.String(),
		)
	}
	return delegations, nil
}
