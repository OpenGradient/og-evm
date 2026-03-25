package bridge_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	evmencoding "github.com/cosmos/evm/encoding"
	bridgeprecompile "github.com/cosmos/evm/precompiles/bridge"
	cmnmocks "github.com/cosmos/evm/precompiles/common/mocks"
	testconstants "github.com/cosmos/evm/testutil/constants"
	bridgekeeper "github.com/cosmos/evm/x/bridge/keeper"
	bridgetypes "github.com/cosmos/evm/x/bridge/types"
	bridgemocks "github.com/cosmos/evm/x/bridge/types/mocks"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type testData struct {
	ctx        sdk.Context
	keeper     bridgekeeper.Keeper
	precompile *bridgeprecompile.Precompile
	bridgeBank *bridgemocks.BankKeeper
}

func newTestData(t *testing.T) testData {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(bridgetypes.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey) //nolint:staticcheck

	bridgeBank := bridgemocks.NewBankKeeper(t)
	accountKeeper := bridgemocks.NewAccountKeeper(t)
	balanceBank := cmnmocks.NewBankKeeper(t)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	cfg := evmencoding.MakeConfig(testconstants.ExampleChainID.EVMChainID)
	keeper := bridgekeeper.NewKeeper(cfg.Codec, storeKey, authority, bridgeBank, accountKeeper)
	vmtypes.SetDefaultEvmCoinInfo(testconstants.ExampleChainCoinInfo[testconstants.ExampleChainID])

	return testData{
		ctx:        ctx,
		keeper:     keeper,
		precompile: bridgeprecompile.NewPrecompile(keeper, balanceBank),
		bridgeBank: bridgeBank,
	}
}

func enabledParams(authorizedContract string) bridgetypes.Params {
	params := bridgetypes.DefaultParams()
	params.Enabled = true
	params.AuthorizedContract = authorizedContract
	return params
}

func newContract(caller common.Address, precompile *bridgeprecompile.Precompile) *vm.Contract {
	return vm.NewContract(caller, precompile.Address(), uint256.NewInt(0), 1_000_000, nil)
}

func decodeBoolResult(t *testing.T, methodName string, bz []byte) bool {
	t.Helper()

	method := bridgeprecompile.ABI.Methods[methodName]
	outputs, err := method.Outputs.Unpack(bz)
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	success, ok := outputs[0].(bool)
	require.True(t, ok)
	return success
}

func TestMintNative_AuthorizedCaller(t *testing.T) {
	td := newTestData(t)
	authorizedContract := testconstants.ExampleEvmAddressAlice
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams(authorizedContract)))

	recipient := common.HexToAddress(testconstants.ExampleEvmAddressBob)
	amount := sdkmath.NewInt(100)
	coins := sdk.NewCoins(sdk.NewCoin(vmtypes.GetEVMCoinDenom(), amount))
	recipientAddr := sdk.AccAddress(recipient.Bytes())

	td.bridgeBank.On("MintCoins", td.ctx, bridgetypes.ModuleName, coins).Return(nil)
	td.bridgeBank.On("SendCoinsFromModuleToAccount", td.ctx, bridgetypes.ModuleName, recipientAddr, coins).Return(nil)

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.MintNativeMethod]
	result, err := td.precompile.MintNative(
		td.ctx,
		newContract(common.HexToAddress(authorizedContract), td.precompile),
		&method,
		[]interface{}{recipient, big.NewInt(100)},
	)
	require.NoError(t, err)
	require.True(t, decodeBoolResult(t, bridgeprecompile.MintNativeMethod, result))
	require.Equal(t, amount, td.keeper.GetTotalMinted(td.ctx))
}

func TestMintNative_UnauthorizedCaller(t *testing.T) {
	td := newTestData(t)
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams(testconstants.ExampleEvmAddressAlice)))

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.MintNativeMethod]
	_, err := td.precompile.MintNative(
		td.ctx,
		newContract(common.HexToAddress(testconstants.ExampleEvmAddressBob), td.precompile),
		&method,
		[]interface{}{common.HexToAddress("0x3333333333333333333333333333333333333333"), big.NewInt(1)},
	)
	require.ErrorIs(t, err, bridgetypes.ErrUnauthorizedCaller)
}

func TestBurnNative_AuthorizedCaller(t *testing.T) {
	td := newTestData(t)
	authorizedContract := testconstants.ExampleEvmAddressAlice
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams(authorizedContract)))

	sender := sdk.AccAddress(common.HexToAddress(authorizedContract).Bytes())
	amount := sdkmath.NewInt(250)
	coins := sdk.NewCoins(sdk.NewCoin(vmtypes.GetEVMCoinDenom(), amount))

	td.bridgeBank.On("SendCoinsFromAccountToModule", td.ctx, sender, bridgetypes.ModuleName, coins).Return(nil)
	td.bridgeBank.On("BurnCoins", td.ctx, bridgetypes.ModuleName, coins).Return(nil)

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.BurnNativeMethod]
	result, err := td.precompile.BurnNative(
		td.ctx,
		newContract(common.HexToAddress(authorizedContract), td.precompile),
		&method,
		[]interface{}{big.NewInt(250)},
	)
	require.NoError(t, err)
	require.True(t, decodeBoolResult(t, bridgeprecompile.BurnNativeMethod, result))
	require.Equal(t, amount, td.keeper.GetTotalBurned(td.ctx))
}

func TestMintNative_DisabledBridge(t *testing.T) {
	td := newTestData(t)
	params := bridgetypes.DefaultParams()
	params.AuthorizedContract = testconstants.ExampleEvmAddressAlice
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.MintNativeMethod]
	_, err := td.precompile.MintNative(
		td.ctx,
		newContract(common.HexToAddress(params.AuthorizedContract), td.precompile),
		&method,
		[]interface{}{common.HexToAddress(testconstants.ExampleEvmAddressBob), big.NewInt(1)},
	)
	require.ErrorIs(t, err, bridgetypes.ErrBridgeDisabled)
}

func TestMintNative_ExceedsMaxTransfer(t *testing.T) {
	td := newTestData(t)
	params := enabledParams(testconstants.ExampleEvmAddressAlice)
	params.MaxTransferAmount = sdkmath.NewInt(10)
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.MintNativeMethod]
	_, err := td.precompile.MintNative(
		td.ctx,
		newContract(common.HexToAddress(params.AuthorizedContract), td.precompile),
		&method,
		[]interface{}{common.HexToAddress(testconstants.ExampleEvmAddressBob), big.NewInt(11)},
	)
	require.ErrorIs(t, err, bridgetypes.ErrExceedsMaxTransfer)
}

func TestIsEnabled(t *testing.T) {
	td := newTestData(t)
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams(testconstants.ExampleEvmAddressAlice)))

	method := bridgeprecompile.ABI.Methods[bridgeprecompile.IsEnabledMethod]
	result, err := td.precompile.IsEnabled(td.ctx, &method)
	require.NoError(t, err)
	require.True(t, decodeBoolResult(t, bridgeprecompile.IsEnabledMethod, result))
}
