package vm

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/contracts"
	stakingprecompile "github.com/cosmos/evm/precompiles/staking"
	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/testutil/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var zeroAddress = common.Address{}

func (s *KeeperTestSuite) TestStakedBondVaultLiquidRedeemLifecycle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	depositAmount := big.NewInt(0).Mul(big.NewInt(5), big.NewInt(1e18))

	depositRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	depositOut, err := contracts.StakedBondVaultContract.ABI.Unpack("depositNative", depositRes.Ret)
	s.Require().NoError(err)
	shares := depositOut[0].(*big.Int)
	s.Require().Equal(0, depositAmount.Cmp(shares))
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalAssets")))
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "balanceOf", owner)))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", shares, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "CLAIMABLE")

	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, other)
	s.Require().ErrorContains(err, "PAYOUT")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.werc20Balance(vault).Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsUnauthorizedRedeemRequest() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", depositAmount, s.Keyring.GetAddr(1), owner)
	s.Require().ErrorContains(err, "insufficient allowance")
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsInvalidAndPausedActions() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(1e18)
	expectVaultError := func(value *big.Int, method string, contains string, args ...interface{}) {
		_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, value, method, args...)
		s.Require().ErrorContains(err, contains)
		s.Require().NoError(s.Network.NextBlock())
	}
	expectVaultFailure := func(value *big.Int, method string, args ...interface{}) {
		res, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, value, method, args...)
		if err != nil {
			s.Require().NoError(s.Network.NextBlock())
			return
		}
		s.Require().True(res.Failed(), "%s unexpectedly succeeded", method)
	}

	s.Require().Zero(s.vaultBig(vault, "maxDeposit", owner).Sign())
	s.Require().Zero(s.vaultBig(vault, "maxMint", owner).Sign())
	s.Require().Zero(s.vaultBig(vault, "maxWithdraw", owner).Sign())
	s.Require().Zero(s.vaultBig(vault, "maxRedeem", owner).Sign())
	expectVaultError(big.NewInt(0), "depositNative", "MIN", owner)
	expectVaultError(depositAmount, "depositNative", "RECEIVER", zeroAddress)
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", true, false, false)
	s.Require().NoError(err)
	expectVaultError(depositAmount, "depositNative", "D_PAUSED", owner)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, false)
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	expectVaultFailure(nil, "deposit", big.NewInt(0), owner)
	expectVaultFailure(nil, "mint", big.NewInt(0), owner)
	expectVaultError(nil, "withdraw", "REDEEM", depositAmount, owner, owner)
	expectVaultError(nil, "redeem", "REDEEM", depositAmount, owner, owner)
	expectVaultError(nil, "requestRedeem", "SHARES", big.NewInt(0), owner, owner)
	expectVaultError(nil, "requestRedeem", "RECEIVER", depositAmount, zeroAddress, owner)
	expectVaultError(nil, "requestRedeem", "OWNER", depositAmount, owner, zeroAddress)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, true, false)
	s.Require().NoError(err)
	expectVaultError(nil, "requestRedeem", "W_PAUSED", depositAmount, owner, owner)
	expectVaultError(nil, "claimRedeem", "REQUEST", big.NewInt(999), zeroAddress)
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsValueBearingNonNativeCalls() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	spender := s.Keyring.GetAddr(1)
	depositAmount := big.NewInt(1e18)
	dust := big.NewInt(1)
	zeroRole := common.Hash{}
	expectValueRejected := func(method string, args ...interface{}) {
		res, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, dust, method, args...)
		if err != nil {
			s.Require().NoError(s.Network.NextBlock())
			return
		}
		s.Require().True(res.Failed(), "%s accepted value-bearing call", method)
	}

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)

	expectValueRejected("deposit", big.NewInt(0), owner)
	expectValueRejected("mint", big.NewInt(0), owner)
	expectValueRejected("requestRedeem", depositAmount, owner, owner)
	expectValueRejected("claimRedeem", big.NewInt(999), zeroAddress)
	expectValueRejected("poke", big.NewInt(1))
	expectValueRejected("syncDelegations")
	expectValueRejected("setPaused", false, false, false)
	expectValueRejected("setValidatorPolicy", uint8(1))
	expectValueRejected("approve", spender, big.NewInt(1))
	expectValueRejected("increaseAllowance", spender, big.NewInt(1))
	expectValueRejected("decreaseAllowance", spender, big.NewInt(0))
	expectValueRejected("transfer", spender, big.NewInt(1))
	expectValueRejected("transferFrom", owner, spender, big.NewInt(1))
	expectValueRejected("grantRole", zeroRole, spender)
	expectValueRejected("revokeRole", zeroRole, spender)
	expectValueRejected("renounceRole", zeroRole, owner)

	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "balanceOf", owner)))
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalAssets")))
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsRawNativeTransfer() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	res, err := s.executeRawVaultTx(s.Keyring.GetPrivKey(0), vault, big.NewInt(1), nil)
	if err != nil {
		s.Require().NoError(s.Network.NextBlock())
		return
	}
	s.Require().True(res.Failed(), "raw native transfer unexpectedly succeeded")
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsUnauthorizedAdminActions() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "setPaused", true, true, true)
	s.Require().Error(err)
	s.Require().NoError(s.Network.NextBlock())
	s.Require().False(s.vaultBool(vault, "depositsPaused"))
	s.Require().False(s.vaultBool(vault, "withdrawalsPaused"))
	s.Require().False(s.vaultBool(vault, "pokePaused"))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().Error(err)
	s.Require().NoError(s.Network.NextBlock())
	// Default targetValidatorCount now covers the full active set (MAX_VALIDATORS=32) so stake
	// spreads across all validators; the rejected setValidatorPolicy(1) should not have changed it.
	s.Require().Equal(0, big.NewInt(32).Cmp(s.vaultBig(vault, "targetValidatorCount")))
}

