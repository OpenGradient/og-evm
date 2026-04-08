package keeper

import (
	"cosmossdk.io/core/store"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/evm/x/poolrebalancer/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper holds state and dependencies for the pool rebalancer.
type Keeper struct {
	storeService  store.KVStoreService
	transientKey  *storetypes.TransientStoreKey
	cdc           codec.BinaryCodec
	stakingKeeper types.StakingKeeper
	evmKeeper     types.EVMKeeper
	accountKeeper types.AccountKeeper
	authority     sdk.AccAddress
}

// NewKeeper returns a new Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	transientKey *storetypes.TransientStoreKey,
	stakingKeeper types.StakingKeeper,
	authority sdk.AccAddress,
	evmKeeper types.EVMKeeper,
	accountKeeper types.AccountKeeper,
) Keeper {
	if err := sdk.VerifyAddressFormat(authority); err != nil {
		panic(err)
	}
	return Keeper{
		storeService:  storeService,
		transientKey:  transientKey,
		cdc:           cdc,
		stakingKeeper: stakingKeeper,
		evmKeeper:     evmKeeper,
		accountKeeper: accountKeeper,
		authority:     authority,
	}
}
