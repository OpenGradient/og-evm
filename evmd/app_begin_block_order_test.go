package evmd

import (
	"os"
	"sync"
	"testing"

	"cosmossdk.io/log"
	evidencetypes "cosmossdk.io/x/evidence/types"
	dbm "github.com/cosmos/cosmos-db"

	srvflags "github.com/cosmos/evm/server/flags"
	"github.com/cosmos/evm/testutil/constants"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simutils "github.com/cosmos/cosmos-sdk/testutil/sims"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func beginBlockModuleIndex(order []string, moduleName string) int {
	for i, name := range order {
		if name == moduleName {
			return i
		}
	}
	return -1
}

var (
	orderTestAppOnce sync.Once
	orderTestApp     *EVMD
	orderTestAppErr  error
)

func getOrderTestApp() (*EVMD, error) {
	orderTestAppOnce.Do(func() {
		home, err := os.MkdirTemp("", "evmd-block-order")
		if err != nil {
			orderTestAppErr = err
			return
		}

		orderTestApp = NewExampleApp(
			log.NewNopLogger(),
			dbm.NewMemDB(),
			nil,
			true,
			simutils.AppOptionsMap{
				flags.FlagHome:      home,
				srvflags.EVMChainID: constants.EighteenDecimalsChainID,
			},
			baseapp.SetChainID(constants.ExampleChainID.ChainID),
		)
	})
	return orderTestApp, orderTestAppErr
}

// TestBeginBlockOrder_PoolRebalancerAfterSlashingAndEvidence guards the ordering required for
// previous-block slash snapshot correctness: slashing and evidence BeginBlock may update staking
// state, so poolrebalancer must snapshot slash signals after both. Staking runs after poolrebalancer
// here; x/staking BeginBlocker only tracks HistoricalInfo, so this relative order does not affect
// slash-snapshot correctness.
func TestBeginBlockOrder_PoolRebalancerAfterSlashingAndEvidence(t *testing.T) {
	app, err := getOrderTestApp()
	require.NoError(t, err)

	order := app.ModuleManager.OrderBeginBlockers
	require.NotEmpty(t, order)
	require.NotNil(t, app.GetTKey(paramstypes.TStoreKey), "params transient key must be mounted")
	require.NotNil(
		t,
		app.GetTKey(poolrebalancertypes.TransientStoreKey),
		"poolrebalancer transient key must be mounted",
	)

	iSlash := beginBlockModuleIndex(order, slashingtypes.ModuleName)
	iEvidence := beginBlockModuleIndex(order, evidencetypes.ModuleName)
	iPool := beginBlockModuleIndex(order, poolrebalancertypes.ModuleName)
	iStake := beginBlockModuleIndex(order, stakingtypes.ModuleName)

	require.NotEqual(t, -1, iSlash, "slashing must be in OrderBeginBlockers")
	require.NotEqual(t, -1, iEvidence, "evidence must be in OrderBeginBlockers")
	require.NotEqual(t, -1, iPool, "poolrebalancer must be in OrderBeginBlockers")
	require.NotEqual(t, -1, iStake, "staking must be in OrderBeginBlockers")

	require.Less(t, iSlash, iEvidence,
		"slashing must run before evidence (downtime vs equivocation slash ordering)")
	require.Less(t, iEvidence, iPool,
		"equivocation evidence updates validator slash state in BeginBlock; poolrebalancer snapshot must follow")
	require.Less(t, iPool, iStake,
		"app orders poolrebalancer before staking BeginBlock; staking BeginBlock does not alter slash snapshot inputs")
}
