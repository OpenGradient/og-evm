package keeper

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaybeRunCommunityPoolAutomation best-effort executes CommunityPool harvest/stake.
// Assumptions:
// - pool params PoolDelegatorAddress points to the CommunityPool contract account,
// - CommunityPool automationCaller is set to types.ModuleEVMAddress.
// It never returns operational call failures; those are logged and retried next block.
func (k Keeper) MaybeRunCommunityPoolAutomation(ctx sdk.Context) error {
	del, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if del.Empty() || k.evmKeeper == nil {
		return nil
	}

	poolContract := common.BytesToAddress(del.Bytes())
	from := types.ModuleEVMAddress
	// Ensure caller account exists so vm.CallEVM can resolve sequence/nonce.
	// Some chains materialize module accounts lazily; CallEVM requires address-based lookup.
	if k.accountKeeper != nil {
		moduleAcc := sdk.AccAddress(from.Bytes())
		if k.accountKeeper.GetAccount(ctx, moduleAcc) == nil {
			k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, moduleAcc))
		}
	}

	for _, method := range []string{"harvest", "stake"} {
		res, callErr := k.evmKeeper.CallEVM(ctx, types.CommunityPoolABI, from, poolContract, true, nil, method)
		if callErr != nil {
			ctx.Logger().Error("poolrebalancer: community pool automation call failed", "method", method, "contract", poolContract.Hex(), "err", callErr)
			continue
		}
		if res != nil && res.Failed() {
			ctx.Logger().Error("poolrebalancer: community pool automation vm failed", "method", method, "contract", poolContract.Hex(), "vm_error", res.VmError)
		}
	}

	return nil
}
