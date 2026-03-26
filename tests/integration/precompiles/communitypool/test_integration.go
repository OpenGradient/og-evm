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
		var err error

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

			txArgs := buildTxArgs(poolAddr)
			callArgs := buildCallArgs(s.communityPoolContract, "deposit", big.NewInt(0))
			check := testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error())
			_, _, err := s.factory.CallContractAndCheckLogs(owner.Priv, txArgs, callArgs, check)
			Expect(err).To(BeNil())
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

		It("withdraws successfully when enough liquid is available", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
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
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", withdrawUnits),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			remainingUnits := s.queryPoolUint(1, poolAddr, "unitsOf", user.Addr)
			totalUnits := s.queryPoolUint(1, poolAddr, "totalUnits")
			Expect(remainingUnits.String()).To(Equal("600"))
			Expect(totalUnits.String()).To(Equal("600"))
		})

		It("reverts withdraw when proportional claim exceeds liquid balance", func() {
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

			// Inflate accounting assets without adding liquid balance.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			check := testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error())
			_, _, err = s.factory.CallContractAndCheckLogs(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "withdraw", big.NewInt(1000)),
				check,
			)
			Expect(err).To(BeNil())
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

			// poolAssets=2000, totalUnits=1000 => pricePerUnit=2e18.
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

			revertCheck := testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error())

			_, _, err := s.factory.CallContractAndCheckLogs(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(20), uint32(7), big.NewInt(2)),
				revertCheck,
			)
			Expect(err).To(BeNil())

			_, _, err = s.factory.CallContractAndCheckLogs(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(123)),
				revertCheck,
			)
			Expect(err).To(BeNil())

			_, _, err = s.factory.CallContractAndCheckLogs(
				nonOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", nonOwner.Addr),
				revertCheck,
			)
			Expect(err).To(BeNil())

			// Owner can still execute privileged actions.
			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(20), uint32(7), big.NewInt(2)),
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

			_, _, err = s.factory.CallContractAndCheckLogs(
				user2.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "deposit", big.NewInt(1)),
				testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error()),
			)
			Expect(err).To(BeNil())

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

			revertCheck := testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error())

			// Old owner should now be blocked.
			_, _, err = s.factory.CallContractAndCheckLogs(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "setConfig", uint32(99), uint32(9), big.NewInt(3)),
				revertCheck,
			)
			Expect(err).To(BeNil())

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
			_, _, err := s.factory.CallContractAndCheckLogs(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "transferOwnership", zeroAddr),
				testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error()),
			)
			Expect(err).To(BeNil())
		})

		It("allows owner to call all privileged methods", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
			owner := s.keyring.GetKey(0)

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

			maxValidators := s.queryPoolUint(0, poolAddr, "maxValidators")
			totalStaked := s.queryPoolUint(0, poolAddr, "totalStaked")
			Expect(maxValidators.String()).To(Equal("6"))
			Expect(totalStaked.String()).To(Equal("321"))
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

			revertCheck := testutil.LogCheckArgs{}.WithErrContains(vm.ErrExecutionReverted.Error())

			_, _, err = s.factory.CallContractAndCheckLogs(
				oldOwner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(500)),
				revertCheck,
			)
			Expect(err).To(BeNil())

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

			_, _, err = s.factory.CallContractAndCheckLogs(
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
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.Sign()).To(Equal(0))
		})

		It("stake delegates liquid and updates totalStaked accounting", func() {
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

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			totalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			Expect(totalStaked.String()).To(Equal(depositAmount.String()))
		})

		It("stake creates on-chain delegation for pool contract delegator", func() {
			poolAddr := s.deployCommunityPool(0, 10, 5, big.NewInt(1))
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
				user.Priv,
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
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "harvest"),
			)
			Expect(s.network.NextBlock()).To(BeNil())
		})

		It("harvest does not modify totalStaked accounting", func() {
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

			s.execTxExpectSuccess(
				user.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "stake"),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			beforeTotalStaked := s.queryPoolUint(1, poolAddr, "totalStaked")
			beforeLiquid := s.queryPoolUint(1, poolAddr, "liquidBalance")

			s.execTxExpectSuccess(
				user.Priv,
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

		It("syncTotalStaked updates accounting views (poolAssets and pricePerUnit)", func() {
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

			beforeAssets := s.queryPoolUint(0, poolAddr, "poolAssets")
			beforePPU := s.queryPoolUint(0, poolAddr, "pricePerUnit")

			s.execTxExpectSuccess(
				owner.Priv,
				buildTxArgs(poolAddr),
				buildCallArgs(s.communityPoolContract, "syncTotalStaked", big.NewInt(1000)),
			)
			Expect(s.network.NextBlock()).To(BeNil())

			afterAssets := s.queryPoolUint(0, poolAddr, "poolAssets")
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

