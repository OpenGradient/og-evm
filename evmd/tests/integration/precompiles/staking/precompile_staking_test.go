package staking

import (
	"testing"

	"github.com/stretchr/testify/suite"

	evm "github.com/cosmos/evm"
	"github.com/cosmos/evm/evmd/tests/integration"
	"github.com/cosmos/evm/tests/integration/precompiles/staking"
	testapp "github.com/cosmos/evm/testutil/app"
)

// The PoA staking guard is off in these suites because their genesis configures no
// community-pool vault (EVM scheduler target), so the staking precompile behaves as upstream.
// The guard's own enforcement (delegation limited to the vault, validator creation and unjail
// blocked) is covered by the x/vm StakedBondVault suite,
// TestStakingGuardBlocksNonVaultDelegationViaPrecompile, and the stakingguard unit tests.
func TestStakingPrecompileTestSuite(t *testing.T) {
	create := testapp.ToEvmAppCreator[evm.StakingPrecompileApp](integration.CreateEvmd, "evm.StakingPrecompileApp")
	s := staking.NewPrecompileTestSuite(create)
	suite.Run(t, s)
}

func TestStakingPrecompileIntegrationTestSuite(t *testing.T) {
	create := testapp.ToEvmAppCreator[evm.StakingPrecompileApp](integration.CreateEvmd, "evm.StakingPrecompileApp")
	staking.TestPrecompileIntegrationTestSuite(t, create)
}
