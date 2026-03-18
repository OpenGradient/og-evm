package types_test

import (
	"testing"

	"github.com/cosmos/evm/x/svip/types"
	"github.com/stretchr/testify/suite"
)

type ParamsTestSuite struct {
	suite.Suite
}

func TestParamsTestSuite(t *testing.T) {
	suite.Run(t, new(ParamsTestSuite))
}

func (suite *ParamsTestSuite) TestParamsValidate() {
	testCases := []struct {
		name     string
		params   types.Params
		expError bool
	}{
		{
			"default params valid",
			types.DefaultParams(),
			false,
		},
		{
			"negative half_life",
			types.Params{HalfLifeSeconds: -1},
			true,
		},
		{
			"activated but half_life=0",
			types.Params{Activated: true, HalfLifeSeconds: 0},
			true,
		},
		{
			"not activated, half_life=0",
			types.Params{Activated: false, HalfLifeSeconds: 0},
			false,
		},
		{
			"valid activated params",
			types.Params{Activated: true, HalfLifeSeconds: 31536000},
			false,
		},
		{
			"valid activated with paused",
			types.Params{Activated: true, Paused: true, HalfLifeSeconds: 31536000},
			false,
		},
	}

	for _, tc := range testCases {
		err := tc.params.Validate()
		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}