// TestStakedBondVaultSettleWorksWhilePokePaused checks that pausing the scheduler (pokePaused)
// doesn't strand withdrawals: the permissionless settle() runs settlement on its own.
func (s *KeeperTestSuite) TestStakedBondVaultSettleWorksWhilePokePaused() {
	s.SetupTest()
	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(5), big.NewInt(1e18))

	depositRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	depositOut, err := contracts.StakedBondVaultContract.ABI.Unpack("depositNative", depositRes.Ret)
	s.Require().NoError(err)
	shares := depositOut[0].(*big.Int)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", shares, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	// Pause the scheduler poke.
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, true)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().Error(err, "poke must revert while pokePaused")
	s.Require().NoError(s.Network.NextBlock())

	// settle() bypasses pokePaused and makes the batch claimable.
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "settle", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err, "claims must succeed even while the scheduler is paused")
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
}

// TestStakedBondVaultPokeStepIsSelfOnly checks that pokeStep is only callable by the contract
// itself, through the internal self-call, and never externally.
func (s *KeeperTestSuite) TestStakedBondVaultPokeStepIsSelfOnly() {
	s.SetupTest()
	vault := s.deployStakedBondVault()
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "pokeStep", uint8(1))
	s.Require().Error(err, "external pokeStep call must revert (SELF)")
}

// TestStakedBondVaultPerRecipientPayouts checks that two redeemers in one batch each receive
// assets in proportion to their shares (3:2), not just a correct combined total.
func (s *KeeperTestSuite) TestStakedBondVaultPerRecipientPayouts() {
	s.SetupTest()
	vault := s.deployStakedBondVault()
	ownerA := s.Keyring.GetAddr(0)
	ownerB := s.Keyring.GetAddr(1)
	amountA := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))
	amountB := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	depA, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, amountA, "depositNative", ownerA)
	s.Require().NoError(err)
	sharesA := s.unpackBig(depA, "depositNative")
	depB, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, amountB, "depositNative", ownerB)
	s.Require().NoError(err)
	sharesB := s.unpackBig(depB, "depositNative")

	reqA, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", sharesA, ownerA, ownerA)
	s.Require().NoError(err)
	idA := s.unpackBig(reqA, "requestRedeem")
	reqB, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", sharesB, ownerB, ownerB)
	s.Require().NoError(err)
	idB := s.unpackBig(reqB, "requestRedeem")

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "settle", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())

	claimA, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", idA, zeroAddress)
	s.Require().NoError(err)
	payoutA := s.unpackBig(claimA, "claimRedeem")
	claimB, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", idB, zeroAddress)
	s.Require().NoError(err)
	payoutB := s.unpackBig(claimB, "claimRedeem")

	// Each redeemer gets their own deposit back (liquid batch, 1:1 NAV); the payouts aren't swapped.
	s.Require().Equal(0, payoutA.Cmp(amountA), "ownerA payout %s != %s", payoutA, amountA)
	s.Require().Equal(0, payoutB.Cmp(amountB), "ownerB payout %s != %s", payoutB, amountB)
}

func (s *KeeperTestSuite) unpackBig(res *evmtypes.MsgEthereumTxResponse, method string) *big.Int {
	out, err := contracts.StakedBondVaultContract.ABI.Unpack(method, res.Ret)
	s.Require().NoError(err)
	return out[0].(*big.Int)
}

