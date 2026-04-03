package poolrebalancer

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// rebalanceIntegrationStubEVM implements poolrebalancertypes.EVMKeeper for this package only.
// Used with a nil account keeper in test_suite so a prefunded keyring delegator can be the pool
// address without failing the user-pubkey check. These tests target rebalance scheduling, queues,
// and staking—not CommunityPool calldata or real VM execution (see precompiles/communitypool).
type rebalanceIntegrationStubEVM struct{}

func (rebalanceIntegrationStubEVM) CallEVM(
	_ sdk.Context,
	_ abi.ABI,
	_, _ common.Address,
	_ bool,
	_ *big.Int,
	_ string,
	_ ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	return &evmtypes.MsgEthereumTxResponse{}, nil
}

func (rebalanceIntegrationStubEVM) IsContract(sdk.Context, common.Address) bool {
	return true
}

var _ poolrebalancertypes.EVMKeeper = rebalanceIntegrationStubEVM{}
