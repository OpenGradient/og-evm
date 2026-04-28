package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestComputeExpectedBondedPrincipal_SkipsNonBondedValidators(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	bondedVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	unbondingVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	sk := &mockStakingKeeper{
		validatorByAddr: map[string]stakingtypes.Validator{
			bondedVal.String(): {
				OperatorAddress: bondedVal.String(),
				Tokens:          math.NewInt(1000),
				DelegatorShares: math.LegacyNewDec(1000),
				Status:          stakingtypes.Bonded,
			},
			unbondingVal.String(): {
				OperatorAddress: unbondingVal.String(),
				Tokens:          math.NewInt(500),
				DelegatorShares: math.LegacyNewDec(500),
				Status:          stakingtypes.Unbonding,
			},
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: bondedVal.String(), Shares: math.LegacyNewDec(100)},
			{DelegatorAddress: del.String(), ValidatorAddress: unbondingVal.String(), Shares: math.LegacyNewDec(50)},
		},
	}
	ctx, k := newProcessRebalanceKeeper(t, sk)
	sum, err := k.ComputeExpectedBondedPrincipal(ctx, del)
	require.NoError(t, err)
	require.Equal(t, "100", sum.String())
}

func TestGetAllDelegatorDelegations_PaginatesAcrossPages(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	baseVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	delegations := make([]stakingtypes.Delegation, 0, delegatorDelegationPageLimit+1)
	for i := uint64(0); i <= delegatorDelegationPageLimit; i++ {
		valBytes := append([]byte{}, baseVal.Bytes()...)
		valBytes[len(valBytes)-1] = byte(i % 255)
		val := sdk.ValAddress(valBytes)
		delegations = append(delegations, stakingtypes.Delegation{
			DelegatorAddress: del.String(),
			ValidatorAddress: val.String(),
			Shares:           math.LegacyNewDec(1),
		})
	}
	sk := &mockStakingKeeper{delegations: delegations}
	ctx, k := newProcessRebalanceKeeper(t, sk)

	got, err := k.getAllDelegatorDelegations(ctx, del)
	require.NoError(t, err)
	require.Len(t, got, int(delegatorDelegationPageLimit+1))
}
