package bridge

import (
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/evm/x/bridge/keeper"
	"github.com/cosmos/evm/x/bridge/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the bridge module state from genesis.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, data types.GenesisState) []abci.ValidatorUpdate {
	if err := k.SetParams(ctx, data.Params); err != nil {
		panic(errorsmod.Wrap(err, "could not set bridge parameters at genesis"))
	}

	if data.TotalMinted.IsPositive() {
		k.SetTotalMinted(ctx, data.TotalMinted)
	}
	if data.TotalBurned.IsPositive() {
		k.SetTotalBurned(ctx, data.TotalBurned)
	}

	return []abci.ValidatorUpdate{}
}

// ExportGenesis exports the current bridge module state.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	return &types.GenesisState{
		Params:      k.GetParams(ctx),
		TotalMinted: k.GetTotalMinted(ctx),
		TotalBurned: k.GetTotalBurned(ctx),
	}
}
