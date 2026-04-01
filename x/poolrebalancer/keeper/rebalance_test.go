package keeper_test

import (
	"bytes"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// testKeeperWithParams creates a keeper backed by an in-memory store and seeds its params.
// The staking keeper is a zero value and must not be used by these unit tests.
func testKeeperWithParams(t *testing.T, rebalanceThresholdBP, maxMovePerOp string) (sdk.Context, keeper.Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	stakingKeeper := &stakingkeeper.Keeper{} // zero value; do not call staking methods
	k := keeper.NewKeeper(cdc, storeService, stakingKeeper, sdk.AccAddress(bytes.Repeat([]byte{9}, 20)), nil, nil)

	bp, err := strconv.ParseUint(rebalanceThresholdBP, 10, 32)
	require.NoError(t, err)

	params := types.DefaultParams()
	params.RebalanceThresholdBp = uint32(bp)
	amt, ok := math.NewIntFromString(maxMovePerOp)
	require.True(t, ok, "invalid maxMovePerOp %q", maxMovePerOp)
	params.MaxMovePerOp = amt
	require.NoError(t, k.SetParams(ctx, params))

	return ctx, k
}

// threeValAddrs returns three deterministic validator addresses (for EqualWeightTarget tests).
func threeValAddrs() []sdk.ValAddress {
	return []sdk.ValAddress{
		sdk.ValAddress(bytes.Repeat([]byte{1}, 20)),
		sdk.ValAddress(bytes.Repeat([]byte{2}, 20)),
		sdk.ValAddress(bytes.Repeat([]byte{3}, 20)),
	}
}

// ---------------------------------------------------------------------------
// 3.1 EqualWeightTarget
// ---------------------------------------------------------------------------

// TestEqualWeightTarget_HappyPath: totalStake=1000, n=3; expect 334, 333, 333 (remainder 1 to first).
func TestEqualWeightTarget_HappyPath(t *testing.T) {
	k := keeper.Keeper{} // zero value; method does not use store or staking
	totalStake := math.NewInt(1000)
	vals := threeValAddrs()
	require.Len(t, vals, 3)

	out, err := k.EqualWeightTarget(totalStake, vals)
	require.NoError(t, err)
	require.Len(t, out, 3)

	// 1000 / 3 = 333, remainder 1 → first validator gets 334, others 333
	sum := math.ZeroInt()
	for _, v := range vals {
		key := v.String()
		amt, ok := out[key]
		require.True(t, ok, "missing key %s", key)
		sum = sum.Add(amt)
	}
	require.True(t, sum.Equal(totalStake), "sum %s != totalStake %s", sum, totalStake)

	require.True(t, out[vals[0].String()].Equal(math.NewInt(334)), "first validator should get 334")
	require.True(t, out[vals[1].String()].Equal(math.NewInt(333)))
	require.True(t, out[vals[2].String()].Equal(math.NewInt(333)))
}

// TestEqualWeightTarget_RemainderZero: totalStake=999, n=3; expect exactly 333 each.
func TestEqualWeightTarget_RemainderZero(t *testing.T) {
	k := keeper.Keeper{}
	totalStake := math.NewInt(999)
	vals := threeValAddrs()

	out, err := k.EqualWeightTarget(totalStake, vals)
	require.NoError(t, err)
	require.Len(t, out, 3)

	for _, v := range vals {
		require.True(t, out[v.String()].Equal(math.NewInt(333)), "validator %s should get 333", v.String())
	}
	sum := math.ZeroInt()
	for _, amt := range out {
		sum = sum.Add(amt)
	}
	require.True(t, sum.Equal(totalStake))
}

// TestEqualWeightTarget_SingleValidator: n=1; that validator gets full totalStake.
func TestEqualWeightTarget_SingleValidator(t *testing.T) {
	k := keeper.Keeper{}
	totalStake := math.NewInt(500)
	vals := []sdk.ValAddress{sdk.ValAddress(bytes.Repeat([]byte{1}, 20))}

	out, err := k.EqualWeightTarget(totalStake, vals)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.True(t, out[vals[0].String()].Equal(totalStake))
}

// TestEqualWeightTarget_Errors: n=0 or totalStake negative returns error.
func TestEqualWeightTarget_Errors(t *testing.T) {
	k := keeper.Keeper{}
	vals := threeValAddrs()

	_, err := k.EqualWeightTarget(math.NewInt(1000), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")

	_, err = k.EqualWeightTarget(math.NewInt(1000), []sdk.ValAddress{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")

	_, err = k.EqualWeightTarget(math.NewInt(-1), vals)
	require.Error(t, err)
	require.Contains(t, err.Error(), "negative")
}

// TestTestKeeperWithParams verifies that testKeeperWithParams sets params and they can be read back.
func TestTestKeeperWithParams(t *testing.T) {
	ctx, k := testKeeperWithParams(t, "50", "100")
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(50), params.RebalanceThresholdBp)
	require.True(t, params.MaxMovePerOp.Equal(math.NewInt(100)))
}

// ---------------------------------------------------------------------------
// 3.2 PickBestRedelegation
// ---------------------------------------------------------------------------

// helper to build sorted keys from deltas
func sortedKeys(deltas map[string]math.Int) []string {
	keys := make([]string, 0, len(deltas))
	for k := range deltas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestPickBestRedelegation_SinglePair verifies a basic src/dst selection without caps.
func TestPickBestRedelegation_SinglePair(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src": math.NewInt(-100),
		"dst": math.NewInt(50),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.ZeroInt() // no cap

	src, dst, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	require.Equal(t, "src", src)
	require.Equal(t, "dst", dst)
	require.True(t, amt.Equal(math.NewInt(50)))
}

// TestPickBestRedelegation_MaxMoveCap ensures MaxMovePerOp cap is applied.
func TestPickBestRedelegation_MaxMoveCap(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src": math.NewInt(-100),
		"dst": math.NewInt(50),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.NewInt(10)

	_, _, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	require.True(t, amt.Equal(math.NewInt(10)))
}

// TestPickBestRedelegation_MaxMoveZeroMeansNoCap verifies maxMove=0 does not cap moves.
func TestPickBestRedelegation_MaxMoveZeroMeansNoCap(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src": math.NewInt(-30),
		"dst": math.NewInt(100),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.ZeroInt()

	_, _, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	// min(|-30|, 100) = 30 since there is no cap
	require.True(t, amt.Equal(math.NewInt(30)))
}

// TestPickBestRedelegation_BlockedPair skips blocked src/dst pairs.
func TestPickBestRedelegation_BlockedPair(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src": math.NewInt(-40),
		"dst": math.NewInt(40),
	}
	keys := sortedKeys(deltas)

	// Block the only possible pair.
	blocked := map[string]map[string]struct{}{
		"src": {"dst": {}},
	}
	maxMove := math.ZeroInt()

	_, _, _, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.False(t, ok)
}

// TestPickBestRedelegation_TieBreak ensures lexicographic tie-break on (src,dst).
func TestPickBestRedelegation_TieBreak(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"a": math.NewInt(-10),
		"b": math.NewInt(-10),
		"c": math.NewInt(10),
		"d": math.NewInt(10),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.ZeroInt()

	src, dst, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	// All valid moves have amount 10; lexicographically smallest (src,dst) is ("a","c").
	require.Equal(t, "a", src)
	require.Equal(t, "c", dst)
	require.True(t, amt.Equal(math.NewInt(10)))
}

// TestPickBestRedelegation_CappedTiePrefersLargerDstDeficit verifies that when capped move
// amounts tie, destination with larger deficit is selected.
func TestPickBestRedelegation_CappedTiePrefersLargerDstDeficit(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src":  math.NewInt(-100),
		"dstA": math.NewInt(1000),
		"dstB": math.NewInt(500),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.NewInt(10) // both candidates cap to move=10

	src, dst, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	require.Equal(t, "src", src)
	require.Equal(t, "dstA", dst, "larger deficit destination should win tie under cap")
	require.True(t, amt.Equal(math.NewInt(10)))
}

// TestPickBestRedelegation_NoSourceOrDest tests cases where no move is possible.
func TestPickBestRedelegation_NoSourceOrDest(t *testing.T) {
	k := keeper.Keeper{}

	// All zero deltas.
	deltasAllZero := map[string]math.Int{
		"a": math.ZeroInt(),
		"b": math.ZeroInt(),
	}
	keys := sortedKeys(deltasAllZero)
	blocked := make(map[string]map[string]struct{})

	_, _, _, ok := k.PickBestRedelegation(deltasAllZero, keys, blocked, math.ZeroInt())
	require.False(t, ok)

	// All positive deltas (no overweight src).
	deltasAllPositive := map[string]math.Int{
		"a": math.NewInt(10),
		"b": math.NewInt(5),
	}
	keys = sortedKeys(deltasAllPositive)
	_, _, _, ok = k.PickBestRedelegation(deltasAllPositive, keys, blocked, math.ZeroInt())
	require.False(t, ok)

	// All negative deltas (no underweight dst).
	deltasAllNegative := map[string]math.Int{
		"a": math.NewInt(-10),
		"b": math.NewInt(-5),
	}
	keys = sortedKeys(deltasAllNegative)
	_, _, _, ok = k.PickBestRedelegation(deltasAllNegative, keys, blocked, math.ZeroInt())
	require.False(t, ok)
}

// TestPickBestRedelegation_MultipleValidators picks the move with largest amount.
func TestPickBestRedelegation_MultipleValidators(t *testing.T) {
	k := keeper.Keeper{}
	deltas := map[string]math.Int{
		"src1": math.NewInt(-100),
		"src2": math.NewInt(-20),
		"dst1": math.NewInt(50),
		"dst2": math.NewInt(60),
	}
	keys := sortedKeys(deltas)
	blocked := make(map[string]map[string]struct{})
	maxMove := math.NewInt(1000) // effectively no cap

	src, dst, amt, ok := k.PickBestRedelegation(deltas, keys, blocked, maxMove)
	require.True(t, ok)
	// Best move is from src1 (overweight 100) to dst2 (underweight 60): amount 60.
	require.Equal(t, "src1", src)
	require.Equal(t, "dst2", dst)
	require.True(t, amt.Equal(math.NewInt(60)))
}

// ---------------------------------------------------------------------------
// 3.3 ComputeDeltas
// ---------------------------------------------------------------------------

// TestComputeDeltas_Basic: target A=100 B=100, current A=120 B=80, totalStake=200; 50 bp threshold = 1.
func TestComputeDeltas_Basic(t *testing.T) {
	_, k := testKeeperWithParams(t, "50", "0")
	target := map[string]math.Int{"A": math.NewInt(100), "B": math.NewInt(100)}
	current := map[string]math.Int{"A": math.NewInt(120), "B": math.NewInt(80)}
	totalStake := math.NewInt(200)

	deltas, err := k.ComputeDeltas(target, current, totalStake, 50)
	require.NoError(t, err)
	require.Len(t, deltas, 2)
	// delta = target - current: A = -20, B = +20. Threshold 200*50/10000 = 1; both |delta| >= 1.
	require.True(t, deltas["A"].Equal(math.NewInt(-20)))
	require.True(t, deltas["B"].Equal(math.NewInt(20)))
}

// TestComputeDeltas_BelowThreshold: same target/current, high RebalanceThresholdBP so threshold > 20.
func TestComputeDeltas_BelowThreshold(t *testing.T) {
	_, k := testKeeperWithParams(t, "1500", "0") // 15% -> threshold 200*1500/10000 = 30
	target := map[string]math.Int{"A": math.NewInt(100), "B": math.NewInt(100)}
	current := map[string]math.Int{"A": math.NewInt(120), "B": math.NewInt(80)}
	totalStake := math.NewInt(200)

	deltas, err := k.ComputeDeltas(target, current, totalStake, 1500)
	require.NoError(t, err)
	require.Len(t, deltas, 2)
	// |delta| 20 < threshold 30 -> both zeroed.
	require.True(t, deltas["A"].Equal(math.ZeroInt()))
	require.True(t, deltas["B"].Equal(math.ZeroInt()))
}

// TestComputeDeltas_UnionOfKeys: validator only in target or only in current; all keys present.
func TestComputeDeltas_UnionOfKeys(t *testing.T) {
	_, k := testKeeperWithParams(t, "50", "0")
	target := map[string]math.Int{"A": math.NewInt(100), "B": math.NewInt(100)}
	current := map[string]math.Int{"A": math.NewInt(50), "C": math.NewInt(50)}
	totalStake := math.NewInt(200)

	deltas, err := k.ComputeDeltas(target, current, totalStake, 50)
	require.NoError(t, err)
	require.Len(t, deltas, 3)
	// A: 100-50=50; B: 100-0=100; C: 0-50=-50. Threshold 1; all non-zero.
	require.True(t, deltas["A"].Equal(math.NewInt(50)))
	require.True(t, deltas["B"].Equal(math.NewInt(100)))
	require.True(t, deltas["C"].Equal(math.NewInt(-50)))
}

// TestComputeDeltas_TotalStakeZero: threshold = 0; deltas are not zeroed by threshold.
func TestComputeDeltas_TotalStakeZero(t *testing.T) {
	_, k := testKeeperWithParams(t, "50", "0")
	target := map[string]math.Int{"A": math.NewInt(0), "B": math.NewInt(0)}
	current := map[string]math.Int{"A": math.NewInt(0), "B": math.NewInt(0)}
	totalStake := math.ZeroInt()

	deltas, err := k.ComputeDeltas(target, current, totalStake, 50)
	require.NoError(t, err)
	require.Len(t, deltas, 2)
	// threshold = 0; delta A = 0, B = 0 (and 0 is not < 0, so they stay 0).
	require.True(t, deltas["A"].Equal(math.ZeroInt()))
	require.True(t, deltas["B"].Equal(math.ZeroInt()))
}

// ---------------------------------------------------------------------------
// 3.4 PickResidualUndelegation
// ---------------------------------------------------------------------------

// TestPickResidualUndelegation_SingleOverweight: one overweight; maxMove=0 -> full amount, maxMove=10 -> capped.
func TestPickResidualUndelegation_SingleOverweight(t *testing.T) {
	deltas := map[string]math.Int{"val": math.NewInt(-100)}

	// maxMove=0 means no cap -> amt = 100
	ctx, k := testKeeperWithParams(t, "50", "0")
	val, amt, ok, err := k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "val", val)
	require.True(t, amt.Equal(math.NewInt(100)))

	// maxMove=10 -> amt = 10
	ctx, k = testKeeperWithParams(t, "50", "10")
	val, amt, ok, err = k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "val", val)
	require.True(t, amt.Equal(math.NewInt(10)))
}

// TestPickResidualUndelegation_LargestWins: two overweight; validator with -100 chosen, amount = min(100, maxMove).
func TestPickResidualUndelegation_LargestWins(t *testing.T) {
	deltas := map[string]math.Int{
		"valA": math.NewInt(-50),
		"valB": math.NewInt(-100),
	}
	ctx, k := testKeeperWithParams(t, "50", "0")

	val, amt, ok, err := k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "valB", val)
	require.True(t, amt.Equal(math.NewInt(100)))

	// With maxMove=30, amount should be 30
	ctx, k = testKeeperWithParams(t, "50", "30")
	_, amt, ok, err = k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, amt.Equal(math.NewInt(30)))
}