// TestStakedBondVaultDepositPricedAfterSlash checks that once a slash pushes NAV below
// totalSupply, a fresh deposit is priced at the diverged share price (more shares per token)
// rather than a naive 1:1.
func (s *KeeperTestSuite) TestStakedBondVaultDepositPricedAfterSlash() {
	s.SetupTest()
	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(4), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	dep1, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	shares1 := s.unpackBig(dep1, "depositNative")
	s.Require().Equal(0, shares1.Cmp(depositAmount), "first deposit is 1:1")
	s.stakeVaultIdle(vault, depositAmount)

	// Slash the sole validator 50%, so NAV halves while supply stays the same.
	validator := s.selectedVaultSDKValidator(vault)
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)
	powerReduction := s.Network.App.GetStakingKeeper().PowerReduction(s.Network.GetContext())
	power := validator.GetConsensusPower(powerReduction)
	_, err = s.Network.App.GetStakingKeeper().Slash(
		s.Network.GetContext(), sdk.ConsAddress(consAddr),
		s.Network.GetContext().BlockHeight(), power, sdkmath.LegacyNewDecWithPrec(5, 1))
	s.Require().NoError(err)

	// NAV should have dropped below supply now that the slash is visible, so share price < 1.
	navAfter := s.vaultBig(vault, "totalAssets")
	s.Require().True(navAfter.Cmp(depositAmount) < 0, "slash should reduce NAV: %s !< %s", navAfter, depositAmount)

	// ERC4626 pricing should reflect the diverged NAV: a fresh deposit is quoted more shares
	// than tokens (share price < 1), not a naive 1:1.
	quoted := s.vaultBig(vault, "convertToShares", depositAmount)
	s.Require().True(quoted.Cmp(depositAmount) > 0, "post-slash deposit must be priced >1:1, got %s shares for %s", quoted, depositAmount)
}

// TestStakedBondVaultSlashBufferReservesIdle checks that with a non-zero slash buffer the vault
// keeps roughly bufferBps of NAV idle (un-delegated) instead of staking everything.
func (s *KeeperTestSuite) TestStakedBondVaultSlashBufferReservesIdle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(1000), big.NewInt(1e18))

	// 5% slash-exposure buffer.
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setSlashBufferBps", big.NewInt(500))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(500).Cmp(s.vaultBig(vault, "slashBufferBps")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	for i := 0; i < 24; i++ {
		_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
		s.Require().NoError(err)
	}

	idle := s.vaultBig(vault, "totalIdleLiquid")
	delegated := s.vaultBig(vault, "totalDelegated")
	nav := new(big.Int).Add(idle, delegated)
	expectedBuffer := new(big.Int).Div(new(big.Int).Mul(nav, big.NewInt(500)), big.NewInt(10000))

	// The buffer is never staked, so idle >= buffer; staking converges close to it.
	s.Require().True(idle.Cmp(expectedBuffer) >= 0, "idle %s below buffer %s", idle, expectedBuffer)
	overBuffer := new(big.Int).Sub(idle, expectedBuffer)
	s.Require().True(overBuffer.Cmp(big.NewInt(1e18)) < 0, "idle %s far above buffer %s", idle, expectedBuffer)
	s.Require().True(delegated.Cmp(nav) < 0, "buffer must leave some NAV un-delegated")
}

// TestStakedBondVaultVoteAbstainAccessControl checks that only the operator may call
// voteAbstain, and that it tolerates ids with no live proposal (each id's failure is swallowed).
func (s *KeeperTestSuite) TestStakedBondVaultVoteAbstainAccessControl() {
	s.SetupTest()
	vault := s.deployStakedBondVault()

	// Non-operator is rejected.
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "voteAbstain", []uint64{1})
	s.Require().Error(err)

	// The operator call succeeds even when the proposal id isn't in a voting period; each id's
	// failure is swallowed so a stale id can't fail the whole call.
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "voteAbstain", []uint64{1})
	s.Require().NoError(err)
}

// TestStakingGuardBlocksNonVaultDelegationViaPrecompile checks the PoA staking guard on the
// EVM path: a non-allowlisted EOA can't delegate through the staking precompile, so the
// validator set can't be captured from the EVM.
func (s *KeeperTestSuite) TestStakingGuardBlocksNonVaultDelegationViaPrecompile() {
	s.SetupTest()

	// Deploy the vault so the scheduler target (the guard's allowlist) is populated.
	s.deployStakedBondVault()

	vals := s.Network.GetValidators()
	s.Require().NotEmpty(vals)
	valoper := vals[0].OperatorAddress

	// A non-vault EOA (keyring[1]) attempts to delegate via the staking precompile.
	stranger := s.Keyring.GetAddr(1)
	stakingAddr := common.HexToAddress(evmtypes.StakingPrecompileAddress)
	_, err := s.Factory.ExecuteContractCall(
		s.Keyring.GetPrivKey(1),
		evmtypes.EvmTxArgs{To: &stakingAddr, GasLimit: 2_000_000},
		types.CallArgs{
			ContractABI: stakingprecompile.ABI,
			MethodName:  "delegate",
			Args:        []interface{}{stranger, valoper, big.NewInt(1e18)},
		},
	)
	s.Require().Error(err, "non-vault delegation via the staking precompile must be rejected by the PoA guard")
	s.Require().NoError(s.Network.NextBlock())
}

