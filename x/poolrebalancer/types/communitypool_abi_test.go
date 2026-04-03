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
}

