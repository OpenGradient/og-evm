package types_test

import (
	"testing"

	"github.com/cosmos/evm/x/svip/types"
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

type MsgsTestSuite struct {
	suite.Suite
}

func TestMsgsTestSuite(t *testing.T) {
	suite.Run(t, new(MsgsTestSuite))
}

func (suite *MsgsTestSuite) TestMsgUpdateParamsValidateBasic() {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	testCases := []struct {
		name    string
		msg     *types.MsgUpdateParams
		expPass bool
	}{
		{
			"fail - invalid authority",
			&types.MsgUpdateParams{Authority: "invalid", Params: types.DefaultParams()},
			false,
		},
		{
			"fail - valid authority but invalid params",
			&types.MsgUpdateParams{Authority: govAddr, Params: types.Params{Activated: true, HalfLifeSeconds: 0}},
			false,
		},
		{
			"pass - valid authority and params",
			&types.MsgUpdateParams{Authority: govAddr, Params: types.DefaultParams()},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgActivateValidateBasic() {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	testCases := []struct {
		name    string
		msg     *types.MsgActivate
		expPass bool
	}{
		{"fail - invalid authority", &types.MsgActivate{Authority: "invalid"}, false},
		{"pass - valid authority", &types.MsgActivate{Authority: govAddr}, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgReactivateValidateBasic() {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	testCases := []struct {
		name    string
		msg     *types.MsgReactivate
		expPass bool
	}{
		{"fail - invalid authority", &types.MsgReactivate{Authority: "invalid"}, false},
		{"pass - valid authority", &types.MsgReactivate{Authority: govAddr}, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgPauseValidateBasic() {
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	testCases := []struct {
		name    string
		msg     *types.MsgPause
		expPass bool
	}{
		{"fail - invalid authority", &types.MsgPause{Authority: "invalid"}, false},
		{"pass - valid authority", &types.MsgPause{Authority: govAddr, Paused: true}, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgFundPoolValidateBasic() {
	validAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	testCases := []struct {
		name    string
		msg     *types.MsgFundPool
		expPass bool
	}{
		{
			"fail - invalid depositor",
			&types.MsgFundPool{
				Depositor: "invalid",
				Amount:    sdk.NewCoins(sdk.NewInt64Coin("aevmos", 1000)),
			},
			false,
		},
		{
			"fail - zero coins",
			&types.MsgFundPool{
				Depositor: validAddr,
				Amount:    sdk.Coins{},
			},
			false,
		},
		{
			"fail - invalid coins (negative)",
			&types.MsgFundPool{
				Depositor: validAddr,
				Amount:    sdk.Coins{sdk.Coin{Denom: "aevmos", Amount: sdkmath.NewInt(-1)}},
			},
			false,
		},
		{
			"pass - valid msg",
			&types.MsgFundPool{
				Depositor: validAddr,
				Amount:    sdk.NewCoins(sdk.NewInt64Coin("aevmos", 1000)),
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.msg.ValidateBasic()
			if tc.expPass {
				suite.NoError(err)
			} else {
				suite.Error(err)
			}
		})
	}
}
