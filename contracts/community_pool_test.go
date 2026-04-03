package contracts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Step 2 (rebalance plan): ensure the committed Hardhat artifact loads and includes the new entrypoint.
func TestLoadCommunityPool_IncludesCreditStakeableFromRebalance(t *testing.T) {
	t.Parallel()
	c, err := LoadCommunityPool()
	require.NoError(t, err)
	require.NotEmpty(t, c.Bin)
	_, ok := c.ABI.Methods["creditStakeableFromRebalance"]
	require.True(t, ok, "artifact ABI should include creditStakeableFromRebalance")
}
