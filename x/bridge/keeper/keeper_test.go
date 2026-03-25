package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	evmencoding "github.com/cosmos/evm/encoding"
	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/x/bridge/keeper"
	"github.com/cosmos/evm/x/bridge/types"
	"github.com/cosmos/evm/x/bridge/types/mocks"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type testData struct {
	ctx    sdk.Context
	keeper keeper.Keeper
	bk     *mocks.BankKeeper
	ak     *mocks.AccountKeeper
}

func newMockedTestData(t *testing.T) testData {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey) //nolint:staticcheck

	bk := mocks.NewBankKeeper(t)
	ak := mocks.NewAccountKeeper(t)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)

	cfg := evmencoding.MakeConfig(testconstants.ExampleChainID.EVMChainID)
	k := keeper.NewKeeper(cfg.Codec, storeKey, authority, bk, ak)
	vmtypes.SetDefaultEvmCoinInfo(testconstants.ExampleChainCoinInfo[testconstants.ExampleChainID])

	return testData{
		ctx:    ctx,
		keeper: k,
		bk:     bk,
		ak:     ak,
	}
}

func govAuthority() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

func enabledParams() types.Params {
	params := types.DefaultParams()
	params.Enabled = true
	return params
}

func TestGetSetParamsAndTotals(t *testing.T) {
	td := newMockedTestData(t)

	require.Equal(t, types.DefaultParams(), td.keeper.GetParams(td.ctx))
	require.True(t, td.keeper.GetTotalMinted(td.ctx).IsZero())
	require.True(t, td.keeper.GetTotalBurned(td.ctx).IsZero())

	params := enabledParams()
	params.AuthorizedContract = testconstants.ExampleEvmAddressAlice
	params.HyperlaneMailbox = testconstants.ExampleEvmAddressBob
	params.MaxTransferAmount = sdkmath.NewInt(12345)
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	td.keeper.SetTotalMinted(td.ctx, sdkmath.NewInt(50))
	td.keeper.AddTotalMinted(td.ctx, sdkmath.NewInt(20))
	td.keeper.SetTotalBurned(td.ctx, sdkmath.NewInt(11))
	td.keeper.AddTotalBurned(td.ctx, sdkmath.NewInt(9))

	require.Equal(t, params, td.keeper.GetParams(td.ctx))
	require.Equal(t, sdkmath.NewInt(70), td.keeper.GetTotalMinted(td.ctx))
	require.Equal(t, sdkmath.NewInt(20), td.keeper.GetTotalBurned(td.ctx))
	require.Equal(t, params.AuthorizedContract, td.keeper.GetAuthorizedContract(td.ctx))
}

func TestMintForBridge(t *testing.T) {
	td := newMockedTestData(t)
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams()))

	recipient := sdk.AccAddress(common.HexToAddress(testconstants.ExampleEvmAddressAlice).Bytes())
	amount := sdkmath.NewInt(100)
	denom := vmtypes.GetEVMCoinDenom()
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))

	td.bk.On("MintCoins", td.ctx, types.ModuleName, coins).Return(nil)
	td.bk.On("SendCoinsFromModuleToAccount", td.ctx, types.ModuleName, recipient, coins).Return(nil)

	err := td.keeper.MintForBridge(td.ctx, recipient, amount)
	require.NoError(t, err)
	require.Equal(t, amount, td.keeper.GetTotalMinted(td.ctx))
}

func TestBurnForBridge(t *testing.T) {
	td := newMockedTestData(t)
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams()))

	sender := sdk.AccAddress(common.HexToAddress(testconstants.ExampleEvmAddressBob).Bytes())
	amount := sdkmath.NewInt(250)
	denom := vmtypes.GetEVMCoinDenom()
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))

	td.bk.On("SendCoinsFromAccountToModule", td.ctx, sender, types.ModuleName, coins).Return(nil)
	td.bk.On("BurnCoins", td.ctx, types.ModuleName, coins).Return(nil)

	err := td.keeper.BurnForBridge(td.ctx, sender, amount)
	require.NoError(t, err)
	require.Equal(t, amount, td.keeper.GetTotalBurned(td.ctx))
}

func TestMintForBridge_Disabled(t *testing.T) {
	td := newMockedTestData(t)

	recipient := sdk.AccAddress(common.HexToAddress(testconstants.ExampleEvmAddressAlice).Bytes())
	err := td.keeper.MintForBridge(td.ctx, recipient, sdkmath.NewInt(1))
	require.ErrorIs(t, err, types.ErrBridgeDisabled)

	td.bk.AssertNotCalled(t, "MintCoins")
	td.bk.AssertNotCalled(t, "SendCoinsFromModuleToAccount")
}

func TestBridgeTransfer_RejectsZeroAmount(t *testing.T) {
	td := newMockedTestData(t)
	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams()))

	recipient := sdk.AccAddress(common.HexToAddress(testconstants.ExampleEvmAddressAlice).Bytes())
	err := td.keeper.MintForBridge(td.ctx, recipient, sdkmath.ZeroInt())
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestBridgeTransfer_RejectsOverMaxAmount(t *testing.T) {
	td := newMockedTestData(t)
	params := enabledParams()
	params.MaxTransferAmount = sdkmath.NewInt(10)
	require.NoError(t, td.keeper.SetParams(td.ctx, params))

	recipient := sdk.AccAddress(common.HexToAddress(testconstants.ExampleEvmAddressAlice).Bytes())
	err := td.keeper.MintForBridge(td.ctx, recipient, sdkmath.NewInt(11))
	require.ErrorIs(t, err, types.ErrExceedsMaxTransfer)
}