// TestPickResidualUndelegation_TieBreak: two same negative delta; lexicographically smaller validator chosen.
func TestPickResidualUndelegation_TieBreak(t *testing.T) {
	deltas := map[string]math.Int{
		"zebra": math.NewInt(-20),
		"alpha": math.NewInt(-20),
	}
	ctx, k := testKeeperWithParams(t, "50", "0")

	val, amt, ok, err := k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "alpha", val)
	require.True(t, amt.Equal(math.NewInt(20)))
}

// TestPickResidualUndelegation_NoOverweight: all deltas >= 0 -> ok false.
func TestPickResidualUndelegation_NoOverweight(t *testing.T) {
	deltas := map[string]math.Int{
		"a": math.NewInt(10),
		"b": math.NewInt(5),
	}
	ctx, k := testKeeperWithParams(t, "50", "0")

	_, _, ok, err := k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestPickResidualUndelegation_ZeroDelta: validator with 0 should not be chosen.
func TestPickResidualUndelegation_ZeroDelta(t *testing.T) {
	deltas := map[string]math.Int{
		"val": math.ZeroInt(),
	}
	ctx, k := testKeeperWithParams(t, "50", "0")

	_, _, ok, err := k.PickResidualUndelegation(ctx, deltas)
	require.NoError(t, err)
	require.False(t, ok)
}
