package keeper

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"

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

// callCommunityPoolEVMWithCommit calls CommunityPool; commit=false is a static call.
func (k Keeper) callCommunityPoolEVMWithCommit(ctx sdk.Context, poolDel sdk.AccAddress, commit bool, method string, args ...any) (*evmtypes.MsgEthereumTxResponse, error) {
	if k.evmKeeper == nil {
		return nil, errors.New("evm keeper is nil")
	}
	k.ensurePoolRebalancerModuleEVMAccount(ctx)
	poolContract := common.BytesToAddress(poolDel.Bytes())
	return k.evmKeeper.CallEVM(ctx, types.CommunityPoolABI, types.ModuleEVMAddress, poolContract, commit, nil, method, args...)
}

// callCommunityPoolEVM state-changing CommunityPool call.
func (k Keeper) callCommunityPoolEVM(ctx sdk.Context, poolDel sdk.AccAddress, method string, args ...any) (*evmtypes.MsgEthereumTxResponse, error) {
	return k.callCommunityPoolEVMWithCommit(ctx, poolDel, true, method, args...)
}

// MaybeRunCommunityPoolAutomation runs harvest then stake on PoolDelegatorAddress (best-effort; errors logged).
func (k Keeper) MaybeRunCommunityPoolAutomation(ctx sdk.Context) error {
	del, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if del.Empty() || k.evmKeeper == nil {
		return nil
	}

	totalUnits, err := k.callCommunityPoolViewUint256(ctx, del, "totalUnits")
	if err != nil {
		return fmt.Errorf("read community pool totalUnits: %w", err)
	}
	if !totalUnits.IsPositive() {
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
