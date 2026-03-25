package bridge

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	_ "embed"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	cmn "github.com/cosmos/evm/precompiles/common"
	"github.com/cosmos/evm/utils"
	bridgekeeper "github.com/cosmos/evm/x/bridge/keeper"
	bridgetypes "github.com/cosmos/evm/x/bridge/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	MintNativeMethod = "mintNative"
	BurnNativeMethod = "burnNative"
	IsEnabledMethod  = "isEnabled"
)

var _ vm.PrecompiledContract = &Precompile{}

var (
	//go:embed abi.json
	f   []byte
	ABI abi.ABI
)

func init() {
	var err error
	ABI, err = abi.JSON(bytes.NewReader(f))
	if err != nil {
		panic(err)
	}
}

// Precompile defines the bridge precompile.
type Precompile struct {
	cmn.Precompile

	abi.ABI
	bridgeKeeper bridgekeeper.Keeper
}

// NewPrecompile creates a new bridge precompile instance.
func NewPrecompile(bridgeKeeper bridgekeeper.Keeper, bankKeeper cmn.BankKeeper) *Precompile {
	return &Precompile{
		Precompile: cmn.Precompile{
			KvGasConfig:           storetypes.KVGasConfig(),
			TransientKVGasConfig:  storetypes.TransientGasConfig(),
			ContractAddress:       common.HexToAddress(evmtypes.BridgePrecompileAddress),
			BalanceHandlerFactory: cmn.NewBalanceHandlerFactory(bankKeeper),
		},
		ABI:          ABI,
		bridgeKeeper: bridgeKeeper,
	}
}

// RequiredGas returns the minimum gas needed to execute the bridge precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}

	method, err := p.MethodById(input[:4])
	if err != nil {
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method))
}

func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readonly bool) ([]byte, error) {
	return p.RunNativeAction(evm, contract, func(ctx sdk.Context) ([]byte, error) {
		return p.Execute(ctx, contract, readonly)
	})
}

// Execute dispatches the bridge precompile methods defined in the ABI.
func (p Precompile) Execute(ctx sdk.Context, contract *vm.Contract, readOnly bool) ([]byte, error) {
	method, args, err := cmn.SetupABI(p.ABI, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case MintNativeMethod:
		return p.MintNative(ctx, contract, method, args)
	case BurnNativeMethod:
		return p.BurnNative(ctx, contract, method, args)
	case IsEnabledMethod:
		return p.IsEnabled(ctx, method)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}
}

// IsTransaction returns whether the given method mutates bridge state.
func (Precompile) IsTransaction(method *abi.Method) bool {
	switch method.Name {
	case MintNativeMethod, BurnNativeMethod:
		return true
	default:
		return false
	}
}

// IsEnabled returns whether the bridge is currently enabled.
func (p Precompile) IsEnabled(ctx sdk.Context, method *abi.Method) ([]byte, error) {
	return method.Outputs.Pack(p.bridgeKeeper.GetParams(ctx).Enabled)
}

func (p Precompile) ensureAuthorizedCaller(ctx sdk.Context, contract *vm.Contract) (common.Address, error) {
	caller := contract.Caller()
	authorizedContract := p.bridgeKeeper.GetAuthorizedContract(ctx)
	if authorizedContract == "" {
		return common.Address{}, errorsmod.Wrap(bridgetypes.ErrUnauthorizedCaller, "no authorized contract configured")
	}

	authorizedCaller := common.HexToAddress(authorizedContract)
	if caller != authorizedCaller {
		return common.Address{}, errorsmod.Wrapf(
			bridgetypes.ErrUnauthorizedCaller,
			"caller %s is not authorized contract %s",
			caller.Hex(),
			authorizedCaller.Hex(),
		)
	}

	return caller, nil
}

func parseAmount(arg interface{}) (sdkmath.Int, error) {
	amountBig, ok := arg.(*big.Int)
	if !ok || amountBig == nil {
		return sdkmath.Int{}, errorsmod.Wrap(bridgetypes.ErrInvalidAmount, "invalid uint256 amount argument")
	}

	amount, err := utils.SafeNewIntFromBigInt(amountBig)
	if err != nil {
		return sdkmath.Int{}, errorsmod.Wrap(bridgetypes.ErrInvalidAmount, err.Error())
	}

	return amount, nil
}
