package vm

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/contracts"
	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/testutil/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	s.Require().ErrorContains(err, "not claimable")

	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, other)
	s.Require().ErrorContains(err, "wrong receiver")
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
	expectVaultError(big.NewInt(0), "depositNative", "zero assets", owner)
	expectVaultError(depositAmount, "depositNative", "receiver zero", zeroAddress)
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", true, false, false)
	s.Require().NoError(err)
	expectVaultError(depositAmount, "depositNative", "deposits paused", owner)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, false)
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	expectVaultFailure(nil, "deposit", big.NewInt(0), owner)
	expectVaultFailure(nil, "mint", big.NewInt(0), owner)
	expectVaultError(nil, "withdraw", "use requestRedeem", depositAmount, owner, owner)
	expectVaultError(nil, "redeem", "use requestRedeem", depositAmount, owner, owner)
	expectVaultError(nil, "requestRedeem", "zero shares", big.NewInt(0), owner, owner)
	expectVaultError(nil, "requestRedeem", "receiver zero", depositAmount, zeroAddress, owner)
	expectVaultError(nil, "requestRedeem", "owner zero", depositAmount, owner, zeroAddress)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, true, false)
	s.Require().NoError(err)
	expectVaultError(nil, "requestRedeem", "withdrawals paused", depositAmount, owner, owner)
	expectVaultError(nil, "claimRedeem", "unknown request", big.NewInt(999), zeroAddress)
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
	expectValueRejected("setValidators", []string{}, []uint16{})
	expectValueRejected("approve", spender, big.NewInt(1))
	expectValueRejected("increaseAllowance", spender, big.NewInt(1))
	expectValueRejected("decreaseAllowance", spender, big.NewInt(0))
	expectValueRejected("transfer", spender, big.NewInt(1))
	expectValueRejected("transferFrom", owner, spender, big.NewInt(1))
	expectValueRejected("permit", owner, spender, big.NewInt(0), big.NewInt(0), uint8(27), zeroRole, zeroRole)
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
	val := s.Network.GetValidators()[0].OperatorAddress

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "setPaused", true, true, true)
	s.Require().Error(err)
	s.Require().NoError(s.Network.NextBlock())
	s.Require().False(s.vaultBool(vault, "depositsPaused"))
	s.Require().False(s.vaultBool(vault, "withdrawalsPaused"))
	s.Require().False(s.vaultBool(vault, "pokePaused"))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().Error(err)
	s.Require().NoError(s.Network.NextBlock())
	s.Require().Zero(s.vaultBig(vault, "validatorCount").Sign())
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

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, owner)
	s.Require().ErrorContains(err, "wrong receiver")
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

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
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
	expectedPayout := big.NewInt(0).Add(donation, depositAmount)

	_, err := s.executeWERC20Tx(s.Keyring.GetPrivKey(0), nil, "transfer", vault, donation)
	s.Require().NoError(err)
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))

	depositRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	depositOut, err := contracts.StakedBondVaultContract.ABI.Unpack("depositNative", depositRes.Ret)
	s.Require().NoError(err)
	shares := depositOut[0].(*big.Int)
	s.Require().Equal(0, depositAmount.Cmp(shares))
	s.Require().Equal(0, expectedPayout.Cmp(s.vaultBig(vault, "totalAssets")))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", shares, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().True(s.werc20Balance(vault).Cmp(donation) <= 0)
}

func (s *KeeperTestSuite) TestStakedBondVaultDoesNotStakeOrphanDonationWithoutShares() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	donation := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeWERC20Tx(s.Keyring.GetPrivKey(0), nil, "transfer", vault, donation)
	s.Require().NoError(err)
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())
	s.Require().Equal(0, donation.Cmp(s.werc20Balance(vault)))
}

func (s *KeeperTestSuite) TestStakedBondVaultRejectsBadValidatorConfiguration() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress

	_, err := s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		[]string{val, val},
		[]uint16{5_000, 5_000},
	)
	s.Require().ErrorContains(err, "duplicate validator")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		[]string{val},
		[]uint16{9_999},
	)
	s.Require().ErrorContains(err, "bad weights")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		[]string{val},
		[]uint16{},
	)
	s.Require().ErrorContains(err, "length mismatch")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		[]string{""},
		[]uint16{10_000},
	)
	s.Require().ErrorContains(err, "empty validator")
	s.Require().NoError(s.Network.NextBlock())

	_, err = s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		[]string{val},
		[]uint16{0},
	)
	s.Require().ErrorContains(err, "zero weight")
	s.Require().NoError(s.Network.NextBlock())

	operatorAddresses := make([]string, 33)
	weights := make([]uint16, 33)
	for i := range operatorAddresses {
		operatorAddresses[i] = val
		weights[i] = 1
	}
	_, err = s.executeVaultTx(
		s.Keyring.GetPrivKey(0),
		vault,
		nil,
		"setValidators",
		operatorAddresses,
		weights,
	)
	s.Require().ErrorContains(err, "too many validators")
}

