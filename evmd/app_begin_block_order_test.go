package evmd

import (
	"os"
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

// TestBeginBlockOrder_PoolRebalancerAfterSlashingAndEvidence guards the ordering required for
// PrepareMaturedPoolUndelegationCredits: slashing and evidence BeginBlock may update bonded or unbonding
// balances; poolrebalancer must snapshot UBD after both. Staking runs after poolrebalancer here;
// x/staking BeginBlocker only tracks HistoricalInfo (delegator UBD matures in staking EndBlock), so this
// relative order does not affect the snapshot’s UBD balances.
func TestBeginBlockOrder_PoolRebalancerAfterSlashingAndEvidence(t *testing.T) {
	home, err := os.MkdirTemp("", "evmd-begin-block-order")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })

	app := NewExampleApp(
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

	order := app.ModuleManager.OrderBeginBlockers
	require.NotEmpty(t, order)

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
		"equivocation evidence slashes UBD in evidence BeginBlock; poolrebalancer snapshot must follow")
	require.Less(t, iPool, iStake,
		"app orders poolrebalancer before staking BeginBlock; staking BeginBlock does not mutate UBD")
}
