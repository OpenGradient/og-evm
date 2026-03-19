package keeper

import (
	"context"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// UpdateParams updates module params. Caller must be the governance module account.
func (k *Keeper) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty update params request")
	}

	if k.authority.String() != req.Authority {
		return nil, errorsmod.Wrapf(
			govtypes.ErrInvalidSigner,
			"invalid authority; expected %s, got %s",
			k.authority.String(),
			req.Authority,
		)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := req.Params.Validate(); err != nil {
		return nil, err
	}
	if err := k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
