package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	cmttypes "github.com/cometbft/cometbft/types"

	evmtrace "github.com/cosmos/evm/trace"
	"github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

var (
	vmMeter        = otel.Meter("evm/x/vm/keeper")
	ethTxCounter   metric.Int64Counter
	ethGasCounter  metric.Float64Counter
	ethGasRatio    metric.Float64Gauge
)

func init() {
	ethTxCounter = evmtrace.MustInt64Counter(vmMeter, "evm.tx.ethereum_tx.total",
		metric.WithDescription("Total number of Ethereum transactions"))
	ethGasCounter = evmtrace.MustFloat64Counter(vmMeter, "evm.tx.ethereum_tx.gas_used.total",
		metric.WithDescription("Total gas used by Ethereum transactions"))
	ethGasRatio = evmtrace.MustFloat64Gauge(vmMeter, "evm.tx.ethereum_tx.gas_ratio",
		metric.WithDescription("Gas limit to gas used ratio"))
}

var _ types.MsgServer = &Keeper{}

// EthereumTx implements the gRPC MsgServer interface. It receives a transaction which is then
// executed (i.e applied) against the go-ethereum EVM. The provided SDK Context is set to the Keeper
// so that it can implements and call the StateDB methods without receiving it as a function
// parameter.
func (k *Keeper) EthereumTx(goCtx context.Context, msg *types.MsgEthereumTx) (_ *types.MsgEthereumTxResponse, err error) {
	goCtx, span := tracer.Start(goCtx, "EthereumTx", trace.WithAttributes(
		attribute.String("tx_hash", msg.Hash().Hex()),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()
	ctx := sdk.UnwrapSDKContext(goCtx)

	tx := msg.AsTransaction()

	txType := fmt.Sprintf("%d", tx.Type())
	execution := "call"
	if tx.To() == nil {
		execution = "create"
	}

	response, err := k.ApplyTransaction(ctx, msg.AsTransaction())
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to apply transaction")
	}

	defer func() {
		attrs := metric.WithAttributes(
			attribute.String("tx_type", txType),
			attribute.String("execution", execution),
		)

		ethTxCounter.Add(goCtx, 1, attrs)

		if response.GasUsed != 0 {
			ethGasCounter.Add(goCtx, float64(response.GasUsed), attrs)

			// Observe which users define a gas limit >> gas used. Note, that
			// gas_limit and gas_used are always > 0
			gasLimit := math.LegacyNewDec(int64(tx.Gas()))                            //#nosec G115 -- int overflow is not a concern here -- tx gas is not going to exceed int64 max value
			gasRatioVal, err := gasLimit.QuoInt64(int64(response.GasUsed)).Float64() //#nosec G115 -- int overflow is not a concern here -- gas used is not going to exceed int64 max value
			if err == nil {
				ethGasRatio.Record(goCtx, gasRatioVal, attrs)
			}
		}
	}()

	attrs := []sdk.Attribute{
		sdk.NewAttribute(sdk.AttributeKeyAmount, tx.Value().String()),
		// add event for ethereum transaction hash format
		sdk.NewAttribute(types.AttributeKeyEthereumTxHash, response.Hash),
		// add event for eth tx gas used, we can't get it from cosmos tx result when it contains multiple eth tx msgs.
		sdk.NewAttribute(types.AttributeKeyTxGasUsed, strconv.FormatUint(response.GasUsed, 10)),
	}

	if len(ctx.TxBytes()) > 0 {
		// add event for CometBFT transaction hash format
		hash := cmttypes.Tx(ctx.TxBytes()).Hash()
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyTxHash, hex.EncodeToString(hash)))
	}

	if to := tx.To(); to != nil {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyRecipient, to.Hex()))
	}

	if response.Failed() {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyEthereumTxFailed, response.VmError))
	}

	// emit events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeEthereumTx,
			attrs...,
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, types.HexAddress(msg.From)),
			sdk.NewAttribute(types.AttributeKeyTxType, txType),
		),
	})

	return response, nil
}

// UpdateParams implements the gRPC MsgServer interface. When an UpdateParams
// proposal passes, it updates the module parameters. The update can only be
// performed if the requested authority is the Cosmos SDK governance module
// account.
func (k *Keeper) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (_ *types.MsgUpdateParamsResponse, err error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx, span := ctx.StartSpan(tracer, "UpdateParams", trace.WithAttributes(
		attribute.String("authority", req.Authority),
		attribute.String("params", req.Params.String()),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()

	if k.authority.String() != req.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority, expected %s, got %s", k.authority.String(), req.Authority)
	}

	if err := k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// RegisterPreinstalls implements the gRPC MsgServer interface. When a RegisterPreinstalls
// proposal passes, it creates the preinstalls. The registration can only be
// performed if the requested authority is the Cosmos SDK governance module
// account.
func (k *Keeper) RegisterPreinstalls(goCtx context.Context, req *types.MsgRegisterPreinstalls) (
	_ *types.MsgRegisterPreinstallsResponse, err error,
) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx, span := ctx.StartSpan(tracer, "RegisterPreinstalls", trace.WithAttributes(
		attribute.String("authority", req.Authority),
	))
	defer func() { evmtrace.EndSpanErr(span, err) }()
	if k.authority.String() != req.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority, expected %s, got %s", k.authority.String(), req.Authority)
	}

	if err := k.AddPreinstalls(ctx, req.Preinstalls); err != nil {
		return nil, err
	}

	return &types.MsgRegisterPreinstallsResponse{}, nil
}
