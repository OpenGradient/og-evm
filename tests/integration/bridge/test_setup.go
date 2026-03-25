package bridge

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/contracts"
	testfactory "github.com/cosmos/evm/testutil/integration/evm/factory"
	testgrpc "github.com/cosmos/evm/testutil/integration/evm/grpc"
	testnetwork "github.com/cosmos/evm/testutil/integration/evm/network"
	testkeyring "github.com/cosmos/evm/testutil/keyring"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	bridgetypes "github.com/cosmos/evm/x/bridge/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

const (
	localDomain  uint32 = 10740
	remoteDomain uint32 = 8453
)

type IntegrationTestSuite struct {
	suite.Suite

	create  testnetwork.CreateEvmApp
	options []testnetwork.ConfigOption

	network *testnetwork.UnitTestNetwork
	factory testfactory.TxFactory
	keyring testkeyring.Keyring
}

func NewIntegrationTestSuite(create testnetwork.CreateEvmApp, options ...testnetwork.ConfigOption) *IntegrationTestSuite {
	return &IntegrationTestSuite{
		create:  create,
		options: options,
	}
}

func (s *IntegrationTestSuite) SetupTest() {
	s.keyring = testkeyring.New(2)
	s.factory = nil
	s.network = nil
}

func (s *IntegrationTestSuite) deployMockMailbox(sender testkeyring.Key, quoteFee int64) common.Address {
	addr, err := s.factory.DeployContract(
		sender.Priv,
		evmtypes.EvmTxArgs{},
		testutiltypes.ContractDeploymentData{
			Contract:        contracts.MockMailboxContract,
			ConstructorArgs: []interface{}{localDomain, big.NewInt(quoteFee)},
		},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.network.NextBlock())

	return addr
}

func (s *IntegrationTestSuite) deployHypOGNative(sender testkeyring.Key, mailbox common.Address) common.Address {
	zeroAddress := common.Address{}
	addr, err := s.factory.DeployContract(
		sender.Priv,
		evmtypes.EvmTxArgs{},
		testutiltypes.ContractDeploymentData{
			Contract: contracts.HypOGNativeContract,
			ConstructorArgs: []interface{}{
				mailbox,
				zeroAddress,
				sender.Addr,
			},
		},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.network.NextBlock())

	return addr
}

func predictBridgeDeployments(owner common.Address) (common.Address, common.Address) {
	mailboxAddr := crypto.CreateAddress(owner, 0)
	routerAddr := crypto.CreateAddress(owner, 1)
	return mailboxAddr, routerAddr
}

func (s *IntegrationTestSuite) setupBridgeNetwork(enabled bool) (common.Address, common.Address) {
	owner := s.keyring.GetKey(0)
	mailboxAddr, routerAddr := predictBridgeDeployments(owner.Addr)

	bridgeGenesis := bridgetypes.DefaultGenesisState()
	bridgeGenesis.Params.Enabled = enabled
	bridgeGenesis.Params.HyperlaneMailbox = mailboxAddr.Hex()
	bridgeGenesis.Params.AuthorizedContract = routerAddr.Hex()

	options := []testnetwork.ConfigOption{
		testnetwork.WithPreFundedAccounts(s.keyring.GetAllAccAddrs()...),
		testnetwork.WithCustomGenesis(testnetwork.CustomGenesisState{
			bridgetypes.ModuleName: bridgeGenesis,
		}),
	}
	options = append(options, s.options...)

	nw := testnetwork.NewUnitTestNetwork(s.create, options...)
	handler := testgrpc.NewIntegrationHandler(nw)
	s.factory = testfactory.New(nw, handler)
	s.network = nw
	s.Require().Equal(bridgeGenesis.Params, nw.App.GetBridgeKeeper().GetParams(nw.GetContext()))

	return mailboxAddr, routerAddr
}

func (s *IntegrationTestSuite) executeContract(
	sender testkeyring.Key,
	contract common.Address,
	contractABI abi.ABI,
	method string,
	value *big.Int,
	args ...interface{},
) {
	_, err := s.factory.ExecuteContractCall(
		sender.Priv,
		evmtypes.EvmTxArgs{
			To:       &contract,
			Amount:   value,
			GasLimit: 5_000_000,
		},
		testutiltypes.CallArgs{
			ContractABI: contractABI,
			MethodName:  method,
			Args:        args,
		},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.network.NextBlock())
}

func (s *IntegrationTestSuite) queryContract(
	contract common.Address,
	contractABI abi.ABI,
	method string,
	args ...interface{},
) []interface{} {
	res, err := s.factory.QueryContract(
		evmtypes.EvmTxArgs{
			To: &contract,
		},
		testutiltypes.CallArgs{
			ContractABI: contractABI,
			MethodName:  method,
			Args:        args,
		},
		0,
	)
	s.Require().NoError(err)

	methodDef := contractABI.Methods[method]
	values, err := methodDef.Outputs.Unpack(res.Ret)
	s.Require().NoError(err)
	return values
}

func packTransferBody(recipient common.Address, amount *big.Int) []byte {
	body := common.LeftPadBytes(recipient.Bytes(), 32)
	return append(body, common.LeftPadBytes(amount.Bytes(), 32)...)
}

func unpackTransferBody(body []byte) (common.Hash, *big.Int) {
	if len(body) != 64 {
		panic("unexpected transfer body length")
	}

	return common.BytesToHash(body[:32]), new(big.Int).SetBytes(body[32:])
}
