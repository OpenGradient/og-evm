package bridge

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/contracts"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func (s *IntegrationTestSuite) TestHandleMintsNativeTokens() {
	owner := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	expectedMailboxAddr, expectedRouterAddr := s.setupBridgeNetwork(true)
	mailboxAddr := s.deployMockMailbox(owner, 7)
	s.Require().Equal(expectedMailboxAddr, mailboxAddr)
	routerAddr := s.deployHypOGNative(owner, mailboxAddr)
	s.Require().Equal(expectedRouterAddr, routerAddr)

	remoteRouter := common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111")
	s.executeContract(
		owner,
		routerAddr,
		contracts.HypOGNativeContract.ABI,
		"enrollRemoteRouter",
		nil,
		remoteDomain,
		remoteRouter,
	)

	beforeBalance := s.network.App.GetBankKeeper().GetBalance(
		s.network.GetContext(),
		recipient.AccAddr,
		s.network.GetBaseDenom(),
	).Amount

	amount := big.NewInt(12345)
	body := packTransferBody(recipient.Addr, amount)
	s.executeContract(
		owner,
		mailboxAddr,
		contracts.MockMailboxContract.ABI,
		"deliver",
		nil,
		routerAddr,
		remoteDomain,
		remoteRouter,
		body,
	)

	afterBalance := s.network.App.GetBankKeeper().GetBalance(
		s.network.GetContext(),
		recipient.AccAddr,
		s.network.GetBaseDenom(),
	).Amount

	s.Require().Equal(0, afterBalance.Sub(beforeBalance).BigInt().Cmp(amount))
	s.Require().Equal(0, s.network.App.GetBridgeKeeper().GetTotalMinted(s.network.GetContext()).BigInt().Cmp(amount))
}

func (s *IntegrationTestSuite) TestTransferRemoteBurnsAndDispatches() {
	owner := s.keyring.GetKey(0)
	expectedMailboxAddr, expectedRouterAddr := s.setupBridgeNetwork(true)
	mailboxAddr := s.deployMockMailbox(owner, 7)
	s.Require().Equal(expectedMailboxAddr, mailboxAddr)
	routerAddr := s.deployHypOGNative(owner, mailboxAddr)
	s.Require().Equal(expectedRouterAddr, routerAddr)

	remoteRouter := common.HexToHash("0x0000000000000000000000002222222222222222222222222222222222222222")
	remoteRecipient := common.HexToHash("0x0000000000000000000000003333333333333333333333333333333333333333")
	s.executeContract(
		owner,
		routerAddr,
		contracts.HypOGNativeContract.ABI,
		"enrollRemoteRouter",
		nil,
		remoteDomain,
		remoteRouter,
	)

	amount := big.NewInt(1000)
	dispatchFee := big.NewInt(7)
	msgValue := new(big.Int).Add(amount, dispatchFee)
	s.executeContract(
		owner,
		routerAddr,
		contracts.HypOGNativeContract.ABI,
		"transferRemote",
		msgValue,
		remoteDomain,
		remoteRecipient,
		amount,
	)

	s.Require().Equal(0, s.network.App.GetBridgeKeeper().GetTotalBurned(s.network.GetContext()).BigInt().Cmp(amount))

	mailboxBalance := s.network.App.GetBankKeeper().GetBalance(
		s.network.GetContext(),
		mailboxAddr.Bytes(),
		s.network.GetBaseDenom(),
	).Amount
	s.Require().Equal(0, mailboxBalance.BigInt().Cmp(dispatchFee))

	routerBalance := s.network.App.GetBankKeeper().GetBalance(
		s.network.GetContext(),
		routerAddr.Bytes(),
		s.network.GetBaseDenom(),
	).Amount
	s.Require().True(routerBalance.IsZero())

	lastDestination := s.queryContract(
		mailboxAddr,
		contracts.MockMailboxContract.ABI,
		"lastDestination",
	)[0].(uint32)
	s.Require().Equal(remoteDomain, lastDestination)

	lastRecipient := s.queryContract(
		mailboxAddr,
		contracts.MockMailboxContract.ABI,
		"lastRecipient",
	)[0].([32]byte)
	s.Require().Equal(remoteRouter, common.BytesToHash(lastRecipient[:]))

	lastBody := s.queryContract(
		mailboxAddr,
		contracts.MockMailboxContract.ABI,
		"lastBody",
	)[0].([]byte)
	bodyRecipient, bodyAmount := unpackTransferBody(lastBody)
	s.Require().Equal(remoteRecipient, bodyRecipient)
	s.Require().Equal(0, bodyAmount.Cmp(amount))
}

func (s *IntegrationTestSuite) TestTransferRemoteFailsWhenBridgeDisabled() {
	owner := s.keyring.GetKey(0)
	expectedMailboxAddr, expectedRouterAddr := s.setupBridgeNetwork(false)
	mailboxAddr := s.deployMockMailbox(owner, 7)
	s.Require().Equal(expectedMailboxAddr, mailboxAddr)
	routerAddr := s.deployHypOGNative(owner, mailboxAddr)
	s.Require().Equal(expectedRouterAddr, routerAddr)

	remoteRouter := common.HexToHash("0x0000000000000000000000004444444444444444444444444444444444444444")
	remoteRecipient := common.HexToHash("0x0000000000000000000000005555555555555555555555555555555555555555")
	s.executeContract(
		owner,
		routerAddr,
		contracts.HypOGNativeContract.ABI,
		"enrollRemoteRouter",
		nil,
		remoteDomain,
		remoteRouter,
	)

	_, err := s.factory.ExecuteContractCall(
		owner.Priv,
		evmtypes.EvmTxArgs{
			To:     &routerAddr,
			Amount: big.NewInt(1007),
		},
		testutiltypes.CallArgs{
			ContractABI: contracts.HypOGNativeContract.ABI,
			MethodName:  "transferRemote",
			Args:        []interface{}{remoteDomain, remoteRecipient, big.NewInt(1000)},
		},
	)
	s.Require().Error(err)
	s.Require().True(s.network.App.GetBridgeKeeper().GetTotalBurned(s.network.GetContext()).IsZero())
}