func (s *KeeperTestSuite) TestStakedBondVaultSetPausedTogglesExactFlags() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", true, true, true)
	s.Require().NoError(err)
	s.Require().True(s.vaultBool(vault, "depositsPaused"))
	s.Require().True(s.vaultBool(vault, "withdrawalsPaused"))
	s.Require().True(s.vaultBool(vault, "pokePaused"))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, false)
	s.Require().NoError(err)
	s.Require().False(s.vaultBool(vault, "depositsPaused"))
	s.Require().False(s.vaultBool(vault, "withdrawalsPaused"))
	s.Require().False(s.vaultBool(vault, "pokePaused"))
}

func (s *KeeperTestSuite) TestStakedBondVaultApprovedRedeemClaimPaysFixedReceiver() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	spender := s.Keyring.GetAddr(1)
	depositAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "approve", spender, depositAmount)
	s.Require().NoError(err)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", depositAmount, spender, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, owner)
	s.Require().ErrorContains(err, "PAYOUT")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.werc20Balance(vault).Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultMultiRequestBatchPaysResidualAndPrunes() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	ownerAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))
	otherAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, ownerAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, otherAmount, "depositNative", other)
	s.Require().NoError(err)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", ownerAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	ownerRequestID := requestOut[0].(*big.Int)
	requestRes, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", otherAmount, other, other)
	s.Require().NoError(err)
	requestOut, err = contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	otherRequestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", otherRequestID, zeroAddress)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", ownerRequestID, zeroAddress)
	s.Require().NoError(err)

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.werc20Balance(vault).Sign())
	res, err := s.callVaultWithData(vault, "withdrawalBatch", false, uint64(1))
	s.Require().NoError(err)
	batchOut, err := contracts.StakedBondVaultContract.ABI.Unpack("withdrawalBatch", res.Ret)
	s.Require().NoError(err)
	s.Require().False(batchOut[1].(bool))
	s.Require().False(batchOut[2].(bool))
}

func (s *KeeperTestSuite) TestStakedBondVaultDirectWrappedDonationBeforeFirstDepositDoesNotBlockExit() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(1)
	donation := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	depositAmount := big.NewInt(1e18)

	_, err := s.executeWERC20Tx(s.Keyring.GetPrivKey(0), nil, "transfer", vault, donation)
	s.Require().NoError(err)
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))

	depositRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	depositOut, err := contracts.StakedBondVaultContract.ABI.Unpack("depositNative", depositRes.Ret)
	s.Require().NoError(err)
	shares := depositOut[0].(*big.Int)
	s.Require().Equal(0, depositAmount.Cmp(shares))
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalAssets")))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", shares, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))
}

func (s *KeeperTestSuite) TestStakedBondVaultDoesNotStakeOrphanDonationWithoutShares() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	donation := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := s.executeWERC20Tx(s.Keyring.GetPrivKey(0), nil, "transfer", vault, donation)
	s.Require().NoError(err)
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsBadValidatorConfiguration() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(0))
	s.Require().ErrorContains(err, "COUNT")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(33))
	s.Require().ErrorContains(err, "COUNT")
}

func (s *KeeperTestSuite) TestStakedBondVaultSetValidatorPolicyStoresTargetCount() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "targetValidatorCount")))
	s.Require().Zero(s.vaultBig(vault, "validatorCount").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultPokeAutoSelectsBondedValidators() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(3))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(3).Cmp(s.vaultBig(vault, "selectedValidatorCount")))
	s.Require().Equal(0, big.NewInt(3).Cmp(s.vaultBig(vault, "validatorCount")))

	for i := int64(0); i < 3; i++ {
		res, err := s.callVaultWithData(vault, "validators", false, big.NewInt(i))
		s.Require().NoError(err)
		out, err := contracts.StakedBondVaultContract.ABI.Unpack("validators", res.Ret)
		s.Require().NoError(err)
		s.Require().NotEmpty(out[0].(string))
		s.Require().Positive(out[1].(*big.Int).Sign())
		s.Require().Zero(out[2].(*big.Int).Sign())
		s.Require().True(out[3].(bool))
	}
}

