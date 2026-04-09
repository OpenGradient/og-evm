package keeper

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestCommunityPoolStakeBucketBigInt(t *testing.T) {
	bi, err := communityPoolStakeBucketBigInt(math.ZeroInt())
	require.NoError(t, err)
	require.Equal(t, 0, bi.Sign())

	bi, err = communityPoolStakeBucketBigInt(math.NewInt(42))
	require.NoError(t, err)
	require.Equal(t, "42", bi.String())

	_, err = communityPoolStakeBucketBigInt(math.NewInt(-1))
	require.Error(t, err)

	tooBig := new(big.Int).Add(abi.MaxUint256, big.NewInt(1))
	_, err = coerceEVMUint256BigInt(tooBig)
	require.Error(t, err)

	_, err = coerceEVMUint256BigInt(abi.MaxUint256)
	require.NoError(t, err)
}

func TestMaybeReconcileCommunityPoolStakedBuckets_ParamsLoadError(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(types.ParamsKey, []byte("not-a-valid-proto")))

	err := k.MaybeReconcileCommunityPoolStakedBuckets(ctx)
	require.Error(t, err)
}

func packCommunityPoolUint256View(t *testing.T, method string, v *big.Int) []byte {
	t.Helper()
	m, ok := types.CommunityPoolABI.Methods[method]
	require.True(t, ok, "abi method %q", method)
	bz, err := m.Outputs.Pack(v)
	require.NoError(t, err)
	return bz
}

func TestMaybeReconcileCommunityPoolStakedBuckets_SkipsWithoutDirtyOrSweep(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))

	ctx = ctx.WithBlockHeight(19) // not divisible by 20
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, false))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))
	require.Empty(t, mockEVM.methods, "no EVM when not dirty and not sweep block")
}

func TestMaybeReconcileCommunityPoolStakedBuckets_RunsOnSweepBlock(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))

	ctx = ctx.WithBlockHeight(40)
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, false))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))
	require.Contains(t, mockEVM.methods, "reconcileStakedBuckets", "EVM write on sweep block")
}

func TestMaybeReconcileCommunityPoolStakedBuckets_StaticReadClearsDirtyWithoutReconcileWrite(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(0)), nil
		},
	}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))

	ctx = ctx.WithBlockHeight(19)
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))
	require.Equal(t, []string{"totalStaked", "pendingRebalanceUnbondReserve"}, mockEVM.methods)
	for _, c := range mockEVM.commits {
		require.False(t, c, "only static reads, no commit=true")
	}
}

func TestMaybeReconcileCommunityPoolStakedBuckets_ReconcileUsesExpectedTuple(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockSK := k.stakingKeeper.(*mockStakingKeeper)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	bondedVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	immatureVal := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	bondedV := stakingtypes.Validator{
		OperatorAddress: bondedVal.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	mockSK.validatorByAddr = map[string]stakingtypes.Validator{bondedVal.String(): bondedV}
	mockSK.delegations = []stakingtypes.Delegation{
		{DelegatorAddress: poolDel.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(40)},
	}
	mockSK.delegationByValAddr = map[string]stakingtypes.Delegation{
		bondedVal.String(): mockSK.delegations[0],
	}

	ctx = ctx.WithBlockTime(time.Unix(11_000, 0).UTC())
	immatureCT := ctx.BlockTime().Add(time.Hour)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: immatureVal.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   immatureCT,
	}))
	mockSK.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + immatureVal.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: immatureVal.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{Balance: math.NewInt(55), CompletionTime: immatureCT},
			},
		},
	}

	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			// Force commit=true reconcile path.
			if method == "totalStaked" {
				return packCommunityPoolUint256View(t, method, big.NewInt(999)), nil
			}
			return packCommunityPoolUint256View(t, method, big.NewInt(999)), nil
		},
	}
	k.evmKeeper = mockEVM

	p := types.DefaultParams()
	p.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, p))
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))
	ctx = ctx.WithBlockHeight(21)

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))

	var rec *struct {
		bonded, pending *big.Int
	}
	for i, m := range mockEVM.methods {
		if m != "reconcileStakedBuckets" {
			continue
		}
		require.Len(t, mockEVM.args[i], 2)
		rec = &struct {
			bonded, pending *big.Int
		}{
			bonded:  mockEVM.args[i][0].(*big.Int),
			pending: mockEVM.args[i][1].(*big.Int),
		}
		break
	}
	require.NotNil(t, rec, "reconcileStakedBuckets should be called")
	require.Equal(t, "40", rec.bonded.String())
	require.Equal(t, "55", rec.pending.String())
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))
}

func TestMaybeReconcileCommunityPoolStakedBuckets_VMFailureLeavesDirty(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(12345)), nil
		},
		failedVM: map[string]string{"reconcileStakedBuckets": "execution reverted"},
	}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))
	ctx = ctx.WithBlockHeight(19)

	err := k.MaybeReconcileCommunityPoolStakedBuckets(ctx)
	require.Error(t, err)
	require.True(t, k.getCommunityPoolReconcileDirty(ctx))
}

