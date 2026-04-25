package poolrebalancer

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"
)

type genesisMockEVMKeeper struct{}

type genesisMockStakingKeeper struct{}

func (genesisMockStakingKeeper) GetBondedValidatorsByPower(context.Context) ([]stakingtypes.Validator, error) {
	return nil, nil
}

func (genesisMockStakingKeeper) GetDelegatorDelegations(context.Context, sdk.AccAddress, uint16) ([]stakingtypes.Delegation, error) {
	return nil, nil
}

func (genesisMockStakingKeeper) GetValidator(context.Context, sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, nil
}

func (genesisMockStakingKeeper) GetDelegation(context.Context, sdk.AccAddress, sdk.ValAddress) (stakingtypes.Delegation, error) {
	return stakingtypes.Delegation{}, nil
}

func (genesisMockStakingKeeper) GetUnbondingDelegation(context.Context, sdk.AccAddress, sdk.ValAddress) (stakingtypes.UnbondingDelegation, error) {
	return stakingtypes.UnbondingDelegation{}, nil
}

func (genesisMockStakingKeeper) BeginRedelegation(context.Context, sdk.AccAddress, sdk.ValAddress, sdk.ValAddress, math.LegacyDec) (time.Time, error) {
	return time.Time{}, nil
}

func (genesisMockStakingKeeper) Undelegate(context.Context, sdk.AccAddress, sdk.ValAddress, math.LegacyDec) (time.Time, math.Int, error) {
	return time.Time{}, math.ZeroInt(), nil
}

func (genesisMockStakingKeeper) UnbondingTime(context.Context) (time.Duration, error) {
	return 0, nil
}

func (genesisMockStakingKeeper) BondDenom(context.Context) (string, error) {
	return "stake", nil
}

func (genesisMockEVMKeeper) CallEVM(
	_ sdk.Context,
	_ abi.ABI,
	_, _ common.Address,
	_ bool,
	_ *big.Int,
	_ string,
	_ ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	return &evmtypes.MsgEthereumTxResponse{}, nil
}

func (genesisMockEVMKeeper) IsContract(sdk.Context, common.Address) bool { return true }

func TestGenesis_ExportsAndRestoresPendingState(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(2_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := genesisMockStakingKeeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, tKey, stakingK, nil, authority, genesisMockEVMKeeper{}, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:      ctx.BlockTime().Add(time.Hour),
	}))

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: srcVal.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(5)),
		CompletionTime:   ctx.BlockTime().Add(2 * time.Hour),
	}))

	exported := ExportGenesis(ctx, k)
	require.NotNil(t, exported)
	require.Len(t, exported.PendingRedelegations, 1)
	require.Len(t, exported.PendingUndelegations, 1)

	// Restore into a fresh store/keeper.
	storeKey2 := storetypes.NewKVStoreKey(types.ModuleName)
	tKey2 := storetypes.NewTransientStoreKey("transient_test2")
	ctx2 := testutil.DefaultContext(storeKey2, tKey2).WithBlockTime(time.Unix(2_000, 0))

	storeService2 := runtime.NewKVStoreService(storeKey2)
	k2 := keeper.NewKeeper(cdc, storeService2, tKey2, stakingK, nil, authority, genesisMockEVMKeeper{}, nil)

	InitGenesis(ctx2, k2, exported)

	redels, err := k2.GetAllPendingRedelegations(ctx2)
	require.NoError(t, err)
	undels, err := k2.GetAllPendingUndelegations(ctx2)
	require.NoError(t, err)

	require.Len(t, redels, 1)
	require.Len(t, undels, 1)
	require.Equal(t, exported.PendingRedelegations[0].DelegatorAddress, redels[0].DelegatorAddress)
	require.Equal(t, exported.PendingUndelegations[0].DelegatorAddress, undels[0].DelegatorAddress)
}

func TestGenesis_RoundTripPreservesDistinctRedelegationSources(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(3_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := genesisMockStakingKeeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, tKey, stakingK, nil, authority, nil, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	srcB := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))
	dst := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	completion := ctx.BlockTime().Add(time.Hour)

	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcA.String(),
		DstValidatorAddress: dst.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:      completion,
	}))
	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcB.String(),
		DstValidatorAddress: dst.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(15)),
		CompletionTime:      completion,
	}))

	exported := ExportGenesis(ctx, k)
	require.Len(t, exported.PendingRedelegations, 2)

	// Restore into a fresh store/keeper and ensure both source-specific entries survive.
	storeKey2 := storetypes.NewKVStoreKey(types.ModuleName)
	tKey2 := storetypes.NewTransientStoreKey("transient_test2")
	ctx2 := testutil.DefaultContext(storeKey2, tKey2).WithBlockTime(time.Unix(3_000, 0))
	k2 := keeper.NewKeeper(cdc, runtime.NewKVStoreService(storeKey2), tKey2, stakingK, nil, authority, nil, nil)
	InitGenesis(ctx2, k2, exported)

	redels, err := k2.GetAllPendingRedelegations(ctx2)
	require.NoError(t, err)
	require.Len(t, redels, 2)

	srcSet := map[string]struct{}{}
	for _, e := range redels {
		srcSet[e.SrcValidatorAddress] = struct{}{}
		require.Equal(t, dst.String(), e.DstValidatorAddress)
		require.Equal(t, completion.UTC(), e.CompletionTime.UTC())
	}
	_, hasA := srcSet[srcA.String()]
	_, hasB := srcSet[srcB.String()]
	require.True(t, hasA)
	require.True(t, hasB)
}