func (s *KeeperTestSuite) TestStakedBondVaultStakeIdleAcrossAutomaticallySelectedValidators() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(10_001), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(3))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)

	delegatedAcrossSelected := big.NewInt(0)
	for i := int64(0); i < 3; i++ {
		res, err := s.callVaultWithData(vault, "validators", false, big.NewInt(int64(i)))
		s.Require().NoError(err)
		out, err := contracts.StakedBondVaultContract.ABI.Unpack("validators", res.Ret)
		s.Require().NoError(err)
		s.Require().NotEmpty(out[0].(string))
		s.Require().Positive(out[1].(*big.Int).Sign())
		s.Require().Positive(out[2].(*big.Int).Sign())
		s.Require().True(out[3].(bool))
		delegatedAcrossSelected.Add(delegatedAcrossSelected, out[2].(*big.Int))
	}
	s.requireVaultDelegatedClose(vault, depositAmount)
	// Equal-spread across the 3 selected validators covers ~the full deposit; a small
	// rounding dust (< MIN_OPERATION_ASSETS) may remain idle under the per-validator cap.
	dust := new(big.Int).Sub(depositAmount, delegatedAcrossSelected)
	s.Require().True(dust.Sign() >= 0 && dust.Cmp(big.NewInt(1e15)) < 0, "unexpected spread shortfall: %s", dust)
}

func (s *KeeperTestSuite) TestStakedBondVaultCanUpdateValidatorPolicyWhileDelegated() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(3))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", s.Keyring.GetAddr(0))
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)

	delegated := s.vaultBig(vault, "totalDelegated")
	s.Require().Positive(delegated.Sign())
	s.Require().Equal(0, delegated.Cmp(s.vaultBig(vault, "liveDelegatedAssets")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "selectedValidatorCount")))
	s.Require().Equal(0, delegated.Cmp(s.vaultBig(vault, "totalDelegated")))
}

func (s *KeeperTestSuite) TestStakedBondVaultPokeRecordsBoundedWork() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	depositAmount := big.NewInt(1e18)

	res, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(0))
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack("poke", res.Ret)
	s.Require().NoError(err)
	s.Require().Zero(out[0].(*big.Int).Sign())
	s.Require().Zero(s.vaultBig(vault, "pokeCount").Sign())
	s.Require().Zero(s.vaultBig(vault, "lastPokeOps").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, true)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(1))
	s.Require().ErrorContains(err, "P_PAUSED")
	s.Require().NoError(s.Network.NextBlock())
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, false)
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", s.Keyring.GetAddr(0))
	s.Require().NoError(err)

	res, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(1))
	s.Require().NoError(err)
	out, err = contracts.StakedBondVaultContract.ABI.Unpack("poke", res.Ret)
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(out[0].(*big.Int)))
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "pokeCount")))
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "lastPokeOps")))
	s.Require().Positive(s.vaultBig(vault, "selectedValidatorCount").Sign())
	s.stakeVaultIdle(vault, depositAmount)
}

func (s *KeeperTestSuite) TestStakedBondVaultStakedRedeemLifecycle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)
	s.requireVaultDelegatedClose(vault, depositAmount)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", depositAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.requireVaultUnbondingClose(vault, depositAmount)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "CLAIMABLE")
	s.Require().NoError(s.Network.NextBlock())

	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "REQUEST")
	s.Require().NoError(s.Network.NextBlock())

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultMixedLiquidAndStakedRedeemLifecycle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	stakedAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	liquidAmount := big.NewInt(1e18)
	totalAmount := big.NewInt(0).Add(stakedAmount, liquidAmount)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, stakedAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, stakedAmount)
	s.requireVaultDelegatedClose(vault, stakedAmount)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, liquidAmount, "depositNative", owner)
	s.Require().NoError(err)
	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", totalAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.requireVaultUnbondingClose(vault, stakedAmount)
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "CLAIMABLE")
	s.Require().NoError(s.Network.NextBlock())

	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultPendingUnbondingDoesNotSkipSettlementAfterLiquidBatch() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	stakedAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	liquidAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, stakedAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, stakedAmount)
	s.requireVaultDelegatedClose(vault, stakedAmount)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", stakedAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	stakedRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.requireVaultUnbondingClose(vault, stakedAmount)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "nextSettlementBatchId")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, liquidAmount, "depositNative", other)
	s.Require().NoError(err)
	requestRes, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", liquidAmount, other, other)
	s.Require().NoError(err)
	requestOut, err = contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	liquidRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "nextSettlementBatchId")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", liquidRequestID, zeroAddress)
	s.Require().NoError(err)

	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", stakedRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultDepositDuringPendingUnbondingCanStakeAndExit() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	ownerAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	otherAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, ownerAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, ownerAmount)
	s.requireVaultDelegatedClose(vault, ownerAmount)

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", ownerAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	ownerRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.requireVaultUnbondingClose(vault, ownerAmount)
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, otherAmount, "depositNative", other)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, otherAmount)
	s.requireVaultUnbondingClose(vault, ownerAmount)

	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", ownerRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
	s.requireVaultDelegatedClose(vault, otherAmount)

	otherShares := s.vaultBig(vault, "balanceOf", other)
	requestRes, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", otherShares, other, other)
	s.Require().NoError(err)
	requestOut, err = contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	otherRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)

	s.waitAndSettleVaultBatch(vault, 2)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", otherRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultSkipsValidatorAtUnbondingEntryCap() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(5), big.NewInt(1e18))
	requestShares := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e15))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(2))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)

	for i := 0; i < 8; i++ {
		_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", requestShares, owner, owner)
		s.Require().NoError(err)
		_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
		s.Require().NoError(err)
	}

	expectedUnbonding := big.NewInt(0).Mul(requestShares, big.NewInt(8))
	s.requireVaultUnbondingClose(vault, expectedUnbonding)
}

