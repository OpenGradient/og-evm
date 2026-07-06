package svip

import (
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/evm/x/svip/keeper"
	"github.com/cosmos/evm/x/svip/types"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
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

	// Rebuild the decay-curve state, which lives in the KV store rather than the genesis
	// proto. A fresh chain is not activated, so there is nothing to rebuild. On export/import
	// of a running chain, d comes back exactly from the half-life, and S re-anchors to the
	// live pool balance (bank inits before svip). That balance already reflects how far the
	// curve has decayed, so mid-decay the re-anchor only drifts by truncation dust.
	if data.Activated && data.Params.HalfLifeSeconds > 0 {
		d, err := keeper.ComputeDecayFactor(data.Params.HalfLifeSeconds)
		if err != nil {
			panic(errorsmod.Wrap(err, "could not compute svip decay factor at genesis"))
		}
		k.SetDecayFactor(ctx, d)
		k.SetScheduledRemaining(ctx, sdkmath.LegacyNewDecFromInt(k.GetLivePoolBalance(ctx)))
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
