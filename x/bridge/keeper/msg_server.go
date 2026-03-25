package keeper

import (
	"context"

	"github.com/cosmos/evm/x/bridge/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the bridge MsgServer interface.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// UpdateParams updates the bridge module parameters. Governance-only.
func (s msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := s.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// SetAuthorizedContract updates only the authorized bridge contract address. Governance-only.
func (s msgServer) SetAuthorizedContract(goCtx context.Context, msg *types.MsgSetAuthorizedContract) (*types.MsgSetAuthorizedContractResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	params := s.GetParams(ctx)
	params.AuthorizedContract = msg.ContractAddress
	if err := s.SetParams(ctx, params); err != nil {
		return nil, err
	}

	return &types.MsgSetAuthorizedContractResponse{}, nil
}
