package keeper

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

type mockDistributionQuerier struct {
	slashHeightsByValidator map[string]map[uint64]struct{}
	queriedValidators       []string
}

func (m *mockDistributionQuerier) IterateValidatorSlashEventsBetween(_ context.Context, val sdk.ValAddress, startingHeight, endingHeight uint64, handler func(height uint64, event distributiontypes.ValidatorSlashEvent) (stop bool)) {
	if m == nil {
		return
	}
	valAddr := val.String()
	m.queriedValidators = append(m.queriedValidators, valAddr)
	if heights, ok := m.slashHeightsByValidator[valAddr]; ok {
		for h := startingHeight; h <= endingHeight; h++ {
			if _, slashed := heights[h]; slashed {
				handler(h, distributiontypes.ValidatorSlashEvent{
					ValidatorPeriod: h,
					Fraction:        math.LegacyNewDec(1),
				})
				return
			}
		}
	}
}

func TestPreparePreviousBlockSlashedValidators_WritesEmptyAtGenesisHeights(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockHeight(1)
	dq := &mockDistributionQuerier{}
	k.distrKeeper = dq

	require.NoError(t, k.PreparePreviousBlockSlashedValidators(ctx))

	slashed, err := k.getPreviousBlockSlashedValidators(ctx)
	require.NoError(t, err)
	require.Empty(t, slashed)
	require.Empty(t, dq.queriedValidators, "genesis-like heights should not issue slash queries")
}

func TestPreparePreviousBlockSlashedValidators_UsesPreviousHeightAndRelevantCandidates(t *testing.T) {
	ctx, k, _ := newTestKeeper(t)
	ctx = ctx.WithBlockHeight(10)
	k.evmKeeper = &mockEVMKeeper{}

	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	targetA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	targetB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	delegatedOnly := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))
	unrelated := sdk.ValAddress(bytes.Repeat([]byte{5}, 20))

	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	params.MaxTargetValidators = 2
	require.NoError(t, k.SetParams(ctx, params))

	sk := k.stakingKeeper.(*mockStakingKeeper)
	sk.vals = []stakingtypes.Validator{
		{OperatorAddress: targetA.String(), Tokens: math.NewInt(100), DelegatorShares: math.LegacyNewDec(100)},
		{OperatorAddress: targetB.String(), Tokens: math.NewInt(90), DelegatorShares: math.LegacyNewDec(90)},
	}
	sk.delegations = []stakingtypes.Delegation{
		{DelegatorAddress: poolDel.String(), ValidatorAddress: delegatedOnly.String(), Shares: math.LegacyNewDec(10)},
	}

	dq := &mockDistributionQuerier{
		slashHeightsByValidator: map[string]map[uint64]struct{}{
			targetB.String():      {9: {}},
			delegatedOnly.String(): {8: {}},
			unrelated.String():    {9: {}},
		},
	}
	k.distrKeeper = dq

	require.NoError(t, k.PreparePreviousBlockSlashedValidators(ctx))

	slashed, err := k.getPreviousBlockSlashedValidators(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		targetB.String(): {},
	}, slashed)

	require.ElementsMatch(t, []string{delegatedOnly.String(), targetA.String(), targetB.String()}, dq.queriedValidators)
}
