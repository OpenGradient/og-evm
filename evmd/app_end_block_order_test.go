package evmd

import (
	"testing"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
	"github.com/stretchr/testify/require"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func endBlockModuleIndex(order []string, moduleName string) int {
	for i, name := range order {
		if name == moduleName {
			return i
		}
	}
	return -1
}

// TestEndBlockOrder_StakingBeforePoolRebalancer guards the ordering required for
// poolrebalancer EndBlock reconciliation from staking truth.
func TestEndBlockOrder_StakingBeforePoolRebalancer(t *testing.T) {
	app, err := getOrderTestApp()
	require.NoError(t, err)

	order := app.ModuleManager.OrderEndBlockers
	require.NotEmpty(t, order)

	iStake := endBlockModuleIndex(order, stakingtypes.ModuleName)
	iPool := endBlockModuleIndex(order, poolrebalancertypes.ModuleName)

	require.NotEqual(t, -1, iStake, "staking must be in OrderEndBlockers")
	require.NotEqual(t, -1, iPool, "poolrebalancer must be in OrderEndBlockers")
	require.Less(t, iStake, iPool,
		"staking EndBlock must run before poolrebalancer EndBlock for reconcile correctness")
}