func (s *KeeperTestSuite) TestStakedBondVaultSlashedStakeCanRedeemRemainingAssets() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)
	validator := s.selectedVaultSDKValidator(vault)
	beforeSlash := s.vaultBig(vault, "liveDelegatedAssets")
	s.Require().Positive(beforeSlash.Sign())

	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)
	powerReduction := s.Network.App.GetStakingKeeper().PowerReduction(s.Network.GetContext())
	power := validator.GetConsensusPower(powerReduction)
	_, err = s.Network.App.GetStakingKeeper().Slash(
		s.Network.GetContext(),
		sdk.ConsAddress(consAddr),
		s.Network.GetContext().BlockHeight(),
		power,
		sdkmath.LegacyNewDecWithPrec(5, 1),
	)
	s.Require().NoError(err)
	afterSlash := s.vaultBig(vault, "liveDelegatedAssets")
	s.Require().Positive(afterSlash.Sign())
	s.Require().Negative(afterSlash.Cmp(beforeSlash))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", depositAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	unbonding := s.vaultBig(vault, "totalWithdrawalUnbonding")
	s.Require().Positive(unbonding.Sign())
	s.Require().True(unbonding.Cmp(depositAmount) <= 0)

	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

// TestStakedBondVaultFullySlashedBatchStillClaimable checks that a withdrawal batch whose
// unbonding proceeds are entirely slashed away is still marked claimable, settled at its
// liquid portion, instead of being stranded.
func (s *KeeperTestSuite) TestStakedBondVaultFullySlashedBatchStillClaimable() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)

	// Request full redemption and undelegate it.
	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", depositAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)
	validator := s.selectedVaultSDKValidator(vault)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)

	// Slash the validator 100% while the batch is unbonding, so proceeds go to zero.
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)
	powerReduction := s.Network.App.GetStakingKeeper().PowerReduction(s.Network.GetContext())
	power := validator.GetConsensusPower(powerReduction)
	_, err = s.Network.App.GetStakingKeeper().Slash(
		s.Network.GetContext(),
		sdk.ConsAddress(consAddr),
		s.Network.GetContext().BlockHeight(),
		power,
		sdkmath.LegacyOneDec(),
	)
	s.Require().NoError(err)

	// Settle and claim: the batch should be claimable, not stranded, despite the total loss.
	s.waitAndSettleVaultBatch(vault, 1)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err, "fully-slashed batch must still be claimable, not permanently stranded")
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultSlashSyncUpdatesCachedDelegations() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidatorPolicy", uint8(1))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", s.Keyring.GetAddr(0))
	s.Require().NoError(err)
	s.stakeVaultIdle(vault, depositAmount)
	validator := s.selectedVaultSDKValidator(vault)

	before := s.vaultBig(vault, "liveDelegatedAssets")
	s.Require().Positive(before.Sign())

	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)
	powerReduction := s.Network.App.GetStakingKeeper().PowerReduction(s.Network.GetContext())
	power := validator.GetConsensusPower(powerReduction)
	_, err = s.Network.App.GetStakingKeeper().Slash(
		s.Network.GetContext(),
		sdk.ConsAddress(consAddr),
		s.Network.GetContext().BlockHeight(),
		power,
		sdkmath.LegacyNewDecWithPrec(5, 1),
	)
	s.Require().NoError(err)

	after := s.vaultBig(vault, "liveDelegatedAssets")
	s.Require().Negative(after.Cmp(before))

	_, err = s.callVaultWithData(vault, "syncDelegations", true)
	s.Require().NoError(err)
	s.Require().Equal(0, after.Cmp(s.vaultBig(vault, "totalDelegated")))
}

