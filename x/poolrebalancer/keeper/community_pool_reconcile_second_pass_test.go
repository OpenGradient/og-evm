package keeper

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestMaybeReconcileCommunityPoolStakedBucketsSecondPass_NoOpWhenDisabled(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	k.SetCommunityPoolReconcileSecondPassForTesting(false)
	t.Cleanup(func() { k.SetCommunityPoolReconcileSecondPassForTesting(false) })
	mockEVM := &mockEVMKeeper{}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))

	ctx = ctx.WithBlockHeight(19)
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBucketsSecondPass(ctx))
	require.Empty(t, mockEVM.methods)
}

func TestMaybeReconcileCommunityPoolStakedBucketsSecondPass_DelegatesWhenEnabled(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	k.SetCommunityPoolReconcileSecondPassForTesting(true)
	t.Cleanup(func() { k.SetCommunityPoolReconcileSecondPassForTesting(false) })
	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(1)), nil
		},
	}
	k.evmKeeper = mockEVM

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	p := types.DefaultParams()
	p.PoolDelegatorAddress = del.String()
	require.NoError(t, k.SetParams(ctx, p))

	ctx = ctx.WithBlockHeight(19)
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBucketsSecondPass(ctx))
	require.Equal(t, []string{"totalStaked", "reconcileTotalStaked"}, mockEVM.methods)
}
