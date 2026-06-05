package keeper

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	EventTypeEVMScheduler      = "evm_scheduler"
	AttributeKeyTargetContract = "target_contract"
	AttributeKeySuccess        = "success"
	AttributeKeyGasUsed        = "gas_used"
	AttributeKeyVMError        = "vm_error"
	AttributeKeyError          = "error"
)

var pokeSelector = crypto.Keccak256([]byte("poke(uint256)"))[:4]

func shouldRunScheduler(ctx sdk.Context, params types.EVMSchedulerParams) bool {
	if !params.Enabled || params.TargetContract == "" || params.CadenceBlocks == 0 {
		return false
	}
	if ctx.BlockHeight() < 0 {
		return false
	}
	return uint64(ctx.BlockHeight())%params.CadenceBlocks == 0 //nolint:gosec // non-negative checked above
}

// RunScheduler performs the configured bounded EndBlock EVM call. It never
// returns an error because scheduler failures must not halt consensus.
func (k Keeper) RunScheduler(ctx sdk.Context) {
	targetContract := ""
	defer func() {
		if r := recover(); r != nil {
			errText := fmt.Sprintf("panic: %v", r)
			k.Logger(ctx).Error("evm scheduler panicked", "target", targetContract, "err", errText)
			k.emitSchedulerEvent(ctx, targetContract, false, 0, "", errText)
		}
	}()

	params := k.GetParams(ctx).Scheduler
	targetContract = params.TargetContract
	if !shouldRunScheduler(ctx, params) {
		return
	}

	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
	if moduleAddr == nil {
		k.emitSchedulerEvent(ctx, params.TargetContract, false, 0, "", "evm module account not found")
		return
	}

	target := common.HexToAddress(params.TargetContract)
	res, err := k.CallEVMSystemWithData(
		ctx,
		common.BytesToAddress(moduleAddr.Bytes()),
		target,
		pokeCalldata(params.MaxOps),
		params.GasCap,
	)
	if err != nil {
		vmErr := ""
		gasUsed := uint64(0)
		if res != nil {
			vmErr = res.VmError
			gasUsed = res.GasUsed
		}
		k.Logger(ctx).Error("evm scheduler call failed", "target", params.TargetContract, "err", err)
		k.emitSchedulerEvent(ctx, params.TargetContract, false, gasUsed, vmErr, err.Error())
		return
	}
	k.emitSchedulerEvent(ctx, params.TargetContract, true, res.GasUsed, "", "")
}

func pokeCalldata(maxOps uint32) []byte {
	data := make([]byte, 4, 36)
	copy(data, pokeSelector)
	return append(data, common.LeftPadBytes(new(big.Int).SetUint64(uint64(maxOps)).Bytes(), 32)...)
}

func (k Keeper) emitSchedulerEvent(ctx sdk.Context, target string, success bool, gasUsed uint64, vmErr, errText string) {
	attrs := []sdk.Attribute{
		sdk.NewAttribute(AttributeKeyTargetContract, target),
		sdk.NewAttribute(AttributeKeySuccess, fmt.Sprintf("%t", success)),
		sdk.NewAttribute(AttributeKeyGasUsed, fmt.Sprintf("%d", gasUsed)),
	}
	if vmErr != "" {
		attrs = append(attrs, sdk.NewAttribute(AttributeKeyVMError, vmErr))
	}
	if errText != "" {
		attrs = append(attrs, sdk.NewAttribute(AttributeKeyError, errText))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeEVMScheduler, attrs...))
}
