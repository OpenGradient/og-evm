package keeper

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

const (
	// communityPoolReconcileSweepIntervalBlocks runs a reconcile even when dirty is false (slash drift catch-up).
	communityPoolReconcileSweepIntervalBlocks int64 = 20
)

func (k Keeper) getCommunityPoolReconcileDirty(ctx context.Context) bool {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.CommunityPoolReconcileDirtyKey)
	if err != nil || len(bz) == 0 {
		return false
	}
	return bz[0] != 0
}

func (k Keeper) setCommunityPoolReconcileDirty(ctx context.Context, v bool) error {
	store := k.storeService.OpenKVStore(ctx)
	if v {
		return store.Set(types.CommunityPoolReconcileDirtyKey, []byte{1})
	}
	return store.Set(types.CommunityPoolReconcileDirtyKey, []byte{0})
}

// markCommunityPoolReconcileDirtyIfPoolDelegator sets the reconcile dirty flag when del is the configured pool delegator.
func (k Keeper) markCommunityPoolReconcileDirtyIfPoolDelegator(ctx context.Context, del sdk.AccAddress) error {
	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if poolDel.Empty() || !del.Equals(poolDel) {
		return nil
	}
	return k.setCommunityPoolReconcileDirty(ctx, true)
}

func (k Keeper) unpackCommunityPoolUint256View(methodName string, ret []byte) (math.Int, error) {
	method, ok := types.CommunityPoolABI.Methods[methodName]
	if !ok {
		return math.ZeroInt(), fmt.Errorf("abi method %q not found", methodName)
	}
	if len(ret) == 0 {
		return math.ZeroInt(), errors.New("empty evm return data")
	}
	out, err := method.Outputs.Unpack(ret)
	if err != nil {
		return math.ZeroInt(), err
	}
	if len(out) != 1 {
		return math.ZeroInt(), fmt.Errorf("expected 1 output, got %d", len(out))
	}
	bi, ok := out[0].(*big.Int)
	if !ok || bi == nil {
		return math.ZeroInt(), fmt.Errorf("expected *big.Int, got %T", out[0])
	}
	if bi.Sign() < 0 {
		return math.ZeroInt(), errors.New("negative uint256")
	}
	if bi.Cmp(abi.MaxUint256) > 0 {
		return math.ZeroInt(), errors.New("uint256 view exceeds max uint256")
	}
	return math.NewIntFromBigInt(bi), nil
}

// coerceEVMUint256BigInt returns bi if it fits a Solidity uint256 ABI argument.
func coerceEVMUint256BigInt(bi *big.Int) (*big.Int, error) {
	if bi == nil {
		return nil, errors.New("nil total-staked big.Int")
	}
	if bi.Sign() < 0 {
		return nil, errors.New("total-staked amount is negative")
	}
	if bi.Cmp(abi.MaxUint256) > 0 {
		return nil, fmt.Errorf("total-staked amount exceeds uint256 (bits>256)")
	}
	return bi, nil
}

// communityPoolStakeBucketBigInt coerces total-staked v to a *big.Int valid for Solidity uint256 ABI encoding.
func communityPoolStakeBucketBigInt(v math.Int) (*big.Int, error) {
	if v.IsNil() {
		return nil, errors.New("nil total-staked amount")
	}
	if v.IsNegative() {
		return nil, fmt.Errorf("total-staked amount is negative: %s", v.String())
	}
	return coerceEVMUint256BigInt(v.BigInt())
}

func (k Keeper) callCommunityPoolViewUint256(ctx sdk.Context, poolDel sdk.AccAddress, method string) (math.Int, error) {
	res, err := k.callCommunityPoolEVMWithCommit(ctx, poolDel, false, method)
	if err != nil {
		return math.ZeroInt(), err
	}
	if res != nil && res.Failed() {
		return math.ZeroInt(), fmt.Errorf("vm error: %s", res.VmError)
	}
	if res == nil {
		return math.ZeroInt(), errors.New("nil evm response")
	}
	return k.unpackCommunityPoolUint256View(method, res.Ret)
}

func (k Keeper) tryReadOnChainCommunityPoolTotalStaked(ctx sdk.Context, poolDel sdk.AccAddress) (math.Int, error) {
	return k.callCommunityPoolViewUint256(ctx, poolDel, "totalStaked")
}

// MaybeReconcileCommunityPoolStakedBuckets aligns EVM totalStaked with staking when dirty or on a sweep block.
// Returns errors for tests/metrics; EndBlocker only logs them.
func (k Keeper) MaybeReconcileCommunityPoolStakedBuckets(ctx context.Context) error {
	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if poolDel.Empty() || k.evmKeeper == nil {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	dirty := k.getCommunityPoolReconcileDirty(ctx)
	sweep := communityPoolReconcileSweepIntervalBlocks > 0 &&
		height%communityPoolReconcileSweepIntervalBlocks == 0
	if !dirty && !sweep {
		return nil
	}

	expBonded, err := k.ComputeExpectedBondedPrincipal(ctx, poolDel)
	if err != nil {
		return fmt.Errorf("compute expected community pool total staked: %w", err)
	}

	onBonded, staticErr := k.tryReadOnChainCommunityPoolTotalStaked(sdkCtx, poolDel)
	if staticErr == nil && onBonded.Equal(expBonded) {
		return k.setCommunityPoolReconcileDirty(ctx, false)
	}

	bondedArg, err := communityPoolStakeBucketBigInt(expBonded)
	if err != nil {
		_ = k.setCommunityPoolReconcileDirty(ctx, true) //nolint:errcheck // persist dirty for retry after fix
		return fmt.Errorf("expected bonded principal for EVM: %w", err)
	}
	res, err := k.callCommunityPoolEVMWithCommit(
		sdkCtx,
		poolDel,
		true,
		"reconcileTotalStaked",
		bondedArg,
	)
	if err != nil {
		_ = k.setCommunityPoolReconcileDirty(ctx, true) //nolint:errcheck // best-effort persist dirty for retry
		return fmt.Errorf("reconcileTotalStaked: %w", err)
	}
	if res != nil && res.Failed() {
		_ = k.setCommunityPoolReconcileDirty(ctx, true) //nolint:errcheck
		return fmt.Errorf("reconcileTotalStaked vm error: %s", res.VmError)
	}
	return k.setCommunityPoolReconcileDirty(ctx, false)
}

// MaybeReconcileCommunityPoolStakedBucketsSecondPass runs reconcile again after ProcessRebalance if enabled (tests).
func (k Keeper) MaybeReconcileCommunityPoolStakedBucketsSecondPass(ctx context.Context) error {
	if k.communityPoolReconcileSecondPass == nil || !k.communityPoolReconcileSecondPass.Load() {
		return nil
	}
	return k.MaybeReconcileCommunityPoolStakedBuckets(ctx)
}