func TestInitGenesis_RejectsPendingUndelegationsWithoutPoolDelegator(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(2_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := genesisMockStakingKeeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, tKey, stakingK, nil, authority, genesisMockEVMKeeper{}, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	gs := types.DefaultGenesisState()
	gs.PendingUndelegations = []types.PendingUndelegation{{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(5)),
		CompletionTime:   ctx.BlockTime().Add(time.Hour),
	}}

	require.PanicsWithValue(t,
		"failed to validate poolrebalancer genesis state: pending undelegations require params.pool_delegator_address to be set",
		func() { InitGenesis(ctx, k, gs) },
	)
}

func TestInitGenesis_RejectsPendingUndelegationsForDifferentDelegator(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(2_000, 0))

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingK := genesisMockStakingKeeper{}
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := keeper.NewKeeper(cdc, storeService, tKey, stakingK, nil, authority, genesisMockEVMKeeper{}, nil)

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	otherDel := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	gs := types.DefaultGenesisState()
	gs.Params.PoolDelegatorAddress = poolDel.String()
	gs.PendingUndelegations = []types.PendingUndelegation{{
		DelegatorAddress: otherDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(5)),
		CompletionTime:   ctx.BlockTime().Add(time.Hour),
	}}

	require.PanicsWithValue(t,
		`failed to validate poolrebalancer genesis state: pending_undelegations[0].delegator_address "`+otherDel.String()+`" must match params.pool_delegator_address "`+poolDel.String()+`"`,
		func() { InitGenesis(ctx, k, gs) },
	)
}

func TestGenesisState_Validate_PendingEntries(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	completion := time.Unix(2_000, 0).UTC()
	validCoin := sdk.NewCoin("stake", math.NewInt(10))

	validRedel := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              validCoin,
		CompletionTime:      completion,
	}
	validUndel := types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: srcVal.String(),
		Balance:          validCoin,
		CompletionTime:   completion,
	}

	t.Run("default genesis valid", func(t *testing.T) {
		require.NoError(t, types.DefaultGenesisState().Validate())
	})

	t.Run("valid pending entries", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		gs.Params.PoolDelegatorAddress = del.String()
		gs.PendingRedelegations = []types.PendingRedelegation{validRedel}
		gs.PendingUndelegations = []types.PendingUndelegation{validUndel}
		require.NoError(t, gs.Validate())
	})

	t.Run("invalid redelegation delegator", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		bad := validRedel
		bad.DelegatorAddress = "notabech32"
		gs.PendingRedelegations = []types.PendingRedelegation{bad}
		require.Error(t, gs.Validate())
	})

	t.Run("invalid redelegation self delegate", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		bad := validRedel
		bad.DstValidatorAddress = bad.SrcValidatorAddress
		gs.PendingRedelegations = []types.PendingRedelegation{bad}
		require.Error(t, gs.Validate())
	})

	t.Run("invalid redelegation non positive amount", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		bad := validRedel
		bad.Amount = sdk.NewCoin("stake", math.ZeroInt())
		gs.PendingRedelegations = []types.PendingRedelegation{bad}
		require.Error(t, gs.Validate())
	})

	t.Run("invalid redelegation zero completion", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		bad := validRedel
		bad.CompletionTime = time.Time{}
		gs.PendingRedelegations = []types.PendingRedelegation{bad}
		require.Error(t, gs.Validate())
	})

	t.Run("invalid undelegation zero completion", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		bad := validUndel
		bad.CompletionTime = time.Time{}
		gs.PendingUndelegations = []types.PendingUndelegation{bad}
		require.Error(t, gs.Validate())
	})

	t.Run("invalid undelegation ownership without pool delegator", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		gs.PendingUndelegations = []types.PendingUndelegation{validUndel}
		err := gs.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "pending undelegations require params.pool_delegator_address to be set")
	})

	t.Run("invalid undelegation ownership mismatch", func(t *testing.T) {
		gs := types.DefaultGenesisState()
		gs.Params.PoolDelegatorAddress = del.String()
		mismatchDel := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
		bad := validUndel
		bad.DelegatorAddress = mismatchDel.String()
		gs.PendingUndelegations = []types.PendingUndelegation{bad}
		err := gs.Validate()
		require.Error(t, err)
		require.EqualError(
			t,
			err,
			`pending_undelegations[0].delegator_address "`+mismatchDel.String()+`" must match params.pool_delegator_address "`+del.String()+`"`,
		)
	})
}
