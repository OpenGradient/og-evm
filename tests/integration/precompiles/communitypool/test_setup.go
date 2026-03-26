package communitypool

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"
	. "github.com/onsi/gomega"

	compiledcontracts "github.com/cosmos/evm/contracts"
	"github.com/cosmos/evm/precompiles/erc20"
	"github.com/cosmos/evm/testutil/integration/evm/factory"
	"github.com/cosmos/evm/testutil/integration/evm/grpc"
	"github.com/cosmos/evm/testutil/integration/evm/network"
	"github.com/cosmos/evm/testutil/integration/evm/utils"
	testkeyring "github.com/cosmos/evm/testutil/keyring"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// IntegrationTestSuite contains shared setup/state for CommunityPool integration tests.
type IntegrationTestSuite struct {
	suite.Suite

	create  network.CreateEvmApp
	options []network.ConfigOption

	network     *network.UnitTestNetwork
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     testkeyring.Keyring

	bondDenom     string
	bondTokenAddr common.Address
	bondTokenPC   *erc20.Precompile
	validatorPrefix string

	communityPoolContract evmtypes.CompiledContract
}

func NewIntegrationTestSuite(create network.CreateEvmApp, options ...network.ConfigOption) *IntegrationTestSuite {
	return &IntegrationTestSuite{
		create:  create,
		options: options,
	}
}

func (s *IntegrationTestSuite) SetupTest() {
	keys := testkeyring.New(3)
	genesis := utils.CreateGenesisWithTokenPairs(keys)

	opts := []network.ConfigOption{
		network.WithPreFundedAccounts(keys.GetAllAccAddrs()...),
		network.WithCustomGenesis(genesis),
	}
	opts = append(opts, s.options...)

	nw := network.NewUnitTestNetwork(s.create, opts...)
	gh := grpc.NewIntegrationHandler(nw)
	tf := factory.New(nw, gh)

	ctx := nw.GetContext()
	sk := nw.App.GetStakingKeeper()
	bondDenom, err := sk.BondDenom(ctx)
	Expect(err).To(BeNil(), "failed to get bond denom")
	Expect(bondDenom).ToNot(BeEmpty(), "bond denom cannot be empty")

	tokenPairID := nw.App.GetErc20Keeper().GetTokenPairID(ctx, bondDenom)
	tokenPair, found := nw.App.GetErc20Keeper().GetTokenPair(ctx, tokenPairID)
	Expect(found).To(BeTrue(), "failed to find token pair for bond denom")
	bondTokenPC := erc20.NewPrecompile(
		tokenPair,
		nw.App.GetBankKeeper(),
		nw.App.GetErc20Keeper(),
		nw.App.GetTransferKeeper(),
	)

	poolContract, err := compiledcontracts.LoadCommunityPool()
	Expect(err).To(BeNil(), "failed to load CommunityPool compiled contract")

	s.network = nw
	s.factory = tf
	s.grpcHandler = gh
	s.keyring = keys
	s.bondDenom = bondDenom
	s.bondTokenAddr = tokenPair.GetERC20Contract()
	s.bondTokenPC = bondTokenPC
	firstVal := nw.GetValidators()[0].OperatorAddress
	s.validatorPrefix = strings.Split(firstVal, "1")[0]
	s.communityPoolContract = poolContract
}

