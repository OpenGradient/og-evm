package keeper

import (
	"context"

	"github.com/cosmos/evm/x/bridge/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns an implementation of the bridge QueryServer interface.
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{Keeper: k}
}

var _ types.QueryServer = queryServer{}

// Params returns the current bridge module parameters.
func (s queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: s.GetParams(ctx)}, nil
}

// BridgeStatus returns the current bridge status and aggregate counters.
func (s queryServer) BridgeStatus(goCtx context.Context, _ *types.QueryBridgeStatusRequest) (*types.QueryBridgeStatusResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := s.GetParams(ctx)

	return &types.QueryBridgeStatusResponse{
		Enabled:            params.Enabled,
		TotalMinted:        s.GetTotalMinted(ctx),
		TotalBurned:        s.GetTotalBurned(ctx),
		AuthorizedContract: params.AuthorizedContract,
	}, nil
}
