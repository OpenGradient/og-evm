package bridge

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	errorsmod "cosmossdk.io/errors"

	bridgetypes "github.com/cosmos/evm/x/bridge/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MintNative mints native tokens to a recipient. Only the authorized bridge contract may call this.
func (p Precompile) MintNative(
	ctx sdk.Context,
	contract *vm.Contract,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if _, err := p.ensureAuthorizedCaller(ctx, contract); err != nil {
		return nil, err
	}

	recipient, ok := args[0].(common.Address)
	if !ok {
		return nil, errorsmod.Wrap(bridgetypes.ErrInvalidAddress, "invalid recipient argument")
	}

	amount, err := parseAmount(args[1])
	if err != nil {
		return nil, err
	}

	recipientAddr := sdk.AccAddress(recipient.Bytes())
	if err := p.bridgeKeeper.MintForBridge(ctx, recipientAddr, amount); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}
