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

type genesisMockStakingKeeper struct{}
type genesisMockEVM struct{}

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
func (genesisMockStakingKeeper) BeginRedelegation(context.Context, sdk.AccAddress, sdk.ValAddress, sdk.ValAddress, math.LegacyDec) (time.Time, error) {
	return time.Time{}, nil
}
func (genesisMockStakingKeeper) UnbondingTime(context.Context) (time.Duration, error) { return time.Hour, nil }
func (genesisMockStakingKeeper) BondDenom(context.Context) (string, error)             { return "stake", nil }
func (genesisMockStakingKeeper) DelegatorDelegations(context.Context, *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	return &stakingtypes.QueryDelegatorDelegationsResponse{}, nil
}
func (genesisMockEVM) CallEVM(
	sdk.Context, abi.ABI, common.Address, common.Address, bool, *big.Int, string, ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	return &evmtypes.MsgEthereumTxResponse{}, nil
}
func (genesisMockEVM) IsContract(sdk.Context, common.Address) bool { return true }

func TestGenesis_ExportsAndRestoresPendingRedelegationsOnly(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey).WithBlockTime(time.Unix(2_000, 0))
	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	sk := genesisMockStakingKeeper{}
	k := keeper.NewKeeper(cdc, storeService, tKey, sk, sk, nil, authority, genesisMockEVM{}, nil)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParamsForGenesis(ctx, params))
	require.NoError(t, k.SetPendingRedelegation(ctx, types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(10)),
		CompletionTime:      ctx.BlockTime().Add(time.Hour),
	}))

	exported := ExportGenesis(ctx, k)
	require.Len(t, exported.PendingRedelegations, 1)
	require.Equal(t, del.String(), exported.PendingRedelegations[0].DelegatorAddress)
}
