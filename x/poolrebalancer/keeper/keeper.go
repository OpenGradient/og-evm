package keeper

import (
	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// Keeper holds state and dependencies for the pool rebalancer.
type Keeper struct {
	storeService  store.KVStoreService
	cdc           codec.BinaryCodec
	stakingKeeper *stakingkeeper.Keeper
	authority     sdk.AccAddress
}

// NewKeeper returns a new Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	stakingKeeper *stakingkeeper.Keeper,
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
