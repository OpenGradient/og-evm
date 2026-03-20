package keeper

import (
	"context"

	"github.com/cosmos/evm/x/svip/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// queryServer implements the SVIP QueryServer interface.
type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns an implementation of the SVIP QueryServer interface.
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{Keeper: k}
}

var _ types.QueryServer = queryServer{}

// Params returns the current SVIP module parameters.
func (s queryServer) Params(goCtx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.QueryParamsResponse{Params: s.GetParams(ctx)}, nil
}

// PoolState returns the current SVIP pool state including balance and distribution info.
func (s queryServer) PoolState(goCtx context.Context, _ *types.QueryPoolStateRequest) (*types.QueryPoolStateResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	activated := s.GetActivated(ctx)
	paused := s.GetPaused(ctx)
	denom := s.getDenom(ctx)
	moduleAddr := s.ak.GetModuleAddress(types.ModuleName)
	balance := s.bk.GetBalance(ctx, moduleAddr, denom)

	var currentRate math.LegacyDec
	if activated && !paused {
		params := s.GetParams(ctx)
		totalPausedSec := float64(s.GetTotalPausedSeconds(ctx))
		elapsed := ctx.BlockTime().Sub(s.GetActivationTime(ctx)).Seconds() - totalPausedSec
		poolAtAct := s.GetPoolBalanceAtActivation(ctx)
		// Calculate tokens per second at current time
		reward := CalculateBlockReward(params.HalfLifeSeconds, poolAtAct, elapsed, 1.0)
		currentRate = math.LegacyNewDecFromInt(reward)
	} else {
		currentRate = math.LegacyZeroDec()
	}

	return &types.QueryPoolStateResponse{
		PoolBalance:          balance,
		TotalDistributed:     s.GetTotalDistributed(ctx),
		CurrentRatePerSecond: currentRate,
		Activated:            activated,
		Paused:               paused,
		ActivationTime:       s.GetActivationTime(ctx),
	}, nil
}
