package contracts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Committed CommunityPool artifact must expose reconcile and view methods used by poolrebalancer.
func TestLoadCommunityPool_IncludesReconcileMethods(t *testing.T) {
	t.Parallel()
	c, err := LoadCommunityPool()
	require.NoError(t, err)
	require.NotEmpty(t, c.Bin)
	_, ok := c.ABI.Methods["reconcileTotalStaked"]
	require.True(t, ok, "artifact ABI should include reconcileTotalStaked")
	_, ok = c.ABI.Methods["totalStaked"]
	require.True(t, ok, "artifact ABI should include totalStaked getter")
	_, ok = c.ABI.Methods["creditStakeableFromRebalance"]
	require.False(t, ok, "artifact ABI should not include creditStakeableFromRebalance")
	_, ok = c.ABI.Methods["reconcileStakedBuckets"]
	require.False(t, ok, "artifact ABI should not include reconcileStakedBuckets")
	_, ok = c.ABI.Methods["pendingRebalanceUnbondReserve"]
	require.False(t, ok, "artifact ABI should not include pendingRebalanceUnbondReserve getter")
}
