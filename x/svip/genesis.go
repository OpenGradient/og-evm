package svip

import (
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/cosmos/evm/x/svip/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the SVIP module genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, data types.GenesisState) []abci.ValidatorUpdate {
	if err := k.SetParams(ctx, data.Params); err != nil {
		panic(errorsmod.Wrap(err, "could not set parameters at genesis"))
	}
	k.SetActivated(ctx, data.Activated)
	k.SetPaused(ctx, data.Paused)
	if data.TotalDistributed.IsPositive() {
		k.SetTotalDistributed(ctx, data.TotalDistributed)
	}
	if !data.ActivationTime.IsZero() {
		k.SetActivationTime(ctx, data.ActivationTime)
		if !data.LastBlockTime.IsZero() {
			k.SetLastBlockTime(ctx, data.LastBlockTime)
		} else {
			k.SetLastBlockTime(ctx, data.ActivationTime)
		}
	}
	if data.TotalPausedSeconds > 0 {
		k.SetTotalPausedSeconds(ctx, data.TotalPausedSeconds)
	}
	if data.PoolBalanceAtActivation.IsPositive() {
		k.SetPoolBalanceAtActivation(ctx, data.PoolBalanceAtActivation)
	}
	return []abci.ValidatorUpdate{}
}

// ExportGenesis exports the SVIP module genesis state.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	return &types.GenesisState{
		Params:                  k.GetParams(ctx),
		TotalDistributed:        k.GetTotalDistributed(ctx),
		ActivationTime:          k.GetActivationTime(ctx),
		PoolBalanceAtActivation: k.GetPoolBalanceAtActivation(ctx),
		LastBlockTime:           k.GetLastBlockTime(ctx),
		TotalPausedSeconds:      k.GetTotalPausedSeconds(ctx),
		Activated:               k.GetActivated(ctx),
		Paused:                  k.GetPaused(ctx),
	}
}