func (s *KeeperTestSuite) TestStakedBondVaultSetValidatorsStoresExactConfig() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "validatorCount")))

	res, err := s.callVaultWithData(vault, "validators", false, big.NewInt(0))
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack("validators", res.Ret)
	s.Require().NoError(err)
	s.Require().Equal(val, out[0].(string))
	s.Require().Equal(uint16(10_000), out[1].(uint16))
	s.Require().Zero(out[2].(*big.Int).Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultStakeIdleSplitsAcrossValidatorsWithRemainder() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	vals := s.Network.GetValidators()
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(10_001), big.NewInt(1e18))
	operatorAddresses := []string{
		vals[0].OperatorAddress,
		vals[1].OperatorAddress,
		vals[2].OperatorAddress,
	}
	weights := []uint16{3_333, 3_333, 3_334}
	expectedFirst := big.NewInt(0).Div(big.NewInt(0).Mul(depositAmount, big.NewInt(3_333)), big.NewInt(10_000))
	expectedSecond := big.NewInt(0).Set(expectedFirst)
	expectedThird := big.NewInt(0).Sub(big.NewInt(0).Sub(depositAmount, expectedFirst), expectedSecond)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", operatorAddresses, weights)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)

	for i, expected := range []*big.Int{expectedFirst, expectedSecond, expectedThird} {
		res, err := s.callVaultWithData(vault, "validators", false, big.NewInt(int64(i)))
		s.Require().NoError(err)
		out, err := contracts.StakedBondVaultContract.ABI.Unpack("validators", res.Ret)
		s.Require().NoError(err)
		s.Require().Equal(operatorAddresses[i], out[0].(string))
		s.Require().Equal(weights[i], out[1].(uint16))
		s.Require().Equal(0, expected.Cmp(out[2].(*big.Int)))
	}
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalDelegated")))
}

func (s *KeeperTestSuite) TestStakedBondVaultStakesAndBlocksValidatorUpdatesWhileDelegated() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", s.Keyring.GetAddr(0))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)

	delegated := s.vaultBig(vault, "totalDelegated")
	s.Require().Positive(delegated.Sign())
	s.Require().Equal(0, delegated.Cmp(s.vaultBig(vault, "liveDelegatedAssets")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{}, []uint16{})
	s.Require().ErrorContains(err, "delegations active")
}

func (s *KeeperTestSuite) TestStakedBondVaultPokeRecordsBoundedWork() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
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
	s.Require().ErrorContains(err, "poke paused")
	s.Require().NoError(s.Network.NextBlock())
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, false)
	s.Require().NoError(err)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
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
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalDelegated")))
}

