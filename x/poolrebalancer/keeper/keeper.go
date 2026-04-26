package keeper

import (
	"sync/atomic"
	"testing"

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
	stakingQuerier types.StakingQuerier
	distrKeeper   types.DistributionKeeper
	evmKeeper     types.EVMKeeper
	accountKeeper types.AccountKeeper
	authority     sdk.AccAddress
	// Second EndBlock reconcile after ProcessRebalance; default off. Shared pointer across Keeper copies.
	communityPoolReconcileSecondPass *atomic.Bool
}

// NewKeeper returns a new Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	transientKey *storetypes.TransientStoreKey,
	stakingKeeper types.StakingKeeper,
	stakingQuerier types.StakingQuerier,
	distrKeeper types.DistributionKeeper,
	authority sdk.AccAddress,
	evmKeeper types.EVMKeeper,
	accountKeeper types.AccountKeeper,
) Keeper {
	if err := sdk.VerifyAddressFormat(authority); err != nil {
		panic(err)
	}
	sp := &atomic.Bool{}
	return Keeper{
		storeService:                     storeService,
		transientKey:                     transientKey,
		cdc:                              cdc,
		stakingKeeper:                    stakingKeeper,
		stakingQuerier:                   stakingQuerier,
		distrKeeper:                      distrKeeper,
		evmKeeper:                        evmKeeper,
		accountKeeper:                    accountKeeper,
		authority:                        authority,
		communityPoolReconcileSecondPass: sp,
	}
}

// SetCommunityPoolReconcileSecondPassForTesting toggles the second reconcile path. No-op unless testing.Testing().
func (k *Keeper) SetCommunityPoolReconcileSecondPassForTesting(v bool) {
	if !testing.Testing() {
		return
	}
	k.communityPoolReconcileSecondPass.Store(v)
}
