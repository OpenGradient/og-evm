package poolrebalancer

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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

type abciMockStakingKeeper struct {
	vals        []stakingtypes.Validator
	delegations []stakingtypes.Delegation
}

func (m abciMockStakingKeeper) GetBondedValidatorsByPower(context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, nil
}
func (m abciMockStakingKeeper) GetDelegatorDelegations(context.Context, sdk.AccAddress, uint16) ([]stakingtypes.Delegation, error) {
	return m.delegations, nil
}
func (abciMockStakingKeeper) GetValidator(context.Context, sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, nil
}
func (abciMockStakingKeeper) GetDelegation(context.Context, sdk.AccAddress, sdk.ValAddress) (stakingtypes.Delegation, error) {
	return stakingtypes.Delegation{}, nil
}
func (abciMockStakingKeeper) BeginRedelegation(context.Context, sdk.AccAddress, sdk.ValAddress, sdk.ValAddress, math.LegacyDec) (time.Time, error) {
	return time.Time{}, nil
}
func (abciMockStakingKeeper) UnbondingTime(context.Context) (time.Duration, error) { return time.Hour, nil }
func (abciMockStakingKeeper) BondDenom(context.Context) (string, error)             { return "stake", nil }
func (abciMockStakingKeeper) DelegatorDelegations(context.Context, *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	return &stakingtypes.QueryDelegatorDelegationsResponse{}, nil
}

type abciMockEVM struct{ methods []string }

func (m *abciMockEVM) CallEVM(
	_ sdk.Context,
	_ abi.ABI,
	_, _ common.Address,
	_ bool,
	_ *big.Int,
	method string,
	_ ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	m.methods = append(m.methods, method)
	var ret []byte
	if method == "totalStaked" {
		meth := types.CommunityPoolABI.Methods["totalStaked"]
		enc, err := meth.Outputs.Pack(big.NewInt(1))
		if err != nil {
			return nil, err
		}
		ret = enc
	}
	return &evmtypes.MsgEthereumTxResponse{Ret: ret}, nil
}
func (*abciMockEVM) IsContract(sdk.Context, common.Address) bool { return true }

func countMethod(methods []string, name string) int {
	n := 0
	for _, m := range methods {
		if m == name {
			n++
		}
	}
	return n
}

func TestEndBlocker_UsesBondedOnlyReconcileMethod(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	evm := &abciMockEVM{}
	sk := abciMockStakingKeeper{}
	k := keeper.NewKeeper(cdc, storeService, tKey, sk, sk, nil, authority, evm, nil)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))

	require.Contains(t, evm.methods, "reconcileTotalStaked")
}

func TestEndBlocker_SecondReconcileOnlyWhenSecondPassEnabled(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockHeight(40)
	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	sk := abciMockStakingKeeper{}

	evmDisabled := &abciMockEVM{}
	kDisabled := keeper.NewKeeper(cdc, storeService, tKey, sk, sk, nil, authority, evmDisabled, nil)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	require.NoError(t, kDisabled.SetParams(ctx, params))
	kDisabled.SetCommunityPoolReconcileSecondPassForTesting(false)
	require.NoError(t, EndBlocker(ctx, kDisabled))
	require.Equal(t, 1, countMethod(evmDisabled.methods, "reconcileTotalStaked"))

	evmEnabled := &abciMockEVM{}
	kEnabled := keeper.NewKeeper(cdc, storeService, tKey, sk, sk, nil, authority, evmEnabled, nil)
	require.NoError(t, kEnabled.SetParams(ctx, params))
	kEnabled.SetCommunityPoolReconcileSecondPassForTesting(true)
	require.NoError(t, EndBlocker(ctx, kEnabled))
	require.GreaterOrEqual(t, countMethod(evmEnabled.methods, "reconcileTotalStaked"), 2)
}