func (s *KeeperTestSuite) TestStakedBondVaultStakedRedeemLifecycle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalDelegated")))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", depositAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, depositAmount.Cmp(s.vaultBig(vault, "totalWithdrawalUnbonding")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "not claimable")
	s.Require().NoError(s.Network.NextBlock())

	maturity := s.vaultBatchMaturity(vault, 1)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "unknown request")
	s.Require().NoError(s.Network.NextBlock())

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultMixedLiquidAndStakedRedeemLifecycle() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	owner := s.Keyring.GetAddr(0)
	stakedAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	liquidAmount := big.NewInt(1e18)
	totalAmount := big.NewInt(0).Add(stakedAmount, liquidAmount)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, stakedAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, stakedAmount.Cmp(s.vaultBig(vault, "totalDelegated")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, liquidAmount, "depositNative", owner)
	s.Require().NoError(err)
	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", totalAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	requestID := requestOut[0].(*big.Int)

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, stakedAmount.Cmp(s.vaultBig(vault, "totalWithdrawalUnbonding")))
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().ErrorContains(err, "not claimable")
	s.Require().NoError(s.Network.NextBlock())

	maturity := s.vaultBatchMaturity(vault, 1)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)

	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultPendingUnbondingDoesNotSkipSettlementAfterLiquidBatch() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	stakedAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	liquidAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, stakedAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, stakedAmount.Cmp(s.vaultBig(vault, "totalDelegated")))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", stakedAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	stakedRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, stakedAmount.Cmp(s.vaultBig(vault, "totalWithdrawalUnbonding")))
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "nextSettlementBatchId")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, liquidAmount, "depositNative", other)
	s.Require().NoError(err)
	requestRes, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", liquidAmount, other, other)
	s.Require().NoError(err)
	requestOut, err = contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	liquidRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(s.vaultBig(vault, "nextSettlementBatchId")))

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", liquidRequestID, zeroAddress)
	s.Require().NoError(err)

	maturity := s.vaultBatchMaturity(vault, 1)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", stakedRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultDepositDuringPendingUnbondingCanStakeAndExit() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	val := s.Network.GetValidators()[0].OperatorAddress
	owner := s.Keyring.GetAddr(0)
	other := s.Keyring.GetAddr(1)
	ownerAmount := big.NewInt(0).Mul(big.NewInt(2), big.NewInt(1e18))
	otherAmount := big.NewInt(1e18)

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{val}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, ownerAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, ownerAmount.Cmp(s.vaultBig(vault, "totalDelegated")))

	requestRes, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "requestRedeem", ownerAmount, owner, owner)
	s.Require().NoError(err)
	requestOut, err := contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	ownerRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, ownerAmount.Cmp(s.vaultBig(vault, "totalWithdrawalUnbonding")))
	s.Require().Zero(s.vaultBig(vault, "totalDelegated").Sign())

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, otherAmount, "depositNative", other)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	s.Require().Equal(0, otherAmount.Cmp(s.vaultBig(vault, "totalDelegated")))
	s.Require().Equal(0, ownerAmount.Cmp(s.vaultBig(vault, "totalWithdrawalUnbonding")))

	maturity := s.vaultBatchMaturity(vault, 1)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", ownerRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
	s.Require().Equal(0, otherAmount.Cmp(s.vaultBig(vault, "totalDelegated")))

	otherShares := s.vaultBig(vault, "balanceOf", other)
	requestRes, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "requestRedeem", otherShares, other, other)
	s.Require().NoError(err)
	requestOut, err = contracts.StakedBondVaultContract.ABI.Unpack("requestRedeem", requestRes.Ret)
	s.Require().NoError(err)
	otherRequestID := requestOut[0].(*big.Int)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)

	maturity = s.vaultBatchMaturity(vault, 2)
	wait = time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(1), vault, nil, "claimRedeem", otherRequestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultSlashedStakeCanRedeemRemainingAssets() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	validator := s.Network.GetValidators()[0]
	owner := s.Keyring.GetAddr(0)
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{validator.OperatorAddress}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", owner)
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
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
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	unbonding := s.vaultBig(vault, "totalWithdrawalUnbonding")
	s.Require().Positive(unbonding.Sign())
	s.Require().True(unbonding.Cmp(depositAmount) <= 0)

	maturity := s.vaultBatchMaturity(vault, 1)
	wait := time.Unix(maturity, 0).Sub(s.Network.GetContext().BlockTime()) + time.Second
	if wait > 0 {
		s.Require().NoError(s.Network.NextBlockAfter(wait))
	}

	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "claimRedeem", requestID, zeroAddress)
	s.Require().NoError(err)
	s.Require().Zero(s.vaultBig(vault, "totalSupply").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalAssets").Sign())
	s.Require().Zero(s.vaultBig(vault, "totalWithdrawalUnbonding").Sign())
}

func (s *KeeperTestSuite) TestStakedBondVaultSlashSyncUpdatesCachedDelegations() {
	s.SetupTest()

	vault := s.deployStakedBondVault()
	validator := s.Network.GetValidators()[0]
	depositAmount := big.NewInt(0).Mul(big.NewInt(3), big.NewInt(1e18))

	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setValidators", []string{validator.OperatorAddress}, []uint16{10_000})
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, depositAmount, "depositNative", s.Keyring.GetAddr(0))
	s.Require().NoError(err)
	_, err = s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "poke", big.NewInt(4))
	s.Require().NoError(err)

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

func (s *KeeperTestSuite) deployStakedBondVault() common.Address {
	admin := s.Keyring.GetAddr(0)
	vault, err := s.Factory.DeployContract(
		s.Keyring.GetPrivKey(0),
		evmtypes.EvmTxArgs{GasLimit: 8_000_000},
		types.ContractDeploymentData{
			Contract:        contracts.StakedBondVaultContract,
			ConstructorArgs: []interface{}{admin, "Staked Bond", "stBOND"},
		},
	)
	s.Require().NoError(err)
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
