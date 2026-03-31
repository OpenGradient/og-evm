package communitypool

import (
	"bytes"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	. "github.com/onsi/gomega"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/evm/precompiles/erc20"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

type withdrawRequestView struct {
	Owner        common.Address
	AmountOut    *big.Int
	Maturity     uint64
	ReserveMoved bool
	Claimed      bool
}

// deployCommunityPool deploys CommunityPool with deterministic defaults used in tests.
func (s *IntegrationTestSuite) deployCommunityPool(
	ownerIdx int,
	maxRetrieve uint32,
	maxValidators uint32,
	minStakeAmount *big.Int,
) common.Address {
	owner := s.keyring.GetKey(ownerIdx)
	addr, err := s.factory.DeployContract(
		owner.Priv,
		evmtypes.EvmTxArgs{},
		testutiltypes.ContractDeploymentData{
			Contract: s.communityPoolContract,
			ConstructorArgs: []interface{}{
				s.bondTokenAddr,
				maxRetrieve,
				maxValidators,
				minStakeAmount,
				owner.Addr,
			},
		},
	)
	Expect(err).To(BeNil(), "failed to deploy CommunityPool")
	Expect(s.network.NextBlock()).To(BeNil(), "failed to commit deployment block")
	return addr
}

func buildCallArgs(contract evmtypes.CompiledContract, method string, args ...interface{}) testutiltypes.CallArgs {
	return testutiltypes.CallArgs{
		ContractABI: contract.ABI,
		MethodName:  method,
		Args:        args,
	}
}

func buildTxArgs(contractAddr common.Address) evmtypes.EvmTxArgs {
	return evmtypes.EvmTxArgs{
		To: &contractAddr,
	}
}

func (s *IntegrationTestSuite) approveBondToken(
	ownerIdx int,
	spender common.Address,
	amount *big.Int,
) {
	owner := s.keyring.GetKey(ownerIdx)
	txArgs := buildTxArgs(s.bondTokenAddr)
	callArgs := testutiltypes.CallArgs{
		ContractABI: s.bondTokenPC.ABI,
		MethodName:  erc20.ApproveMethod,
		Args:        []interface{}{spender, amount},
	}

	s.execTxExpectSuccess(owner.Priv, txArgs, callArgs)
	Expect(s.network.NextBlock()).To(BeNil(), "failed to commit approve tx")
}

func (s *IntegrationTestSuite) queryPoolUint(
	callerIdx int,
	contractAddr common.Address,
	method string,
	args ...interface{},
) *big.Int {
	_ = callerIdx
	txArgs := buildTxArgs(contractAddr)
	callArgs := buildCallArgs(s.communityPoolContract, method, args...)

	ethRes, err := s.factory.QueryContract(txArgs, callArgs, 0)
	Expect(err).To(BeNil(), "query call failed")

	out, err := s.communityPoolContract.ABI.Unpack(method, ethRes.Ret)
	Expect(err).To(BeNil(), "failed to unpack query output")
	Expect(out).ToNot(BeEmpty(), "empty query output")

	switch value := out[0].(type) {
	case *big.Int:
		return value
	case uint8:
		return new(big.Int).SetUint64(uint64(value))
	case uint16:
		return new(big.Int).SetUint64(uint64(value))
	case uint32:
		return new(big.Int).SetUint64(uint64(value))
	case uint64:
		return new(big.Int).SetUint64(value)
	default:
		Expect(false).To(BeTrue(), "unexpected query output type")
		return nil
	}
}

func (s *IntegrationTestSuite) queryPoolAddress(
	contractAddr common.Address,
	method string,
	args ...interface{},
) common.Address {
	txArgs := buildTxArgs(contractAddr)
	callArgs := buildCallArgs(s.communityPoolContract, method, args...)

	ethRes, err := s.factory.QueryContract(txArgs, callArgs, 0)
	Expect(err).To(BeNil(), "query call failed")

	out, err := s.communityPoolContract.ABI.Unpack(method, ethRes.Ret)
	Expect(err).To(BeNil(), "failed to unpack query output")
	Expect(out).To(HaveLen(1), "unexpected query output length")

	addr, ok := out[0].(common.Address)
	Expect(ok).To(BeTrue(), "unexpected query output type")
	return addr
}

func (s *IntegrationTestSuite) execTxExpectSuccess(
	priv cryptotypes.PrivKey,
	txArgs evmtypes.EvmTxArgs,
	callArgs testutiltypes.CallArgs,
) {
	ethRes := s.execTxAndGetEthResponse(priv, txArgs, callArgs)
	Expect(ethRes.VmError).To(BeEmpty(), "unexpected EVM execution revert")
}

func (s *IntegrationTestSuite) execTxAndGetEthResponse(
	priv cryptotypes.PrivKey,
	txArgs evmtypes.EvmTxArgs,
	callArgs testutiltypes.CallArgs,
) *evmtypes.MsgEthereumTxResponse {
	if txArgs.GasLimit == 0 {
		txArgs.GasLimit = 2_000_000
	}
	res, err := s.factory.ExecuteContractCall(priv, txArgs, callArgs)
	Expect(err).To(BeNil(), "expected tx execution success")

	ethRes, err := evmtypes.DecodeTxResponse(res.Data)
	Expect(err).To(BeNil(), "failed to decode ethereum tx response")
	return ethRes
}

func (s *IntegrationTestSuite) findEventLog(
	ethRes *evmtypes.MsgEthereumTxResponse,
	emitter common.Address,
	event abi.Event,
) *evmtypes.Log {
	for _, lg := range ethRes.Logs {
		if !strings.EqualFold(lg.Address, emitter.Hex()) {
			continue
		}
		if len(lg.Topics) == 0 {
			continue
		}
		if strings.EqualFold(lg.Topics[0], event.ID.Hex()) {
			return lg
		}
	}
	return nil
}

func (s *IntegrationTestSuite) execTxExpectCustomError(
	priv cryptotypes.PrivKey,
	txArgs evmtypes.EvmTxArgs,
	callArgs testutiltypes.CallArgs,
	errorSignature string,
) {
	if txArgs.GasLimit == 0 {
		txArgs.GasLimit = 2_000_000
	}
	res, err := s.factory.ExecuteContractCall(priv, txArgs, callArgs)
	Expect(err).To(BeNil(), "expected tx execution to return response for revert checks")

	ethRes, err := evmtypes.DecodeTxResponse(res.Data)
	Expect(err).To(BeNil(), "failed to decode ethereum tx response")
	Expect(ethRes.VmError).To(ContainSubstring(vm.ErrExecutionReverted.Error()))
	Expect(len(ethRes.Ret)).To(BeNumerically(">=", 4), "revert payload too short for custom error selector")

	expectedSelector := crypto.Keccak256([]byte(errorSignature))[:4]
	Expect(bytes.Equal(ethRes.Ret[:4], expectedSelector)).
		To(BeTrue(), "expected custom error %s (selector %x), got selector %x", errorSignature, expectedSelector, ethRes.Ret[:4])
}

func (s *IntegrationTestSuite) queryBondTokenBalance(addr common.Address) *big.Int {
	ethRes, err := s.factory.QueryContract(
		buildTxArgs(s.bondTokenAddr),
		testutiltypes.CallArgs{
			ContractABI: s.bondTokenPC.ABI,
			MethodName:  erc20.BalanceOfMethod,
			Args:        []interface{}{addr},
		},
		0,
	)
	Expect(err).To(BeNil(), "failed querying bond token balance")

	out, err := s.bondTokenPC.ABI.Unpack(erc20.BalanceOfMethod, ethRes.Ret)
	Expect(err).To(BeNil(), "failed to unpack bond token balance")
	Expect(out).To(HaveLen(1))
	bal, ok := out[0].(*big.Int)
	Expect(ok).To(BeTrue(), "unexpected balance output type")
	return bal
}

func (s *IntegrationTestSuite) queryWithdrawRequest(contractAddr common.Address, requestID *big.Int) withdrawRequestView {
	ethRes, err := s.factory.QueryContract(
		buildTxArgs(contractAddr),
		buildCallArgs(s.communityPoolContract, "withdrawRequests", requestID),
		0,
	)
	Expect(err).To(BeNil(), "failed querying withdraw request")

	out, err := s.communityPoolContract.ABI.Unpack("withdrawRequests", ethRes.Ret)
	Expect(err).To(BeNil(), "failed to unpack withdraw request")
	Expect(out).To(HaveLen(5))

	owner, ok := out[0].(common.Address)
	Expect(ok).To(BeTrue(), "unexpected owner type")
	amountOut, ok := out[1].(*big.Int)
	Expect(ok).To(BeTrue(), "unexpected amountOut type")

	var maturity uint64
	switch t := out[2].(type) {
	case uint64:
		maturity = t
	case *big.Int:
		maturity = t.Uint64()
	default:
		Expect(false).To(BeTrue(), "unexpected maturity type")
	}

	reserveMoved, ok := out[3].(bool)
	Expect(ok).To(BeTrue(), "unexpected reserveMoved type")
	claimed, ok := out[4].(bool)
	Expect(ok).To(BeTrue(), "unexpected claimed type")

	return withdrawRequestView{
		Owner:        owner,
		AmountOut:    amountOut,
		Maturity:     maturity,
		ReserveMoved: reserveMoved,
		Claimed:      claimed,
	}
}

func (s *IntegrationTestSuite) advanceToMaturity(maturity uint64) {
	now := uint64(s.network.GetContext().BlockTime().Unix())
	if maturity <= now {
		return
	}
	delta := time.Duration(maturity-now+1) * time.Second
	Expect(s.network.NextBlockAfter(delta)).To(BeNil(), "failed to advance block time to maturity")
}

func (s *IntegrationTestSuite) assertPoolInvariants(poolAddr common.Address) {
	liquid := s.queryPoolUint(0, poolAddr, "liquidBalance")
	rewardReserve := s.queryPoolUint(0, poolAddr, "rewardReserve")
	maturedReserve := s.queryPoolUint(0, poolAddr, "maturedWithdrawReserve")
	pendingReserve := s.queryPoolUint(0, poolAddr, "pendingWithdrawReserve")
	ledger := s.queryPoolUint(0, poolAddr, "stakeablePrincipalLedger")
	commitments := s.queryPoolUint(0, poolAddr, "totalWithdrawCommitments")

	// rewardReserve <= liquidBalance
	Expect(rewardReserve.Cmp(liquid)).To(BeNumerically("<=", 0))

	// rewardReserve + maturedWithdrawReserve <= liquidBalance
	reserved := new(big.Int).Add(new(big.Int).Set(rewardReserve), maturedReserve)
	Expect(reserved.Cmp(liquid)).To(BeNumerically("<=", 0))

	// stakeablePrincipalLedger + rewardReserve + maturedWithdrawReserve <= liquidBalance
	accounted := new(big.Int).Add(new(big.Int).Set(ledger), rewardReserve)
	accounted.Add(accounted, maturedReserve)
	Expect(accounted.Cmp(liquid)).To(BeNumerically("<=", 0))

	// totalWithdrawCommitments == pendingWithdrawReserve + maturedWithdrawReserve
	expectedCommitments := new(big.Int).Add(new(big.Int).Set(pendingReserve), maturedReserve)
	Expect(commitments.String()).To(Equal(expectedCommitments.String()))
}
