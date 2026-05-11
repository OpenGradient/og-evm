package keeper

import (
	"time"

	"github.com/cosmos/evm/x/svip/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper grants access to the SVIP module state.
type Keeper struct {
	cdc       codec.BinaryCodec
	storeKey  storetypes.StoreKey
	authority sdk.AccAddress
	bk        types.BankKeeper
	ak        types.AccountKeeper
}

// NewKeeper creates a new SVIP keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	authority sdk.AccAddress,
	bk types.BankKeeper,
	ak types.AccountKeeper,
) Keeper {
	if err := sdk.VerifyAddressFormat(authority); err != nil {
		panic(err)
	}
	return Keeper{
		cdc:       cdc,
		storeKey:  storeKey,
		authority: authority,
		bk:        bk,
		ak:        ak,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", types.ModuleName)
}

// --- State accessors ---

// GetTotalDistributed returns the amount of tokens distributed since the last (re)activation.
func (k Keeper) GetTotalDistributed(ctx sdk.Context) sdkmath.Int {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.TotalDistributedKey)
	if bz == nil {
		return sdkmath.ZeroInt()
	}
	var val sdkmath.Int
	if err := val.Unmarshal(bz); err != nil {
		panic(err)
	}
	return val
}

// SetTotalDistributed sets the amount of tokens distributed since the last (re)activation.
func (k Keeper) SetTotalDistributed(ctx sdk.Context, val sdkmath.Int) {
	store := ctx.KVStore(k.storeKey)
	bz, err := val.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.TotalDistributedKey, bz)
}

// AddTotalDistributed increments the total distributed counter by amount.
func (k Keeper) AddTotalDistributed(ctx sdk.Context, amount sdkmath.Int) {
	current := k.GetTotalDistributed(ctx)
	k.SetTotalDistributed(ctx, current.Add(amount))
}

// GetActivationTime returns the time SVIP was activated.
func (k Keeper) GetActivationTime(ctx sdk.Context) time.Time {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ActivationTimeKey)
	if bz == nil {
		return time.Time{}
	}
	var t time.Time
	if err := t.UnmarshalBinary(bz); err != nil {
		panic(err)
	}
	return t
}

// SetActivationTime stores the time SVIP was activated.
func (k Keeper) SetActivationTime(ctx sdk.Context, t time.Time) {
	store := ctx.KVStore(k.storeKey)
	bz, err := t.MarshalBinary()
	if err != nil {
		panic(err)
	}
	store.Set(types.ActivationTimeKey, bz)
}

// GetLastBlockTime returns the timestamp of the last processed block.
func (k Keeper) GetLastBlockTime(ctx sdk.Context) time.Time {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.LastBlockTimeKey)
	if bz == nil {
		return time.Time{}
	}
	var t time.Time
	if err := t.UnmarshalBinary(bz); err != nil {
		panic(err)
	}
	return t
}

// SetLastBlockTime stores the timestamp of the last processed block.
func (k Keeper) SetLastBlockTime(ctx sdk.Context, t time.Time) {
	store := ctx.KVStore(k.storeKey)
	bz, err := t.MarshalBinary()
	if err != nil {
		panic(err)
	}
	store.Set(types.LastBlockTimeKey, bz)
}

// GetPoolBalanceAtActivation returns the pool balance snapshot at activation time.
func (k Keeper) GetPoolBalanceAtActivation(ctx sdk.Context) sdkmath.Int {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.PoolBalanceAtActivationKey)
	if bz == nil {
		return sdkmath.ZeroInt()
	}
	var val sdkmath.Int
	if err := val.Unmarshal(bz); err != nil {
		panic(err)
	}
	return val
}

// SetPoolBalanceAtActivation stores the pool balance snapshot at activation time.
func (k Keeper) SetPoolBalanceAtActivation(ctx sdk.Context, val sdkmath.Int) {
	store := ctx.KVStore(k.storeKey)
	bz, err := val.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.PoolBalanceAtActivationKey, bz)
}

// GetTotalPausedSeconds returns the cumulative seconds SVIP has been paused.
func (k Keeper) GetTotalPausedSeconds(ctx sdk.Context) int64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.TotalPausedSecondsKey)
	if bz == nil {
		return 0
	}
	var val sdkmath.Int
	if err := val.Unmarshal(bz); err != nil {
		panic(err)
	}
	return val.Int64()
}

// SetTotalPausedSeconds stores the cumulative seconds SVIP has been paused.
func (k Keeper) SetTotalPausedSeconds(ctx sdk.Context, secs int64) {
	store := ctx.KVStore(k.storeKey)
	val := sdkmath.NewInt(secs)
	bz, err := val.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.TotalPausedSecondsKey, bz)
}

var isTrue = []byte("0x01")

// GetActivated returns whether SVIP reward distribution is active.
func (k Keeper) GetActivated(ctx sdk.Context) bool {
	return ctx.KVStore(k.storeKey).Has(types.ActivatedKey)
}

// SetActivated stores the activated flag using a presence-based pattern.
func (k Keeper) SetActivated(ctx sdk.Context, activated bool) {
	store := ctx.KVStore(k.storeKey)
	if activated {
		store.Set(types.ActivatedKey, isTrue)
		return
	}
	store.Delete(types.ActivatedKey)
}

// GetPaused returns whether SVIP is in emergency pause state.
func (k Keeper) GetPaused(ctx sdk.Context) bool {
	return ctx.KVStore(k.storeKey).Has(types.PausedKey)
}

// SetPaused stores the paused flag using a presence-based pattern.
func (k Keeper) SetPaused(ctx sdk.Context, paused bool) {
	store := ctx.KVStore(k.storeKey)
	if paused {
		store.Set(types.PausedKey, isTrue)
		return
	}
	store.Delete(types.PausedKey)
}

// getPoolBalance reads the live pool balance from x/bank (not from custom state).
func (k Keeper) getPoolBalance(ctx sdk.Context) sdkmath.Int {
	moduleAddr := k.ak.GetModuleAddress(types.ModuleName)
	return k.bk.GetBalance(ctx, moduleAddr, k.getDenom(ctx)).Amount
}

// getDenom returns the native denom at runtime via the EVM module's global config.
func (k Keeper) getDenom(_ sdk.Context) string {
	return evmtypes.GetEVMCoinDenom()
}
