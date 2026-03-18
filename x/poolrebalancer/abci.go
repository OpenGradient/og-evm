package poolrebalancer

import (
	"github.com/cosmos/evm/x/poolrebalancer/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker runs at end of block: complete matured redelegations/undelegations, then process rebalance.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) error {
	if err := k.CompletePendingRedelegations(ctx); err != nil {
		return err
	}
	if err := k.CompletePendingUndelegations(ctx); err != nil {
		return err
	}
	if err := k.ProcessRebalance(ctx); err != nil {
		return err
	}
	return nil
}
