package communitypool

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	. "github.com/onsi/gomega"

	"github.com/cosmos/evm/precompiles/erc20"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

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
				s.validatorPrefix,
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

func (s *IntegrationTestSuite) execTxExpectSuccess(
	priv cryptotypes.PrivKey,
	txArgs evmtypes.EvmTxArgs,
	callArgs testutiltypes.CallArgs,
) {
	if txArgs.GasLimit == 0 {
		txArgs.GasLimit = 2_000_000
	}
	res, err := s.factory.ExecuteContractCall(priv, txArgs, callArgs)
	Expect(err).To(BeNil(), "expected tx execution success")

	ethRes, err := evmtypes.DecodeTxResponse(res.Data)
	Expect(err).To(BeNil(), "failed to decode ethereum tx response")
	Expect(ethRes.VmError).To(BeEmpty(), "unexpected EVM execution revert")
}

