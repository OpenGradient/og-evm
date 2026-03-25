package bridge

import (
	"testing"

	"github.com/stretchr/testify/suite"

	evm "github.com/cosmos/evm"
	"github.com/cosmos/evm/evmd/tests/integration"
	"github.com/cosmos/evm/tests/integration/bridge"
	testapp "github.com/cosmos/evm/testutil/app"
)

func TestBridgeIntegrationSuite(t *testing.T) {
	create := testapp.ToEvmAppCreator[evm.IntegrationNetworkApp](integration.CreateEvmd, "evm.IntegrationNetworkApp")
	s := bridge.NewIntegrationTestSuite(create)
	suite.Run(t, s)
}
