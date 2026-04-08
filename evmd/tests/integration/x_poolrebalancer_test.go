// Pool rebalancer integration tests must be built with -tags=test (singular) so x/evm’s test-only
// EVMConfigurator.ResetTestConfig is included (see x/vm/types/config.go).
//
// Example: go test -tags=test ./evmd/tests/integration -run TestPoolRebalancerKeeperIntegrationTestSuite -count=1
package integration

import (
	"testing"

	"github.com/stretchr/testify/suite"

	evm "github.com/cosmos/evm"
	testapp "github.com/cosmos/evm/testutil/app"

	poolrebalancer "github.com/cosmos/evm/tests/integration/x/poolrebalancer"
)

func TestPoolRebalancerKeeperIntegrationTestSuite(t *testing.T) {
	create := testapp.ToEvmAppCreator[evm.IntegrationNetworkApp](CreateEvmd, "evm.IntegrationNetworkApp")
	s := poolrebalancer.NewKeeperIntegrationTestSuite(create)
	suite.Run(t, s)
}
