package keeper

import (
	"github.com/cosmos/evm/x/bridge/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper grants access to the bridge module state.
type Keeper struct {
	cdc       codec.BinaryCodec
	storeKey  storetypes.StoreKey
	authority sdk.AccAddress
	bk        types.BankKeeper
	ak        types.AccountKeeper
}

// NewKeeper creates a new bridge keeper.
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

// GetTotalMinted returns the cumulative amount minted by the bridge.
func (k Keeper) GetTotalMinted(ctx sdk.Context) sdkmath.Int {
	return k.getInt(ctx, types.TotalMintedStoreKey)
}

// SetTotalMinted stores the cumulative amount minted by the bridge.
func (k Keeper) SetTotalMinted(ctx sdk.Context, amount sdkmath.Int) {
	k.setInt(ctx, types.TotalMintedStoreKey, amount)
}

// AddTotalMinted increments the cumulative minted amount.
func (k Keeper) AddTotalMinted(ctx sdk.Context, amount sdkmath.Int) {
	k.SetTotalMinted(ctx, k.GetTotalMinted(ctx).Add(amount))
}

// GetTotalBurned returns the cumulative amount burned by the bridge.
func (k Keeper) GetTotalBurned(ctx sdk.Context) sdkmath.Int {
	return k.getInt(ctx, types.TotalBurnedStoreKey)
}

// SetTotalBurned stores the cumulative amount burned by the bridge.
func (k Keeper) SetTotalBurned(ctx sdk.Context, amount sdkmath.Int) {
	k.setInt(ctx, types.TotalBurnedStoreKey, amount)
}

// AddTotalBurned increments the cumulative burned amount.
func (k Keeper) AddTotalBurned(ctx sdk.Context, amount sdkmath.Int) {
	k.SetTotalBurned(ctx, k.GetTotalBurned(ctx).Add(amount))
}

func (k Keeper) getDenom() string {
	return evmtypes.GetEVMCoinDenom()
}

func (k Keeper) getInt(ctx sdk.Context, key []byte) sdkmath.Int {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(key)
	if bz == nil {
		return sdkmath.ZeroInt()
	}

	var amount sdkmath.Int
	if err := amount.Unmarshal(bz); err != nil {
		panic(err)
	}

	return amount
}

func (k Keeper) setInt(ctx sdk.Context, key []byte, amount sdkmath.Int) {
	store := ctx.KVStore(k.storeKey)
	bz, err := amount.Marshal()
	if err != nil {
		panic(err)
	}

	store.Set(key, bz)
}
