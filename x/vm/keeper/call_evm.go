package keeper

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cosmos/evm/server/config"
	evmtrace "github.com/cosmos/evm/trace"
	"github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CallEVM performs a smart contract method call using given args.
func (k Keeper) CallEVM(
	ctx sdk.Context,
	abi abi.ABI,
	from, contract common.Address,
	commit bool,
	gasCap *big.Int,
	method string,
	args ...any,
) (_ *types.MsgEthereumTxResponse, err error) {
	ctx, span := ctx.StartSpan(tracer, "CallEVM", trace.WithAttributes(
		attribute.String("from", from.Hex()),
		attribute.String("contract", contract.Hex()),
		attribute.String("method", method),
		attribute.Bool("commit", commit),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()
	data, err := abi.Pack(method, args...)
	if err != nil {
		return nil, errorsmod.Wrap(
			types.ErrABIPack,
			errorsmod.Wrap(err, "failed to create transaction data").Error(),
		)
	}

	resp, err := k.CallEVMWithData(ctx, from, &contract, data, commit, gasCap)
	if err != nil {
		return resp, errorsmod.Wrapf(err, "contract call failed: method '%s', contract '%s'", method, contract)
	}
	return resp, nil
}

// CallEVMWithData performs a smart contract method call using contract data.
func (k Keeper) CallEVMWithData(
	ctx sdk.Context,
	from common.Address,
	contract *common.Address,
	data []byte,
	commit bool,
	gasCap *big.Int,
) (_ *types.MsgEthereumTxResponse, err error) {
	contractAddr := ""
	if contract != nil {
		contractAddr = contract.Hex()
	}
	ctx, span := ctx.StartSpan(tracer, "CallEVMWithData", trace.WithAttributes(
		attribute.String("from", from.Hex()),
		attribute.String("contract", contractAddr),
		attribute.Bool("commit", commit),
		attribute.Int("data_size", len(data)),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()
	nonce, err := k.accountKeeper.GetSequence(ctx, from.Bytes())
	if err != nil {
		return nil, err
	}
	gasLimit, err := evmCallGasLimit(gasCap)
	if err != nil {
		return nil, err
	}

	msg := core.Message{
		From:       from,
		To:         contract,
		Nonce:      nonce,
		Value:      big.NewInt(0),
		GasLimit:   gasLimit,
		GasPrice:   big.NewInt(0),
		GasTipCap:  big.NewInt(0),
		GasFeeCap:  big.NewInt(0),
		Data:       data,
		AccessList: ethtypes.AccessList{},
	}

	res, err := k.ApplyMessage(ctx, msg, nil, commit, true)
	if err != nil {
		return nil, err
	}

	if res.Failed() {
		k.ResetGasMeterAndConsumeGas(ctx, ctx.GasMeter().Limit())
		return res, errorsmod.Wrap(types.ErrVMExecution, res.VmError)
	}

	ctx.GasMeter().ConsumeGas(res.GasUsed, "apply evm message")

	return res, nil
}

// CallEVMSystemWithData performs a bounded system EVM call. State changes are
// committed only when the VM execution succeeds.
func (k Keeper) CallEVMSystemWithData(
	ctx sdk.Context,
	from common.Address,
	contract common.Address,
	data []byte,
	gasLimit uint64,
) (_ *types.MsgEthereumTxResponse, err error) {
	ctx, span := ctx.StartSpan(tracer, "CallEVMSystemWithData", trace.WithAttributes(
		attribute.String("from", from.Hex()),
		attribute.String("contract", contract.Hex()),
		attribute.Int("data_size", len(data)),
		attribute.String("gas_limit", fmt.Sprintf("%d", gasLimit)),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()
	if gasLimit == 0 {
		return nil, fmt.Errorf("system EVM gas limit must be greater than zero")
	}
	cachedCtx, commit := ctx.CacheContext()
	fromAcc := sdk.AccAddress(from.Bytes())
	if !k.accountKeeper.HasAccount(cachedCtx, fromAcc) {
		k.accountKeeper.SetAccount(cachedCtx, k.accountKeeper.NewAccountWithAddress(cachedCtx, fromAcc))
	}
	nonce, err := k.accountKeeper.GetSequence(cachedCtx, fromAcc)
	if err != nil {
		return nil, err
	}
	to := contract
	msg := core.Message{
		From:       from,
		To:         &to,
		Nonce:      nonce,
		Value:      big.NewInt(0),
		GasLimit:   gasLimit,
		GasPrice:   big.NewInt(0),
		GasTipCap:  big.NewInt(0),
		GasFeeCap:  big.NewInt(0),
		Data:       data,
		AccessList: ethtypes.AccessList{},
	}
	res, err := k.ApplyMessage(cachedCtx, msg, nil, true, true)
	if err != nil {
		return res, err
	}
	if res.Failed() {
		return res, errorsmod.Wrap(types.ErrVMExecution, res.VmError)
	}
	commit()
	return res, nil
}

func evmCallGasLimit(gasCap *big.Int) (uint64, error) {
	if gasCap == nil {
		return config.DefaultGasCap, nil
	}
	if gasCap.Sign() <= 0 {
		return 0, fmt.Errorf("gas cap must be greater than zero")
	}
	if !gasCap.IsUint64() {
		return 0, fmt.Errorf("gas cap overflows uint64: %s", gasCap)
	}
	return gasCap.Uint64(), nil
}
