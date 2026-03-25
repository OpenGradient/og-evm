package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/bridge/keeper"
	"github.com/cosmos/evm/x/bridge/types"

	sdkmath "cosmossdk.io/math"
)

func TestUpdateParams_InvalidAuthority(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: "invalid",
		Params:    types.DefaultParams(),
	})
	require.ErrorContains(t, err, "invalid authority")
}

func TestUpdateParams_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	params := enabledParams()
	params.AuthorizedContract = "0x1111111111111111111111111111111111111111"
	params.HyperlaneMailbox = "0x2222222222222222222222222222222222222222"
	params.MaxTransferAmount = sdkmath.NewInt(999)

	_, err := srv.UpdateParams(td.ctx, &types.MsgUpdateParams{
		Authority: govAuthority(),
		Params:    params,
	})
	require.NoError(t, err)
	require.Equal(t, params, td.keeper.GetParams(td.ctx))
}

func TestSetAuthorizedContract_HappyPath(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	require.NoError(t, td.keeper.SetParams(td.ctx, enabledParams()))

	_, err := srv.SetAuthorizedContract(td.ctx, &types.MsgSetAuthorizedContract{
		Authority:       govAuthority(),
		ContractAddress: "0x3333333333333333333333333333333333333333",
	})
	require.NoError(t, err)
	require.Equal(t, "0x3333333333333333333333333333333333333333", td.keeper.GetParams(td.ctx).AuthorizedContract)
}

func TestSetAuthorizedContract_InvalidAuthority(t *testing.T) {
	td := newMockedTestData(t)
	srv := keeper.NewMsgServerImpl(td.keeper)

	_, err := srv.SetAuthorizedContract(td.ctx, &types.MsgSetAuthorizedContract{
		Authority:       "invalid",
		ContractAddress: "0x3333333333333333333333333333333333333333",
	})
	require.ErrorContains(t, err, "invalid authority")
}
