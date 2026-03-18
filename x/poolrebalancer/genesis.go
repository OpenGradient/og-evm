package poolrebalancer

import (
	"fmt"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes module state from genesis.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, gs *types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(fmt.Sprintf("failed to validate %s genesis state: %s", types.ModuleName, err))
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		panic(fmt.Sprintf("failed to set %s params: %s", types.ModuleName, err))
	}
	for _, entry := range gs.PendingRedelegations {
		if err := k.SetPendingRedelegation(ctx, entry); err != nil {
			panic(fmt.Sprintf("failed to restore pending redelegation: %s", err))
		}
	}
	for _, entry := range gs.PendingUndelegations {
		if err := k.SetPendingUndelegation(ctx, entry); err != nil {
			panic(fmt.Sprintf("failed to restore pending undelegation: %s", err))
		}
	}
}

// ExportGenesis exports module state to genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get %s params: %s", types.ModuleName, err))
	}
	redelegations, err := k.GetAllPendingRedelegations(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to export pending redelegations: %s", err))
	}
	undelegations, err := k.GetAllPendingUndelegations(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to export pending undelegations: %s", err))
	}
	return &types.GenesisState{
		Params:               params,
		PendingRedelegations: redelegations,
		PendingUndelegations: undelegations,
	}
}
