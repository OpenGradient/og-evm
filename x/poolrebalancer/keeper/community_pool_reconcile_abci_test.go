package keeper

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func packCommunityPoolUint256View(t *testing.T, method string, v *big.Int) []byte {
	t.Helper()
	m, ok := types.CommunityPoolABI.Methods[method]
	require.True(t, ok, "abi method %q", method)
	bz, err := m.Outputs.Pack(v)
	require.NoError(t, err)
	return bz
}

func TestMaybeReconcileCommunityPoolStakedBuckets_UsesReconcileTotalStaked(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	mockSK := k.stakingKeeper.(*mockStakingKeeper)
	mockSK.validatorByAddr = map[string]stakingtypes.Validator{
		val.String(): {
			OperatorAddress: val.String(),
			Tokens:          math.NewInt(1000),
			DelegatorShares: math.LegacyNewDec(1000),
			Status:          stakingtypes.Bonded,
		},
	}
	mockSK.delegations = []stakingtypes.Delegation{
		{DelegatorAddress: poolDel.String(), ValidatorAddress: val.String(), Shares: math.LegacyNewDec(12)},
	}

	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(1)), nil
		},
	}
	k.evmKeeper = mockEVM
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))
	require.Contains(t, mockEVM.methods, "reconcileTotalStaked")
}

func TestMaybeReconcileCommunityPoolStakedBuckets_StaticReadClearsDirty(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	setPoolDelegatorForTest(t, ctx, &k, poolDel)

	mockEVM := &mockEVMKeeper{
		ViewRetEncoder: func(method string) ([]byte, error) {
			return packCommunityPoolUint256View(t, method, big.NewInt(0)), nil
		},
	}
	k.evmKeeper = mockEVM
	require.NoError(t, k.setCommunityPoolReconcileDirty(ctx, true))

	require.NoError(t, k.MaybeReconcileCommunityPoolStakedBuckets(ctx))
	require.False(t, k.getCommunityPoolReconcileDirty(ctx))
	require.Equal(t, []string{"totalStaked"}, mockEVM.methods)
}
