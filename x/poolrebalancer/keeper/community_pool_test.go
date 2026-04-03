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
	args      [][]any

	errByMethod map[string]error
	failedVM    map[string]string // method -> VmError (non-empty => Failed())

	// isContractFn optionally gates IsContract; nil means all addresses are treated as contracts.
	isContractFn func(common.Address) bool
}

func (m *mockEVMKeeper) IsContract(_ sdk.Context, addr common.Address) bool {
	if m != nil && m.isContractFn != nil {
		return m.isContractFn(addr)
	}
	return true
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
	m.args = append(m.args, append([]any(nil), args...))
	if err, ok := m.errByMethod[method]; ok {
		return nil, err
	}
	vmErr := ""
	if m.failedVM != nil {
		vmErr = m.failedVM[method]
	}
	return &evmtypes.MsgEthereumTxResponse{VmError: vmErr}, nil
}

func TestMaybeRunCommunityPoolAutomation_SkipsWhenPoolDelegatorUnset(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
	require.Empty(t, mockEVM.methods)
}

func TestMaybeRunCommunityPoolAutomation_SkipsWhenEVMKeeperUnset(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{7}, 20))
	params := pooltypes.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, params))

	k.evmKeeper = nil
	require.NoError(t, k.MaybeRunCommunityPoolAutomation(ctx))
	require.Empty(t, mockEVM.methods)
}

func TestMaybeRunCommunityPoolAutomation_CallsHarvestThenStake(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
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
	ctx, k, _ := newTestKeeper(t)
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