func (s *KeeperTestSuite) stakeVaultIdle(vault common.Address, expectedDelegated *big.Int) {
	for i := 0; i < 24 && s.vaultBig(vault, "totalDelegated").Cmp(expectedDelegated) < 0; i++ {
		_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
		s.Require().NoError(err)
	}
	s.requireVaultDelegatedClose(vault, expectedDelegated)
}

func (s *KeeperTestSuite) requireVaultDelegatedClose(vault common.Address, expectedDelegated *big.Int) {
	delegated := s.vaultBig(vault, "totalDelegated")
	idle := s.vaultBig(vault, "totalIdleLiquid")
	s.Require().True(delegated.Cmp(expectedDelegated) <= 0)
	dust := big.NewInt(0).Sub(expectedDelegated, delegated)
	s.Require().True(dust.Cmp(big.NewInt(1e15)) < 0, "delegation dust %s exceeds minimum operation size", dust.String())
	s.Require().Equal(0, dust.Cmp(idle))
}

func (s *KeeperTestSuite) requireVaultUnbondingClose(vault common.Address, expectedUnbonding *big.Int) {
	unbonding := s.vaultBig(vault, "totalWithdrawalUnbonding")
	s.Require().True(unbonding.Cmp(expectedUnbonding) <= 0)
	dust := big.NewInt(0).Sub(expectedUnbonding, unbonding)
	s.Require().True(dust.Cmp(big.NewInt(1e15)) < 0, "unbonding dust %s exceeds minimum operation size", dust.String())
}

func (s *KeeperTestSuite) waitAndSettleVaultBatch(vault common.Address, batchID uint64) {
	maturity := s.vaultBatchMaturity(vault, batchID)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(6))
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) selectedVaultSDKValidator(vault common.Address) stakingtypes.Validator {
	res, err := s.callVaultWithData(vault, "validators", false, big.NewInt(0))
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack("validators", res.Ret)
	s.Require().NoError(err)
	operatorAddress := sdk.ValAddress(common.HexToAddress(out[0].(string)).Bytes()).String()
	for _, validator := range s.Network.GetValidators() {
		if validator.OperatorAddress == operatorAddress {
			return validator
		}
	}
	s.T().Fatalf("selected validator %s not found", operatorAddress)
	return stakingtypes.Validator{}
}

func (s *KeeperTestSuite) deployStakedBondVault() common.Address {
	admin := s.Keyring.GetAddr(0)
	vault, err := s.Factory.DeployContract(
		s.Keyring.GetPrivKey(0),
		evmtypes.EvmTxArgs{GasLimit: 8_000_000},
		types.ContractDeploymentData{
			Contract: contracts.StakedBondVaultContract,
			// The test genesis registers the native ERC20 at WEVMOSContractMainnet (0xD494...),
			// so the vault's asset points there; production and local deploys use 0xEeee...EEeE.
			ConstructorArgs: []interface{}{admin, common.HexToAddress(testconstants.WEVMOSContractMainnet), "Staked Bond", "stBOND"},
		},
	)
	s.Require().NoError(err)

	// Authorize the deployed vault as the sole staking-guard delegator by pointing the EVM
	// scheduler's target contract at it, mirroring the production deploy step. The deploy tx
	// above ran in the live finalize-block state without committing, so write the param into
	// that same state (via a context over it) and let the NextBlock below commit the deploy and
	// the param together. Otherwise the guard fails closed and rejects the vault's own
	// delegations during later poke txs.
	baseApp := s.Network.App.GetBaseApp()
	liveCtx := baseApp.NewContextLegacy(false, s.Network.GetContext().BlockHeader())
	evmKeeper := s.Network.App.GetEVMKeeper()
	params := evmKeeper.GetParams(liveCtx)
	params.Scheduler.TargetContract = vault.Hex()
	s.Require().NoError(evmKeeper.SetParams(liveCtx, params))

	s.Require().NoError(s.Network.NextBlock())
	return vault
}

