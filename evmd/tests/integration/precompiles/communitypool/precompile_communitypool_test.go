package communitypool

import (
	"testing"

	evm "github.com/cosmos/evm"
	"github.com/cosmos/evm/evmd/tests/integration"
	communitypooltests "github.com/cosmos/evm/tests/integration/precompiles/communitypool"
	testapp "github.com/cosmos/evm/testutil/app"
)

func TestCommunityPoolPrecompileIntegrationTestSuite(t *testing.T) {
	create := testapp.ToEvmAppCreator[evm.Erc20IntegrationApp](integration.CreateEvmd, "evm.Erc20IntegrationApp")
	communitypooltests.TestCommunityPoolIntegrationSuite(t, create)
}

