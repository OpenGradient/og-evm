package bridge

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BurnNative burns native tokens from the authorized bridge contract balance.
func (p Precompile) BurnNative(
	ctx sdk.Context,
	contract *vm.Contract,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	caller, err := p.ensureAuthorizedCaller(ctx, contract)
	if err != nil {
		return nil, err
	}

	amount, err := parseAmount(args[0])
	if err != nil {
		return nil, err
	}

	senderAddr := sdk.AccAddress(caller.Bytes())
	if err := p.bridgeKeeper.BurnForBridge(ctx, senderAddr, amount); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}