func (s *KeeperTestSuite) executeVaultTx(
	priv cryptotypes.PrivKey,
	vault common.Address,
	value *big.Int,
	method string,
	args ...interface{},
) (*evmtypes.MsgEthereumTxResponse, error) {
	txArgs := evmtypes.EvmTxArgs{
		To:       &vault,
		GasLimit: 8_000_000,
		Amount:   value,
	}
	res, err := s.Factory.ExecuteContractCall(
		priv,
		txArgs,
		types.CallArgs{
			ContractABI: contracts.StakedBondVaultContract.ABI,
			MethodName:  method,
			Args:        args,
		},
	)
	if err != nil {
		return nil, err
	}
	ethRes, err := s.Factory.GetEvmTransactionResponseFromTxResult(res)
	if err != nil {
		return nil, err
	}
	return ethRes, s.Network.NextBlock()
}

func (s *KeeperTestSuite) executeRawVaultTx(
	priv cryptotypes.PrivKey,
	vault common.Address,
	value *big.Int,
	input []byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	txArgs := evmtypes.EvmTxArgs{
		To:       &vault,
		GasLimit: 8_000_000,
		Amount:   value,
		Input:    input,
	}
	res, err := s.Factory.ExecuteEthTx(priv, txArgs)
	if err != nil {
		return nil, err
	}
	ethRes, err := s.Factory.GetEvmTransactionResponseFromTxResult(res)
	if err != nil {
		return nil, err
	}
	return ethRes, s.Network.NextBlock()
}

func (s *KeeperTestSuite) executeWERC20Tx(
	priv cryptotypes.PrivKey,
	value *big.Int,
	method string,
	args ...interface{},
) (*evmtypes.MsgEthereumTxResponse, error) {
	werc20 := common.HexToAddress(testconstants.WEVMOSContractMainnet)
	txArgs := evmtypes.EvmTxArgs{
		To:       &werc20,
		GasLimit: 8_000_000,
		Amount:   value,
	}
	res, err := s.Factory.ExecuteContractCall(
		priv,
		txArgs,
		types.CallArgs{
			ContractABI: contracts.WATOMContract.ABI,
			MethodName:  method,
			Args:        args,
		},
	)
	if err != nil {
		return nil, err
	}
	ethRes, err := s.Factory.GetEvmTransactionResponseFromTxResult(res)
	if err != nil {
		return nil, err
	}
	return ethRes, s.Network.NextBlock()
}

func (s *KeeperTestSuite) vaultBig(vault common.Address, method string, args ...interface{}) *big.Int {
	res, err := s.callVaultWithData(vault, method, false, args...)
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack(method, res.Ret)
	s.Require().NoError(err)
	switch value := out[0].(type) {
	case *big.Int:
		return value
	case uint64:
		return new(big.Int).SetUint64(value)
	case uint8:
		return big.NewInt(int64(value))
	default:
		s.T().Fatalf("unexpected numeric type %T", value)
		return nil
	}
}

func (s *KeeperTestSuite) vaultBool(vault common.Address, method string, args ...interface{}) bool {
	res, err := s.callVaultWithData(vault, method, false, args...)
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack(method, res.Ret)
	s.Require().NoError(err)
	value, ok := out[0].(bool)
	s.Require().True(ok, "unexpected bool type %T", out[0])
	return value
}

func (s *KeeperTestSuite) vaultBatchMaturity(vault common.Address, batchID uint64) int64 {
	res, err := s.callVaultWithData(vault, "withdrawalBatch", false, batchID)
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack("withdrawalBatch", res.Ret)
	s.Require().NoError(err)
	switch maturity := out[11].(type) {
	case int64:
		return maturity
	case *big.Int:
		return maturity.Int64()
	default:
		s.T().Fatalf("unexpected maturity type %T", out[11])
		return 0
	}
}

func (s *KeeperTestSuite) callVaultWithData(vault common.Address, method string, commit bool, args ...interface{}) (*evmtypes.MsgEthereumTxResponse, error) {
	data, err := contracts.StakedBondVaultContract.ABI.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	return s.Network.App.GetEVMKeeper().CallEVMWithData(
		s.Network.GetContext(),
		erc20types.ModuleAddress,
		&vault,
		data,
		commit,
		big.NewInt(8_000_000),
	)
}

func (s *KeeperTestSuite) werc20Balance(account common.Address) *big.Int {
	werc20 := common.HexToAddress(testconstants.WEVMOSContractMainnet)
	data, err := contracts.ERC20MinterBurnerDecimalsContract.ABI.Pack("balanceOf", account)
	s.Require().NoError(err)
	res, err := s.Network.App.GetEVMKeeper().CallEVMWithData(
		s.Network.GetContext(),
		erc20types.ModuleAddress,
		&werc20,
		data,
		false,
		big.NewInt(8_000_000),
	)
	s.Require().NoError(err)
	out, err := contracts.ERC20MinterBurnerDecimalsContract.ABI.Unpack("balanceOf", res.Ret)
	s.Require().NoError(err)
	return out[0].(*big.Int)
}
