package keeper

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestUpdateParams_RejectsWrongAuthority(t *testing.T) {
	ctx, k := newTestKeeper(t)

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

func TestUpdateParams_AcceptsAuthorityAndUpdatesParams(t *testing.T) {
	ctx, k := newTestKeeper(t)

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
