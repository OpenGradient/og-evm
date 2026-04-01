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
}

