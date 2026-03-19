package keeper

import (
	"cosmossdk.io/core/store"
	"github.com/cosmos/evm/x/poolrebalancer/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper holds state and dependencies for the pool rebalancer.
type Keeper struct {
	storeService  store.KVStoreService
	cdc           codec.BinaryCodec
	stakingKeeper types.StakingKeeper
	authority     sdk.AccAddress
}

// NewKeeper returns a new Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	stakingKeeper types.StakingKeeper,
	authority sdk.AccAddress,
) Keeper {
	if err := sdk.VerifyAddressFormat(authority); err != nil {
		panic(err)
	}
	return Keeper{
		storeService:  storeService,
		cdc:           cdc,
		stakingKeeper: stakingKeeper,
		authority:     authority,
	}
}
