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
			"half_life=0 valid",
			types.Params{HalfLifeSeconds: 0},
			false,
		},
		{
			"valid half_life",
			types.Params{HalfLifeSeconds: 31536000},
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
