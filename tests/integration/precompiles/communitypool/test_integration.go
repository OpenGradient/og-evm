package communitypool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	//nolint:revive // dot imports are fine for Ginkgo
	. "github.com/onsi/ginkgo/v2"
	//nolint:revive // dot imports are fine for Ginkgo
	. "github.com/onsi/gomega"

	"github.com/cosmos/evm/precompiles/testutil"
	"github.com/cosmos/evm/testutil/integration/evm/network"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestCommunityPoolIntegrationSuite scaffolds the CommunityPool integration suite.
// Detailed behavior scenarios are implemented in subsequent test steps.
func TestCommunityPoolIntegrationSuite(t *testing.T, create network.CreateEvmApp, options ...network.ConfigOption) {
	_ = Describe("CommunityPool integration scaffold", func() {
		var s *IntegrationTestSuite

		BeforeEach(func() {
			s = NewIntegrationTestSuite(create, options...)
			s.SetupTest()
		})

		It("sets up suite dependencies for CommunityPool tests", func() {
			Expect(s.network).ToNot(BeNil())
			Expect(s.factory).ToNot(BeNil())
			Expect(s.grpcHandler).ToNot(BeNil())
			Expect(s.keyring).ToNot(BeNil())
			Expect(s.bondDenom).ToNot(BeEmpty())
			Expect(s.bondTokenAddr).ToNot(Equal([20]byte{}))
			Expect(s.communityPoolContract.Bin).ToNot(BeEmpty())
		})

		It("rejects constructor with invalid maxValidators", func() {
			owner := s.keyring.GetKey(0)
			_, err := s.factory.DeployContract(
				owner.Priv,
				evmtypes.EvmTxArgs{},
				testutiltypes.ContractDeploymentData{
					Contract: s.communityPoolContract,
					ConstructorArgs: []interface{}{
						s.bondTokenAddr,
						uint32(10),
						uint32(0), // invalid
						big.NewInt(1),
						owner.Addr,
					},
				},
			)
			Expect(err).To(HaveOccurred())
		})

		It("reverts on deposit(0)", func() {
			owner := s.keyring.GetKey(0)
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))

			s.execTxExpectCustomError(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", big.NewInt(0)),
				"InvalidAmount()",
			)
		})

		It("mints 1:1 units on first deposit", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			depositor := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				depositor.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			userUnits := s.queryPoolUint(1, poolAddr, "unitsOf", depositor.Addr)
			totalUnits := s.queryPoolUint(1, poolAddr, "totalUnits")
			Expect(userUnits.String()).To(Equal(depositAmount.String()))
			Expect(totalUnits.String()).To(Equal(depositAmount.String()))
		})

		It("mints proportional units on subsequent deposit", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user1 := s.keyring.GetKey(1)
			user2 := s.keyring.GetKey(2)

			firstDeposit := big.NewInt(1000)
			secondDeposit := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, firstDeposit)
			s.approveBondToken(2, poolAddr, secondDeposit)

			s.execTxExpectSuccess(
				user1.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", firstDeposit),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user2.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", secondDeposit),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			user2Units := s.queryPoolUint(2, poolAddr, "unitsOf", user2.Addr)
			totalUnits := s.queryPoolUint(0, poolAddr, "totalUnits")
			Expect(user2Units.String()).To(Equal("500"))
			Expect(totalUnits.String()).To(Equal("1500"))
		})

		It("creates async withdraw request and updates accounting", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(400)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Withdraw path is strict unbonding-based, so ensure principal is staked first.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			remainingUnits := s.queryPoolUint(1, poolAddr, "unitsOf", user.Addr)
			totalUnits := s.queryPoolUint(1, poolAddr, "totalUnits")
			Expect(remainingUnits.String()).To(Equal("600"))
			Expect(totalUnits.String()).To(Equal("600"))

			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			pendingReserve := s.queryPoolUint(1, poolAddr, "pendingWithdrawReserve")
			maturedReserve := s.queryPoolUint(1, poolAddr, "maturedWithdrawReserve")
			Expect(totalStaked.String()).To(Equal("600"))
			Expect(pendingReserve.String()).To(Equal("400"))
			Expect(maturedReserve.Sign()).To(Equal(0))

			request := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			Expect(request.Owner).To(Equal(user.Addr))
			Expect(request.AmountOut.String()).To(Equal("400"))
			Expect(request.ReserveMoved).To(BeFalse())
			Expect(request.Claimed).To(BeFalse())
			Expect(request.Maturity).To(BeNumerically(">", 0))
			s.assertPoolInvariants(poolAddr)
		})

		It("increments nextWithdrawRequestId across multiple requests", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(200)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Starts at 1 before any request.
			nextBefore := s.queryPoolUint(1, poolAddr, "nextWithdrawRequestId")
			Expect(nextBefore.String()).To(Equal("1"))

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			nextAfterFirst := s.queryPoolUint(1, poolAddr, "nextWithdrawRequestId")
			Expect(nextAfterFirst.String()).To(Equal("2"))

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			nextAfterSecond := s.queryPoolUint(1, poolAddr, "nextWithdrawRequestId")
			Expect(nextAfterSecond.String()).To(Equal("3"))

			req1 := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			req2 := s.queryWithdrawRequest(poolAddr, big.NewInt(2))
			Expect(req1.Owner).To(Equal(user.Addr))
			Expect(req2.Owner).To(Equal(user.Addr))
			Expect(req1.AmountOut.String()).To(Equal("200"))
			Expect(req2.AmountOut.String()).To(Equal("200"))
			Expect(req1.Claimed).To(BeFalse())
			Expect(req2.Claimed).To(BeFalse())
			s.assertPoolInvariants(poolAddr)
		})

		It("reverts withdraw when userUnits is zero", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", big.NewInt(0)),
				"InvalidUnits()",
			)
			s.assertPoolInvariants(poolAddr)
		})

		It("reverts withdraw when requested units exceed user balance", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", big.NewInt(1001)),
				"InvalidUnits()",
			)
			s.assertPoolInvariants(poolAddr)
		})

		It("reverts withdraw when no staked principal exists", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", big.NewInt(1000)),
				"InvalidAmount()",
			)
		})

		It("enforces maturity and ownership in claimWithdraw", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			other := s.keyring.GetKey(2)

			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(400)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Request owner only.
			s.execTxExpectCustomError(
				other.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
				"Unauthorized()",
			)

			// Request cannot be claimed before maturity.
			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
				"RequestNotMatured(uint64,uint64)",
			)
		})

		It("claims matured withdraw and prevents double claim", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(400)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			request := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			s.advanceToMaturity(request.Maturity)

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Reserves should be fully consumed for this single request.
			pendingReserve := s.queryPoolUint(1, poolAddr, "pendingWithdrawReserve")
			maturedReserve := s.queryPoolUint(1, poolAddr, "maturedWithdrawReserve")
			commitments := s.queryPoolUint(1, poolAddr, "totalWithdrawCommitments")
			Expect(pendingReserve.Sign()).To(Equal(0))
			Expect(maturedReserve.Sign()).To(Equal(0))
			Expect(commitments.Sign()).To(Equal(0))

			claimedReq := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			Expect(claimedReq.ReserveMoved).To(BeTrue())
			Expect(claimedReq.Claimed).To(BeTrue())

			// Second claim must revert.
			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
				"RequestAlreadyClaimed()",
			)
			s.assertPoolInvariants(poolAddr)
		})

		It("emits withdraw lifecycle events with expected request id", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(400)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			withdrawRes := s.execTxAndGetEthResponse(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			withdrawEvt := s.communityPoolContract.ABI.Events["WithdrawRequested"]
			withdrawLog := s.findEventLog(withdrawRes, poolAddr, withdrawEvt)
			Expect(withdrawLog).ToNot(BeNil(), "expected WithdrawRequested event")
			Expect(withdrawLog.Topics).To(HaveLen(3))
			Expect(withdrawLog.Topics[1]).To(Equal(common.BytesToHash(user.Addr.Bytes()).Hex()))
			Expect(withdrawLog.Topics[2]).To(Equal(common.BigToHash(big.NewInt(1)).Hex()))

			requestData, err := withdrawEvt.Inputs.Unpack(withdrawLog.Data)
			Expect(err).To(BeNil(), "failed to decode WithdrawRequested data")
			Expect(requestData).To(HaveLen(3))
			unitsVal, ok := requestData[0].(*big.Int)
			Expect(ok).To(BeTrue())
			amountOutVal, ok := requestData[1].(*big.Int)
			Expect(ok).To(BeTrue())
			Expect(unitsVal.String()).To(Equal(withdrawUnits.String()))
			Expect(amountOutVal.String()).To(Equal("400"))

			request := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			s.advanceToMaturity(request.Maturity)

			claimRes := s.execTxAndGetEthResponse(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			claimEvt := s.communityPoolContract.ABI.Events["WithdrawClaimed"]
			claimLog := s.findEventLog(claimRes, poolAddr, claimEvt)
			Expect(claimLog).ToNot(BeNil(), "expected WithdrawClaimed event")
			Expect(claimLog.Topics).To(HaveLen(3))
			Expect(claimLog.Topics[1]).To(Equal(common.BytesToHash(user.Addr.Bytes()).Hex()))
			Expect(claimLog.Topics[2]).To(Equal(common.BigToHash(big.NewInt(1)).Hex()))
		})

		It("claims multiple matured requests out of order", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			withdrawUnits := big.NewInt(200)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			req1 := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			req2 := s.queryWithdrawRequest(poolAddr, big.NewInt(2))
			if req2.Maturity > req1.Maturity {
				s.advanceToMaturity(req2.Maturity)
			} else {
				s.advanceToMaturity(req1.Maturity)
			}

			// Claim second request first, then first request.
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(2)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(1)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			claimedReq1 := s.queryWithdrawRequest(poolAddr, big.NewInt(1))
			claimedReq2 := s.queryWithdrawRequest(poolAddr, big.NewInt(2))
			Expect(claimedReq1.Claimed).To(BeTrue())
			Expect(claimedReq2.Claimed).To(BeTrue())
			Expect(claimedReq1.ReserveMoved).To(BeTrue())
			Expect(claimedReq2.ReserveMoved).To(BeTrue())

			pendingReserve := s.queryPoolUint(1, poolAddr, "pendingWithdrawReserve")
			maturedReserve := s.queryPoolUint(1, poolAddr, "maturedWithdrawReserve")
			Expect(pendingReserve.Sign()).To(Equal(0))
			Expect(maturedReserve.Sign()).To(Equal(0))
			s.assertPoolInvariants(poolAddr)
		})

		It("reverts claimWithdraw for non-existent request", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)

			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", big.NewInt(9999)),
				"InvalidRequest()",
			)
		})

		It("returns expected pricePerUnit for empty and adjusted pool", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			// Empty pool price is defined as 1e18.
			emptyPPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")
			Expect(emptyPPU.String()).To(Equal("1000000000000000000"))

			amount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, amount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", amount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// principalAssets=2000, totalUnits=1000 => pricePerUnit=2e18.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			updatedPPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")
			Expect(updatedPPU.String()).To(Equal("2000000000000000000"))
		})

		It("restricts owner-only methods to owner account", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			nonOwner := s.keyring.GetKey(1)

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(20), uint32(7), big.NewInt(2)),
				"Unauthorized()",
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(123)),
				"Unauthorized()",
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", nonOwner.Addr),
				"Unauthorized()",
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setAutomationCaller", nonOwner.Addr),
				"Unauthorized()",
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Owner can still execute privileged actions.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(20), uint32(7), big.NewInt(2)),
			)
		})

		It("restricts stake and harvest to owner or automation caller", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			nonOwner := s.keyring.GetKey(1)
			automation := s.keyring.GetKey(2)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
				"Unauthorized()",
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setAutomationCaller", automation.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				automation.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "harvest"),
				"Unauthorized()",
			)
		})

		It("reverts dust deposit that would mint zero units", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user1 := s.keyring.GetKey(1)
			user2 := s.keyring.GetKey(2)

			s.approveBondToken(1, poolAddr, big.NewInt(1000))
			s.approveBondToken(2, poolAddr, big.NewInt(1))

			s.execTxExpectSuccess(
				user1.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Inflate asset accounting so a 1-token deposit maps to 0 units:
			// minted = floor(1 * 1000 / 2000) = 0.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeUser2Units := s.queryPoolUint(2, poolAddr, "unitsOf", user2.Addr)
			beforeTotalUnits := s.queryPoolUint(2, poolAddr, "totalUnits")

			s.execTxExpectCustomError(
				user2.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", big.NewInt(1)),
				"ZeroMintedUnits()",
			)

			afterUser2Units := s.queryPoolUint(2, poolAddr, "unitsOf", user2.Addr)
			afterTotalUnits := s.queryPoolUint(2, poolAddr, "totalUnits")
			Expect(afterUser2Units.String()).To(Equal(beforeUser2Units.String()))
			Expect(afterTotalUnits.String()).To(Equal(beforeTotalUnits.String()))
		})

		It("transfers ownership and updates privileged access", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			oldOwner := s.keyring.GetKey(0)
			newOwner := s.keyring.GetKey(1)

			s.execTxExpectSuccess(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", newOwner.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Old owner should now be blocked.
			s.execTxExpectCustomError(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(99), uint32(9), big.NewInt(3)),
				"Unauthorized()",
			)

			// New owner should now be allowed.
			s.execTxExpectSuccess(
				newOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(99), uint32(9), big.NewInt(3)),
			)
		})

		It("rejects transferOwnership to zero address", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)

			zeroAddr := common.Address{}
			s.execTxExpectCustomError(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", zeroAddr),
				"InvalidAddress()",
			)
		})

		It("allows owner to call all privileged methods", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			automation := s.keyring.GetKey(1)

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(11), uint32(6), big.NewInt(2)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(321)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setAutomationCaller", automation.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			maxValidators := s.queryPoolUint(0, poolAddr, "maxValidators")
			totalStaked := s.queryPoolUint(0, poolAddr, "totalStaked")
			automationCaller := s.queryPoolAddress(poolAddr, "automationCaller")
			Expect(maxValidators.String()).To(Equal("6"))
			Expect(totalStaked.String()).To(Equal("321"))
			Expect(automationCaller.Hex()).To(Equal(automation.Addr.Hex()))
		})

		It("reverts setConfig when maxValidators is zero", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)

			s.execTxExpectCustomError(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(10), uint32(0), big.NewInt(1)),
				"InvalidConfig()",
			)
		})

		It("emits ConfigUpdated with applied values", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)

			res := s.execTxAndGetEthResponse(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(20), uint32(7), big.NewInt(2)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			cfgEvt := s.communityPoolContract.ABI.Events["ConfigUpdated"]
			cfgLog := s.findEventLog(res, poolAddr, cfgEvt)
			Expect(cfgLog).ToNot(BeNil(), "expected ConfigUpdated event")

			cfgData, err := cfgEvt.Inputs.Unpack(cfgLog.Data)
			Expect(err).To(BeNil(), "failed to decode ConfigUpdated data")
			Expect(cfgData).To(HaveLen(3))
			maxRetrieve, ok := cfgData[0].(uint32)
			Expect(ok).To(BeTrue())
			maxValidators, ok := cfgData[1].(uint32)
			Expect(ok).To(BeTrue())
			minStakeAmount, ok := cfgData[2].(*big.Int)
			Expect(ok).To(BeTrue())

			Expect(maxRetrieve).To(Equal(uint32(20)))
			Expect(maxValidators).To(Equal(uint32(7)))
			Expect(minStakeAmount.String()).To(Equal("2"))
		})

		It("setConfig minStakeAmount change gates stake behavior", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Raise threshold above available liquid; stake should no-op.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(10), uint32(5), big.NewInt(2000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())
			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.Sign()).To(Equal(0))

			// Lower threshold; stake should now delegate.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(10), uint32(5), big.NewInt(1)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked = s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.String()).To(Equal("1000"))
			s.assertPoolInvariants(poolAddr)
		})

		It("blocks old owner from syncTotalStaked after ownership transfer", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			oldOwner := s.keyring.GetKey(0)
			newOwner := s.keyring.GetKey(1)

			s.execTxExpectSuccess(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", newOwner.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(500)),
				"Unauthorized()",
			)

			s.execTxExpectSuccess(
				newOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(500)),
			)
		})

		It("keeps unit state unchanged when withdraw reverts on insufficient liquid", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			amount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, amount)

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", amount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeUserUnits := s.queryPoolUint(1, poolAddr, "unitsOf", user.Addr)
			beforeTotalUnits := s.queryPoolUint(1, poolAddr, "totalUnits")

			_, _, err := s.factory.CallContractAndCheckLogs(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", amount),
				testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error()),
			)
			Expect(err).To(BeNil())

			afterUserUnits := s.queryPoolUint(1, poolAddr, "unitsOf", user.Addr)
			afterTotalUnits := s.queryPoolUint(1, poolAddr, "totalUnits")
			Expect(afterUserUnits.String()).To(Equal(beforeUserUnits.String()))
			Expect(afterTotalUnits.String()).To(Equal(beforeTotalUnits.String()))
		})

		It("stake is a no-op when liquid is below minStakeAmount", func() {
			// minStakeAmount is intentionally set above deposit.
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(2000))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.Sign()).To(Equal(0))
		})

		It("stake delegates liquid and updates totalStaked accounting", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.String()).To(Equal(depositAmount.String()))
		})

		It("stake creates on-chain delegation for pool contract delegator", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)
			firstVal := s.network.GetValidators()[0].OperatorAddress

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			poolDelegator := sdk.AccAddress(poolAddr.Bytes()).String()
			delRes, err := s.grpcHandler.GetDelegation(poolDelegator, firstVal)
			Expect(err).To(BeNil())
			Expect(delRes).ToNot(BeNil())
			Expect(delRes.DelegationResponse).ToNot(BeNil())
			Expect(delRes.DelegationResponse.Balance.Amount.IsPositive()).To(BeTrue())
		})

		It("harvest executes successfully after staking", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "harvest"),
			)
			Expect(s.network.NextBlock()).To(BeNil())
		})

		It("harvest does not modify totalStaked accounting", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeTotalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			beforeLiquid := s.queryPoolUint(1, poolAddr, "liquidBalance")

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "harvest"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			afterTotalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			afterLiquid := s.queryPoolUint(1, poolAddr, "liquidBalance")

			// Core invariant: harvest only affects liquid rewards, not delegated principal accounting.
			Expect(afterTotalStaked.String()).To(Equal(beforeTotalStaked.String()))
			// In no-reward conditions this can stay equal; with rewards it should increase.
			Expect(afterLiquid.Cmp(beforeLiquid)).To(BeNumerically(">=", 0))
		})

		It("claimRewards is a no-op without harvested rewards", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// No harvest happened, so user has no claimable rewards.
			beforeRewardReserve := s.queryPoolUint(1, poolAddr, "rewardReserve")

			_, ethRes, err := s.factory.CallContractAndCheckLogs(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimRewards"),
				testutil.LogCheckArgs{}.WithExpPass(true),
			)
			Expect(err).To(BeNil())
			Expect(s.network.NextBlock()).To(BeNil())

			afterRewardReserve := s.queryPoolUint(1, poolAddr, "rewardReserve")
			unpacked, err := s.communityPoolContract.ABI.Unpack("claimRewards", ethRes.Ret)
			Expect(err).To(BeNil())
			Expect(unpacked).To(HaveLen(1))
			claimedAmount, ok := unpacked[0].(*big.Int)
			Expect(ok).To(BeTrue())

			Expect(claimedAmount.Sign()).To(Equal(0))
			Expect(afterRewardReserve.String()).To(Equal(beforeRewardReserve.String()))
		})

		It("claimRewards is idempotent for a given reward index", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			depositAmount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, depositAmount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			// Harvest updates reward index and reserve (or leaves unchanged in zero-reward conditions).
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "harvest"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeFirstClaimReserve := s.queryPoolUint(1, poolAddr, "rewardReserve")

			_, firstClaimRes, err := s.factory.CallContractAndCheckLogs(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimRewards"),
				testutil.LogCheckArgs{}.WithExpPass(true),
			)
			Expect(err).To(BeNil())
			Expect(s.network.NextBlock()).To(BeNil())

			afterFirstClaimReserve := s.queryPoolUint(1, poolAddr, "rewardReserve")
			firstUnpacked, err := s.communityPoolContract.ABI.Unpack("claimRewards", firstClaimRes.Ret)
			Expect(err).To(BeNil())
			Expect(firstUnpacked).To(HaveLen(1))
			firstClaimed, ok := firstUnpacked[0].(*big.Int)
			Expect(ok).To(BeTrue())

			// First claim cannot increase reserve and cannot decrease user balance.
			Expect(afterFirstClaimReserve.Cmp(beforeFirstClaimReserve)).To(BeNumerically("<=", 0))
			Expect(firstClaimed.Sign()).To(BeNumerically(">=", 0))

			// Second immediate claim should be a no-op at the same reward index.
			_, secondClaimRes, err := s.factory.CallContractAndCheckLogs(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimRewards"),
				testutil.LogCheckArgs{}.WithExpPass(true),
			)
			Expect(err).To(BeNil())
			Expect(s.network.NextBlock()).To(BeNil())

			afterSecondClaimReserve := s.queryPoolUint(1, poolAddr, "rewardReserve")
			secondUnpacked, err := s.communityPoolContract.ABI.Unpack("claimRewards", secondClaimRes.Ret)
			Expect(err).To(BeNil())
			Expect(secondUnpacked).To(HaveLen(1))
			secondClaimed, ok := secondUnpacked[0].(*big.Int)
			Expect(ok).To(BeTrue())

			Expect(afterSecondClaimReserve.String()).To(Equal(afterFirstClaimReserve.String()))
			Expect(secondClaimed.Sign()).To(Equal(0))
		})

		It("syncTotalStaked updates accounting views (principalAssets and pricePerUnit)", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

			depositAmount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, depositAmount)

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", depositAmount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeAssets := s.queryPoolUint(0, poolAddr, "principalAssets")
			beforePPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			afterAssets := s.queryPoolUint(0, poolAddr, "principalAssets")
			afterPPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")

			Expect(beforeAssets.String()).To(Equal("1000"))
			Expect(beforePPU.String()).To(Equal("1000000000000000000"))
			Expect(afterAssets.String()).To(Equal("2000"))
			Expect(afterPPU.String()).To(Equal("2000000000000000000"))
		})

		It("syncTotalStaked does not create staking delegation side effects", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)

			poolDelegator := sdk.AccAddress(poolAddr.Bytes()).String()
			firstVal := s.network.GetValidators()[0].OperatorAddress

			_, err := s.grpcHandler.GetDelegation(poolDelegator, firstVal)
			Expect(err).ToNot(BeNil(), "expected no delegation before any staking action")

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(999)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			_, err = s.grpcHandler.GetDelegation(poolDelegator, firstVal)
			Expect(err).ToNot(BeNil(), "syncTotalStaked must not create staking delegation")
		})
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, "CommunityPool Integration Suite")
}
