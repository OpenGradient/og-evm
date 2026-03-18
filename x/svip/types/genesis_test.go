package types_test

import (
	"testing"

	"github.com/cosmos/evm/x/svip/types"
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"
)

type GenesisTestSuite struct {
	suite.Suite
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}

func (suite *GenesisTestSuite) TestDefaultGenesisState() {
	gs := types.DefaultGenesisState()
	suite.Require().NotNil(gs)
	suite.Require().NoError(gs.Validate())
}

func (suite *GenesisTestSuite) TestGenesisStateValidate() {
	testCases := []struct {
		name     string
		genesis  types.GenesisState
		expError bool
	}{
		{
			"valid - default genesis",
			*types.DefaultGenesisState(),
			false,
		},
		{
			"valid - positive total_distributed",
			types.GenesisState{
				Params:                  types.DefaultParams(),
				TotalDistributed:        sdkmath.NewInt(1000),
				PoolBalanceAtActivation: sdkmath.ZeroInt(),
			},
			false,
		},
		{
			"invalid - negative total_distributed",
			types.GenesisState{
				Params:                  types.DefaultParams(),
				TotalDistributed:        sdkmath.NewInt(-1),
				PoolBalanceAtActivation: sdkmath.ZeroInt(),
			},
			true,
		},
		{
			"invalid - activated with half_life=0",
			types.GenesisState{
				Params:                  types.Params{Activated: true, HalfLifeSeconds: 0},
				TotalDistributed:        sdkmath.ZeroInt(),
				PoolBalanceAtActivation: sdkmath.ZeroInt(),
			},
			true,
		},
	}

	for _, tc := range testCases {
		err := tc.genesis.Validate()
		if tc.expError {
			suite.Require().Error(err, tc.name)
		} else {
			suite.Require().NoError(err, tc.name)
		}
	}
}