func TestBeginTrackedUndelegation_poolDelegator_setsReconcileDirty(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(12_000, 0).UTC())
	sk := k.stakingKeeper.(*mockStakingKeeper)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	sk.validatorByAddr = map[string]stakingtypes.Validator{
		val.String(): {
			OperatorAddress: val.String(),
			Tokens:          math.NewInt(1000),
			DelegatorShares: math.LegacyNewDec(1000),
			Status:          stakingtypes.Bonded,
		},
	}
	del := stakingtypes.Delegation{
		DelegatorAddress: poolDel.String(), ValidatorAddress: val.String(), Shares: math.LegacyNewDec(80),
	}
	sk.delegations = []stakingtypes.Delegation{del}
	sk.delegationByValAddr = map[string]stakingtypes.Delegation{val.String(): del}

	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM
	p := types.DefaultParams()
	p.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, p))
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, false))

	_, _, err := k.BeginTrackedUndelegation(ctx, poolDel, val, sdk.NewCoin("stake", math.NewInt(15)))
	require.NoError(t, err)
	require.True(t, k.getCommunityPoolReconcileDirty(ctx))
}

func TestBeginTrackedRedelegation_poolDelegator_setsReconcileDirty(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(13_000, 0).UTC())
	sk := k.stakingKeeper.(*mockStakingKeeper)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	srcV := stakingtypes.Validator{
		OperatorAddress: srcVal.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	dstV := stakingtypes.Validator{
		OperatorAddress: dstVal.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	sk.validatorByAddr = map[string]stakingtypes.Validator{
		srcVal.String(): srcV,
		dstVal.String(): dstV,
	}
	delegation := stakingtypes.Delegation{
		DelegatorAddress: poolDel.String(), ValidatorAddress: srcVal.String(), Shares: math.LegacyNewDec(60),
	}
	sk.delegations = []stakingtypes.Delegation{delegation}
	sk.delegationByValAddr = map[string]stakingtypes.Delegation{srcVal.String(): delegation}

	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM
	p := types.DefaultParams()
	p.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, p))
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, false))

	_, err := k.BeginTrackedRedelegation(ctx, poolDel, srcVal, dstVal, sdk.NewCoin("stake", math.NewInt(8)))
	require.NoError(t, err)
	require.True(t, k.getCommunityPoolReconcileDirty(ctx))
}

func TestMaybeReconcile_afterBeginTrackedUndelegation_reconcileArgsMatchExpectedBuckets(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	blockTime := time.Unix(14_000, 0).UTC()
	ctx = ctx.WithBlockTime(blockTime)
	ctx = ctx.WithBlockHeight(21)

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	sk := k.stakingKeeper.(*mockStakingKeeper)
	v := stakingtypes.Validator{
		OperatorAddress: val.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}
	sk.validatorByAddr = map[string]stakingtypes.Validator{val.String(): v}
	del0 := stakingtypes.Delegation{
		DelegatorAddress: poolDel.String(), ValidatorAddress: val.String(), Shares: math.LegacyNewDec(100),
	}
	sk.delegations = []stakingtypes.Delegation{del0}
	sk.delegationByValAddr = map[string]stakingtypes.Delegation{val.String(): del0}

	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(1)), nil
		},
	}
	k.evmKeeper = mockEVM

	p := types.DefaultParams()
	p.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, p))
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, false))

	unbond := sdk.NewCoin("stake", math.NewInt(25))
	ct, amt, err := k.BeginTrackedUndelegation(ctx, poolDel, val, unbond)
	require.NoError(t, err)
	require.True(t, k.getCommunityPoolReconcileDirty(ctx))

	shares, err := v.SharesFromTokens(amt)
	require.NoError(t, err)
	del1 := stakingtypes.Delegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Shares:           del0.Shares.Sub(shares),
	}
	sk.delegations = []stakingtypes.Delegation{del1}
	sk.delegationByValAddr = map[string]stakingtypes.Delegation{val.String(): del1}

	sk.ubdByDelVal = map[string]stakingtypes.UnbondingDelegation{
		poolDel.String() + "|" + val.String(): {
			DelegatorAddress: poolDel.String(),
			ValidatorAddress: val.String(),
			Entries: []stakingtypes.UnbondingDelegationEntry{
				{Balance: amt, CompletionTime: ct},
			},
		},
	}

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))

	bondedExp, pendingExp, err := k.ComputeExpectedCommunityPoolStakedBuckets(ctx, poolDel, blockTime)
	require.NoError(t, err)

	var gotB, gotP *big.Int
	for i, m := range mockEVM.methods {
		if m == "reconcileStakedBuckets" {
			gotB = mockEVM.args[i][0].(*big.Int)
			gotP = mockEVM.args[i][1].(*big.Int)
			break
		}
	}
	require.NotNil(t, gotB)
	require.Equal(t, bondedExp.String(), gotB.String())
	require.Equal(t, pendingExp.String(), gotP.String())
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))
}
