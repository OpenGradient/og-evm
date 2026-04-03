package keeper

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestUpdateParams_RejectsWrongAuthority(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	// Current keeper authority is 0x09..09; use a different address.
	wrongAuthority := sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String()

	msg := &types.MsgUpdateParams{
		Authority: wrongAuthority,
		Params:    types.DefaultParams(),
	}

	_, err := k.UpdateParams(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority")
}

func TestUpdateParams_RejectsNilRequest(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	_, err := k.UpdateParams(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty update params request")
}

func TestUpdateParams_AcceptsAuthorityAndUpdatesParams(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	authority := k.authority.String()

	newParams := types.DefaultParams()
	newParams.MaxOpsPerBlock = 9
	newParams.MaxMovePerOp = math.NewInt(77)

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    newParams,
	}

	_, err := k.UpdateParams(ctx, msg)
	require.NoError(t, err)

	got, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(9), got.MaxOpsPerBlock)
	require.True(t, got.MaxMovePerOp.Equal(math.NewInt(77)))
}

func TestUpdateParams_RejectsInvalidParamsWithValidAuthority(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	authority := k.authority.String()
	invalid := types.DefaultParams()
	invalid.MaxTargetValidators = 0

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    invalid,
	}

	_, err := k.UpdateParams(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_target_validators must be positive")
}

// UpdateParams calls SetParams, so pool_delegator validation applies on the gov path too.
func TestUpdateParams_RejectsNonEmptyPoolWhenEVMKeeperNil(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()

	_, err := k.UpdateParams(ctx, &types.MsgUpdateParams{
		Authority: k.authority.String(),
		Params:    params,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires evm keeper")
}

func TestUpdateParams_RejectsUserAccountPoolDelegator(t *testing.T) {
	ctx, k, mockAcc := newTestKeeper(t)
	k.evmKeeper = &mockEVMKeeper{}

	priv := secp256k1.GenPrivKey()
	pub := priv.PubKey()
	addr := sdk.AccAddress(pub.Address())
	mockAcc.SetAccount(ctx, authtypes.NewBaseAccount(addr, pub, 0, 0))

	params := types.DefaultParams()
	params.PoolDelegatorAddress = addr.String()

	_, err := k.UpdateParams(ctx, &types.MsgUpdateParams{
		Authority: k.authority.String(),
		Params:    params,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "user account with signing keys")
}

func TestMsgUpdateParams_ValidateBasic_RejectsInvalidParams(t *testing.T) {
	msg := &types.MsgUpdateParams{
		Authority: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		Params: types.Params{
			PoolDelegatorAddress:  "",
			MaxTargetValidators:   0, // invalid
			RebalanceThresholdBp:  50,
			MaxOpsPerBlock:        5,
			MaxMovePerOp:          math.ZeroInt(),
			UseUndelegateFallback: true,
		},
	}

	require.Error(t, msg.ValidateBasic())
}

func TestSetParams_RejectsInvalidParamsDirectly(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)

	invalid := types.DefaultParams()
	invalid.MaxTargetValidators = 0

	err := k.SetParams(ctx, invalid)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_target_validators must be positive")
}

func TestSetParams_RejectsNonEmptyPoolWhenEVMKeeperNil(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()

	err := k.SetParams(ctx, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires evm keeper")
}

func TestSetParams_RejectsNonEmptyPoolWhenAuthAndEVMUnset(t *testing.T) {
	ctx, k := newTestKeeperNilAuthAndEVM(t)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()

	err := k.SetParams(ctx, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires account keeper or evm keeper for validation")
}

// User account with pubkey (signing keys); same intent as plan's RejectsUserAccountWithPubkey.
func TestSetParams_RejectsUserAccountPoolDelegator(t *testing.T) {
	ctx, k, mockAcc := newTestKeeper(t)
	k.evmKeeper = &mockEVMKeeper{}

	priv := secp256k1.GenPrivKey()
	pub := priv.PubKey()
	addr := sdk.AccAddress(pub.Address())
	acc := authtypes.NewBaseAccount(addr, pub, 0, 0)
	mockAcc.SetAccount(ctx, acc)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = addr.String()

	err := k.SetParams(ctx, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "user account with signing keys")
}

func TestSetParams_AcceptsBootstrapNoAuthAccount(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	k.evmKeeper = &mockEVMKeeper{
		isContractFn: func(common.Address) bool { return false },
	}
	addr := sdk.AccAddress(bytes.Repeat([]byte{0xAB}, 20))
	params := types.DefaultParams()
	params.PoolDelegatorAddress = addr.String()

	require.NoError(t, k.SetParams(ctx, params))
}

func TestSetParams_RejectsNonContractWhenAccountExistsWithoutBootstrap(t *testing.T) {
	ctx, k, mockAcc := newTestKeeper(t)
	k.evmKeeper = &mockEVMKeeper{
		isContractFn: func(common.Address) bool { return false },
	}
	addr := sdk.AccAddress(bytes.Repeat([]byte{0xAB}, 20))
	mockAcc.SetAccount(ctx, authtypes.NewBaseAccountWithAddress(addr))

	params := types.DefaultParams()
	params.PoolDelegatorAddress = addr.String()

	err := k.SetParams(ctx, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an EVM contract")
}
