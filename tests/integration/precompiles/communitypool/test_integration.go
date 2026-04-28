package communitypool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	//nolint:revive
	. "github.com/onsi/ginkgo/v2"
	//nolint:revive
	. "github.com/onsi/gomega"

	"github.com/cosmos/evm/testutil/integration/evm/network"
)

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

		It("reverts withdraw when stakeable principal is non-zero", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			user := s.keyring.GetKey(1)
			amount := big.NewInt(1000)

			s.approveBondToken(1, poolAddr, amount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", amount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", big.NewInt(1)),
				"WithdrawRequiresAllPrincipalBonded(uint256)",
			)
		})

		It("reconcileTotalStaked updates principalAssets and pricePerUnit through totalStaked only", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)
			automation := s.keyring.GetKey(2)

			amount := big.NewInt(1000)
			s.approveBondToken(1, poolAddr, amount)
			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", amount),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			assetsBefore := s.queryPoolUint(0, poolAddr, "principalAssets")
			ppuBefore := s.queryPoolUint(0, poolAddr, "pricePerUnit")
			Expect(assetsBefore.String()).To(Equal("1000"))
			Expect(ppuBefore.String()).To(Equal("1000000000000000000"))

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setAutomationCaller", automation.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				automation.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "reconcileTotalStaked", big.NewInt(500)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked := s.queryPoolUint(0, poolAddr, "totalStaked")
			assetsAfter := s.queryPoolUint(0, poolAddr, "principalAssets")
			ppuAfter := s.queryPoolUint(0, poolAddr, "pricePerUnit")
			Expect(totalStaked.String()).To(Equal("500"))
			Expect(assetsAfter.String()).To(Equal("1500"))
			Expect(ppuAfter.String()).To(Equal("1500000000000000000"))
		})

		It("owner syncTotalStaked remains available and owner-gated", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			nonOwner := s.keyring.GetKey(1)

			s.execTxExpectCustomError(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(100)),
				"Unauthorized()",
			)

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(100)),
			)
			Expect(s.network.NextBlock()).To(BeNil())
			Expect(s.queryPoolUint(0, poolAddr, "totalStaked").String()).To(Equal("100"))
		})

		It("restricts reconcileTotalStaked to automation caller", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			automation := s.keyring.GetKey(2)

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setAutomationCaller", automation.Addr),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectCustomError(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "reconcileTotalStaked", big.NewInt(1)),
				"Unauthorized()",
			)

			s.execTxExpectSuccess(
				automation.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "reconcileTotalStaked", big.NewInt(321)),
			)
			Expect(s.network.NextBlock()).To(BeNil())
			Expect(s.queryPoolUint(0, poolAddr, "totalStaked").String()).To(Equal("321"))
		})

		It("returns expected pricePerUnit for empty and adjusted pool", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			user := s.keyring.GetKey(1)

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

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			updatedPPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")
			Expect(updatedPPU.String()).To(Equal("2000000000000000000"))
		})

		It("runs two-user withdraw maturity lifecycle and claimWithdraw payout", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)
			userA := s.keyring.GetKey(1)
			userB := s.keyring.GetKey(2)

			amountA := big.NewInt(900)
			amountB := big.NewInt(600)
			s.approveBondToken(1, poolAddr, amountA)
			s.approveBondToken(2, poolAddr, amountB)
			s.execTxExpectSuccess(userA.Priv, buildTxArgs(poolAddr), buildCallArgs(s.communityPoolContract, "deposit", amountA))
			s.execTxExpectSuccess(userB.Priv, buildTxArgs(poolAddr), buildCallArgs(s.communityPoolContract, "deposit", amountB))
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(owner.Priv, buildTxArgs(poolAddr), buildCallArgs(s.communityPoolContract, "stake"))
			Expect(s.network.NextBlock()).To(BeNil())

			userAUnits := s.queryPoolUint(0, poolAddr, "unitsOf", userA.Addr)
			totalStakedBefore := s.queryPoolUint(0, poolAddr, "totalStaked")
			totalUnitsBefore := s.queryPoolUint(0, poolAddr, "totalUnits")
			expectedOut := new(big.Int).Mul(new(big.Int).Set(userAUnits), new(big.Int).Set(totalStakedBefore))
			expectedOut.Quo(expectedOut, totalUnitsBefore)

			withdrawRes := s.execTxAndGetEthResponse(
				userA.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", userAUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			withdrawOut, err := s.communityPoolContract.ABI.Unpack("withdraw", withdrawRes.Ret)
			Expect(err).To(BeNil(), "failed to unpack withdraw output")
			Expect(withdrawOut).To(HaveLen(1))
			requestID, ok := withdrawOut[0].(*big.Int)
			Expect(ok).To(BeTrue(), "unexpected withdraw return type")

			req := s.queryWithdrawRequest(poolAddr, requestID)
			Expect(req.Owner).To(Equal(userA.Addr))
			Expect(req.AmountOut.String()).To(Equal(expectedOut.String()))
			Expect(req.Claimed).To(BeFalse())

			s.advanceToMaturity(req.Maturity)

			claimRes := s.execTxAndGetEthResponse(
				userA.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "claimWithdraw", requestID),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			claimOut, err := s.communityPoolContract.ABI.Unpack("claimWithdraw", claimRes.Ret)
			Expect(err).To(BeNil(), "failed to unpack claimWithdraw output")
			Expect(claimOut).To(HaveLen(1))
			claimedAmount, ok := claimOut[0].(*big.Int)
			Expect(ok).To(BeTrue(), "unexpected claimWithdraw return type")
			Expect(claimedAmount.String()).To(Equal(expectedOut.String()))

			reqAfter := s.queryWithdrawRequest(poolAddr, requestID)
			Expect(reqAfter.Claimed).To(BeTrue())

			s.assertPoolInvariants(poolAddr)
		})

		It("keeps ownership transfer behavior unchanged", func() {
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
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(99), uint32(9), big.NewInt(3)),
				"Unauthorized()",
			)

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
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, "CommunityPool Integration Suite")
}
