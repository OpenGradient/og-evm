package keeper

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ensurePoolRebalancerModuleEVMAccount materializes the module account used as tx sender for CallEVM.
func (k Keeper) ensurePoolRebalancerModuleEVMAccount(ctx sdk.Context) {
	if k.accountKeeper == nil {
		return
	}
	moduleAcc := sdk.AccAddress(types.ModuleEVMAddress.Bytes())
	if k.accountKeeper.GetAccount(ctx, moduleAcc) == nil {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, moduleAcc))
	}
}

// callCommunityPoolEVM invokes a CommunityPool method using the minimal embedded ABI (commit=true).
func (k Keeper) callCommunityPoolEVM(ctx sdk.Context, poolDel sdk.AccAddress, method string, args ...any) (*evmtypes.MsgEthereumTxResponse, error) {
	if k.evmKeeper == nil {
		return nil, errors.New("evm keeper is nil")
	}
	k.ensurePoolRebalancerModuleEVMAccount(ctx)
	poolContract := common.BytesToAddress(poolDel.Bytes())
	return k.evmKeeper.CallEVM(ctx, types.CommunityPoolABI, types.ModuleEVMAddress, poolContract, true, nil, method, args...)
}

// creditCommunityPoolStakeableFromRebalance calls CommunityPool.creditStakeableFromRebalance(amount).
func (k Keeper) creditCommunityPoolStakeableFromRebalance(ctx sdk.Context, poolDel sdk.AccAddress, amount math.Int) error {
	if !amount.IsPositive() {
		return nil
	}
	res, err := k.callCommunityPoolEVM(ctx, poolDel, "creditStakeableFromRebalance", amount.BigInt())
	if err != nil {
		return fmt.Errorf("creditStakeableFromRebalance: %w", err)
	}
	if res != nil && res.Failed() {
		return fmt.Errorf("creditStakeableFromRebalance vm error: %s", res.VmError)
	}
	return nil
}

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

	for _, method := range []string{"harvest", "stake"} {
		res, callErr := k.callCommunityPoolEVM(ctx, del, method)
		if callErr != nil {
			ctx.Logger().Error("poolrebalancer: community pool automation call failed", "method", method, "contract", common.BytesToAddress(del.Bytes()).Hex(), "err", callErr)
			continue
		}
		if res != nil && res.Failed() {
			ctx.Logger().Error("poolrebalancer: community pool automation vm failed", "method", method, "contract", common.BytesToAddress(del.Bytes()).Hex(), "vm_error", res.VmError)
		}
	}

	return nil
}
