package keeper

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	pooltypes "github.com/cosmos/evm/x/poolrebalancer/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type mockEVMKeeper struct {
	methods   []string
	froms     []common.Address
	contracts []common.Address

	errByMethod map[string]error
}

func (m *mockEVMKeeper) CallEVM(
	ctx sdk.Context,
	abi abi.ABI,
	from, contract common.Address,
	commit bool,
	gasCap *big.Int,
	method string,
	args ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	m.methods = append(m.methods, method)
	m.froms = append(m.froms, from)
	m.contracts = append(m.contracts, contract)
	if err, ok := m.errByMethod[method]; ok {
		return nil, err
	}
	return &evmtypes.MsgEthereumTxResponse{}, nil
}

func TestMaybeRunCommunityPoolAutomation_SkipsWhenPoolDelegatorUnset(t *testing.T) {
	ctx, k := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
	require.Empty(t, mockEVM.methods)
}

func TestMaybeRunCommunityPoolAutomation_SkipsWhenEVMKeeperUnset(t *testing.T) {
	ctx, k := newTestKeeper(t)

	del := sdk.AccAddress(bytes.Repeat([]byte{7}, 20))
	params := pooltypes.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
}

func TestMaybeRunCommunityPoolAutomation_CallsHarvestThenStake(t *testing.T) {
	ctx, k := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	params := pooltypes.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
	require.Equal(t, []string{"harvest", "stake"}, mockEVM.methods)

	expectedContract := common.BytesToAddress(del.Bytes())
	require.Len(t, mockEVM.froms, 2)
	require.Len(t, mockEVM.contracts, 2)
	require.Equal(t, pooltypes.ModuleEVMAddress, mockEVM.froms[0])
	require.Equal(t, pooltypes.ModuleEVMAddress, mockEVM.froms[1])
	require.Equal(t, expectedContract, mockEVM.contracts[0])
	require.Equal(t, expectedContract, mockEVM.contracts[1])
}

func TestMaybeRunCommunityPoolAutomation_HarvestFailureDoesNotBlockStake(t *testing.T) {
	ctx, k := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		errByMethod: map[string]error{
			"harvest": errors.New("mock harvest failure"),
		},
	}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	params := pooltypes.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
	require.Equal(t, []string{"harvest", "stake"}, mockEVM.methods)
}

