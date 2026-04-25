package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommunityPoolABI_MethodsPresent(t *testing.T) {
	stakeMethod, ok := CommunityPoolABI.Methods["stake"]
	require.True(t, ok)
	require.Empty(t, stakeMethod.Inputs)

	harvestMethod, ok := CommunityPoolABI.Methods["harvest"]
	require.True(t, ok)
	require.Empty(t, harvestMethod.Inputs)

	creditMethod, ok := CommunityPoolABI.Methods["creditStakeableFromRebalance"]
	require.True(t, ok)
	require.Len(t, creditMethod.Inputs, 1)
	require.Equal(t, "uint256", creditMethod.Inputs[0].Type.String())

	reconcileMethod, ok := CommunityPoolABI.Methods["reconcileStakedBuckets"]
	require.True(t, ok)
	require.Len(t, reconcileMethod.Inputs, 2)
	require.Equal(t, "uint256", reconcileMethod.Inputs[0].Type.String())
	require.Equal(t, "uint256", reconcileMethod.Inputs[1].Type.String())

	totalStakedMethod, ok := CommunityPoolABI.Methods["totalStaked"]
	require.True(t, ok)
	require.Empty(t, totalStakedMethod.Inputs)
	require.Equal(t, "view", totalStakedMethod.StateMutability)

	pendingMethod, ok := CommunityPoolABI.Methods["pendingRebalanceUnbondReserve"]
	require.True(t, ok)
	require.Empty(t, pendingMethod.Inputs)
	require.Equal(t, "view", pendingMethod.StateMutability)

	totalUnitsMethod, ok := CommunityPoolABI.Methods["totalUnits"]
	require.True(t, ok)
	require.Empty(t, totalUnitsMethod.Inputs)
	require.Equal(t, "view", totalUnitsMethod.StateMutability)
}
