package types

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper defines the subset of staking keeper methods used by poolrebalancer.
type StakingKeeper interface {
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
	GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	GetDelegation(ctx context.Context, delegatorAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error)
	BeginRedelegation(ctx context.Context, delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (completionTime time.Time, err error)
	Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (completionTime time.Time, amount sdkmath.Int, err error)
	UnbondingTime(ctx context.Context) (time.Duration, error)
	BondDenom(ctx context.Context) (string, error)
}

// EVMKeeper defines the subset of vm keeper methods used by poolrebalancer.
type EVMKeeper interface {
	CallEVM(
		ctx sdk.Context,
		abi abi.ABI,
		from, contract common.Address,
		commit bool,
		gasCap *big.Int,
		method string,
		args ...any,
	) (*evmtypes.MsgEthereumTxResponse, error)
	// IsContract reports whether the address holds non-delegated EVM bytecode (see x/vm/keeper.IsContract).
	IsContract(ctx sdk.Context, address common.Address) bool
}

// AccountKeeper defines the subset of auth keeper methods used by poolrebalancer.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	SetAccount(ctx context.Context, acc sdk.AccountI)
	NewAccountWithAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}
