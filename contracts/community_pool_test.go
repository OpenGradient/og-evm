package contracts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Committed CommunityPool artifact must expose credit, reconcile, and bucket view methods used by poolrebalancer.
func TestLoadCommunityPool_IncludesCreditAndReconcileMethods(t *testing.T) {
	t.Parallel()
	c, err := LoadCommunityPool()
	require.NoError(t, err)
	require.NotEmpty(t, c.Bin)
	_, ok := c.ABI.Methods["creditStakeableFromRebalance"]
	require.True(t, ok, "artifact ABI should include creditStakeableFromRebalance")
	_, ok = c.ABI.Methods["reconcileStakedBuckets"]
	require.True(t, ok, "artifact ABI should include reconcileStakedBuckets")
	_, ok = c.ABI.Methods["totalStaked"]
	require.True(t, ok, "artifact ABI should include totalStaked getter")
	_, ok = c.ABI.Methods["pendingRebalanceUnbondReserve"]
	require.True(t, ok, "artifact ABI should include pendingRebalanceUnbondReserve getter")
}
