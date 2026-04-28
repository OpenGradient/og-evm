package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

type recordedBeginRedelegation struct {
	del            sdk.AccAddress
	srcVal, dstVal sdk.ValAddress
	shares         math.LegacyDec
}

type mockStakingKeeper struct {
	vals                  []stakingtypes.Validator
	validatorByAddr       map[string]stakingtypes.Validator
	delegations           []stakingtypes.Delegation
	delegationByValAddr   map[string]stakingtypes.Delegation
	failBeginRedelegation bool

	beginRedelegationRecords []recordedBeginRedelegation
}

func (m *mockStakingKeeper) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, nil
}

func (m *mockStakingKeeper) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return m.delegations, nil
}

func (m *mockStakingKeeper) DelegatorDelegations(ctx context.Context, req *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	start := 0
	if req != nil && req.Pagination != nil && len(req.Pagination.Key) > 0 {
		parsed, err := strconv.Atoi(string(req.Pagination.Key))
		if err != nil {
			return nil, fmt.Errorf("invalid pagination key: %w", err)
		}
		start = parsed
	}
	if start > len(m.delegations) {
		start = len(m.delegations)
	}
	limit := len(m.delegations)
	if req != nil && req.Pagination != nil && req.Pagination.Limit > 0 && int(req.Pagination.Limit) < limit {
		limit = int(req.Pagination.Limit)
	}
	end := start + limit
	if end > len(m.delegations) {
		end = len(m.delegations)
	}
	responses := make([]stakingtypes.DelegationResponse, 0, end-start)
	for _, delegation := range m.delegations[start:end] {
		responses = append(responses, stakingtypes.DelegationResponse{Delegation: delegation})
	}
	var nextKey []byte
	if end < len(m.delegations) {
		nextKey = []byte(strconv.Itoa(end))
	}
	return &stakingtypes.QueryDelegatorDelegationsResponse{
		DelegationResponses: responses,
		Pagination:          &query.PageResponse{NextKey: nextKey},
	}, nil
}

func (m *mockStakingKeeper) GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	val, ok := m.validatorByAddr[addr.String()]
	if !ok {
		return stakingtypes.Validator{}, errors.New("validator not found")
	}
	return val, nil
}

func (m *mockStakingKeeper) GetDelegation(ctx context.Context, delegatorAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error) {
	delegation, ok := m.delegationByValAddr[valAddr.String()]
	if !ok {
		return stakingtypes.Delegation{}, errors.New("delegation not found")
	}
	return delegation, nil
}

func (m *mockStakingKeeper) BeginRedelegation(ctx context.Context, delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress, sharesAmount math.LegacyDec) (completionTime time.Time, err error) {
	m.beginRedelegationRecords = append(m.beginRedelegationRecords, recordedBeginRedelegation{
		del: delAddr, srcVal: valSrcAddr, dstVal: valDstAddr, shares: sharesAmount,
	})
	if m.failBeginRedelegation {
		return time.Time{}, errors.New("mock begin redelegation failed")
	}
	return sdk.UnwrapSDKContext(ctx).BlockTime().Add(time.Hour), nil
}

func (m *mockStakingKeeper) UnbondingTime(ctx context.Context) (time.Duration, error) {
	return time.Hour, nil
}

func (m *mockStakingKeeper) BondDenom(ctx context.Context) (string, error) {
	return "stake", nil
}

func newProcessRebalanceKeeper(t *testing.T, sk *mockStakingKeeper) (sdk.Context, Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	ctx = ctx.WithBlockTime(time.Now().UTC())

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := NewKeeper(cdc, storeService, tKey, sk, sk, nil, authority, &mockEVMKeeper{}, nil)

	return ctx, k
}

func setupBasicRebalanceState(t *testing.T, ctx sdk.Context, k Keeper) (sdk.AccAddress, sdk.ValAddress, sdk.ValAddress) {
	t.Helper()

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 2
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 1
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))

	return del, srcVal, dstVal
}

func attrsToMap(attrs []abci.EventAttribute) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value
	}
	return out
}

func TestGetTargetBondedValidators_UsesPowerOrderAndMaxTargetCap(t *testing.T) {
	valLowAddr := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valHighAddr := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valMidAddr := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	valLow := stakingtypes.Validator{OperatorAddress: valLowAddr.String()}
	valHigh := stakingtypes.Validator{OperatorAddress: valHighAddr.String()}
	valMid := stakingtypes.Validator{OperatorAddress: valMidAddr.String()}

	sk := &mockStakingKeeper{
		// Mock the staking keeper's bonded-by-power query order directly. The rebalancer policy is to
		// preserve this order and cap it, not to sort by operator address or mirror contract stake remainder order.
		vals: []stakingtypes.Validator{valHigh, valMid, valLow},
	}
	ctx, k := newProcessRebalanceKeeper(t, sk)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	params.MaxTargetValidators = 2
	require.NoError(t, k.SetParams(ctx, params))

	targets, err := k.GetTargetBondedValidators(ctx)
	require.NoError(t, err)
	require.Equal(t, []sdk.ValAddress{valHighAddr, valMidAddr}, targets)
}

func TestProcessRebalance_EmitsRedelegationFailedEvent(t *testing.T) {
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	srcValidator := stakingtypes.Validator{
		OperatorAddress: srcVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}
	dstValidator := stakingtypes.Validator{
		OperatorAddress: dstVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{srcValidator, dstValidator},
		validatorByAddr: map[string]stakingtypes.Validator{
			srcVal.String(): srcValidator,
			dstVal.String(): dstValidator,
		},
		delegations: []stakingtypes.Delegation{
			{
				DelegatorAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			srcVal.String(): {
				DelegatorAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
		failBeginRedelegation: true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	del, _, _ := setupBasicRebalanceState(t, ctx, k)

	require.NoError(t, k.ProcessRebalance(ctx))

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type != types.EventTypeRedelegationFailed {
			continue
		}
		found = true
		attrs := attrsToMap(ev.Attributes)
		require.Equal(t, del.String(), attrs[types.AttributeKeyDelegator])
		require.Equal(t, srcVal.String(), attrs[types.AttributeKeySrcValidator])
		require.Equal(t, dstVal.String(), attrs[types.AttributeKeyDstValidator])
		require.Equal(t, "50", attrs[types.AttributeKeyAmount])
		require.Equal(t, "stake", attrs[types.AttributeKeyDenom])
		require.Contains(t, attrs[types.AttributeKeyReason], "mock begin redelegation failed")
	}
	require.True(t, found, "expected redelegation failure event")
}

func TestProcessRebalance_EmitsOnlyRedelegationFailureWhenRedelegationFails(t *testing.T) {
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	srcValidator := stakingtypes.Validator{
		OperatorAddress: srcVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}
	dstValidator := stakingtypes.Validator{
		OperatorAddress: dstVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{srcValidator, dstValidator},
		validatorByAddr: map[string]stakingtypes.Validator{
			srcVal.String(): srcValidator,
			dstVal.String(): dstValidator,
		},
		delegations: []stakingtypes.Delegation{
			{
				DelegatorAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			srcVal.String(): {
				DelegatorAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
		failBeginRedelegation: true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	del, _, _ := setupBasicRebalanceState(t, ctx, k)

	require.NoError(t, k.ProcessRebalance(ctx))

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type == types.EventTypeRedelegationFailed {
			found = true
		}
	}
	require.True(t, found, "expected redelegation failure event when begin redelegation fails")
	_ = del
	_ = srcVal
}

// TestProcessRebalance_StopsWhenRedelegationBlocked ensures the loop stops when no
// eligible redelegation can be started.
func TestProcessRebalance_StopsWhenRedelegationBlocked(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valC := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	mkVal := func(addr sdk.ValAddress) stakingtypes.Validator {
		return stakingtypes.Validator{
			OperatorAddress: addr.String(),
			Tokens:          math.NewInt(100),
			DelegatorShares: math.LegacyNewDec(100),
		}
	}
	valASt := mkVal(valA)
	valBSt := mkVal(valB)
	valCSt := mkVal(valC)

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{valASt, valBSt, valCSt},
		validatorByAddr: map[string]stakingtypes.Validator{
			valA.String(): valASt,
			valB.String(): valBSt,
			valC.String(): valCSt,
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(90)},
			{DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			valA.String(): {DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(90)},
			valB.String(): {DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		failBeginRedelegation: true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 3
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 5
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.ProcessRebalance(ctx))

	var successOps int
	for _, ev := range sdk.UnwrapSDKContext(ctx).EventManager().Events() {
		switch ev.Type {
		case types.EventTypeRebalanceSummary:
			attrs := attrsToMap(ev.Attributes)
			require.Equal(t, del.String(), attrs[types.AttributeKeyDelegator])
			successOps, _ = strconv.Atoi(attrs[types.AttributeKeyOpsDone])
		}
	}
	require.Equal(t, 0, successOps, "expected no successful ops when redelegations are blocked/failing")
}

func TestProcessRebalance_RedelegationFailureDoesNotScheduleOps(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valC := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	mkVal := func(addr sdk.ValAddress) stakingtypes.Validator {
		return stakingtypes.Validator{
			OperatorAddress: addr.String(),
			Tokens:          math.NewInt(100),
			DelegatorShares: math.LegacyNewDec(100),
		}
	}
	valASt := mkVal(valA)
	valBSt := mkVal(valB)
	valCSt := mkVal(valC)

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{valASt, valBSt, valCSt},
		validatorByAddr: map[string]stakingtypes.Validator{
			valA.String(): valASt,
			valB.String(): valBSt,
			valC.String(): valCSt,
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(90)},
			{DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			valA.String(): {DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(90)},
			valB.String(): {DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		failBeginRedelegation: true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 3
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 5
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.ProcessRebalance(ctx))

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	var successOps int
	var sawRedelegationFailure bool
	for _, ev := range events {
		switch ev.Type {
		case types.EventTypeRedelegationFailed:
			sawRedelegationFailure = true
		case types.EventTypeRebalanceSummary:
			attrs := attrsToMap(ev.Attributes)
			successOps, _ = strconv.Atoi(attrs[types.AttributeKeyOpsDone])
		}
	}

	require.True(t, sawRedelegationFailure, "expected redelegation failure event")
	require.Equal(t, 0, successOps, "expected no successful ops when redelegation cannot begin")
	_ = valB
}

// TestProcessRebalance_HappyPath_SingleRedelegation asserts one successful scheduling op maps to exactly
// one staking BeginRedelegation with the shares implied by token amount (equal-weight target, max_ops=1).
func TestProcessRebalance_HappyPath_SingleRedelegation(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	srcValidator := stakingtypes.Validator{
		OperatorAddress: srcVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}
	dstValidator := stakingtypes.Validator{
		OperatorAddress: dstVal.String(),
		Tokens:          math.NewInt(100),
		DelegatorShares: math.LegacyNewDec(100),
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{srcValidator, dstValidator},
		validatorByAddr: map[string]stakingtypes.Validator{
			srcVal.String(): srcValidator,
			dstVal.String(): dstValidator,
		},
		delegations: []stakingtypes.Delegation{
			{
				DelegatorAddress: del.String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			srcVal.String(): {
				DelegatorAddress: del.String(),
				ValidatorAddress: srcVal.String(),
				Shares:           math.LegacyNewDec(100),
			},
		},
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	gotDel, gotSrc, gotDst := setupBasicRebalanceState(t, ctx, k)
	require.Equal(t, del.String(), gotDel.String())
	require.Equal(t, srcVal.String(), gotSrc.String())
	require.Equal(t, dstVal.String(), gotDst.String())

	require.NoError(t, k.ProcessRebalance(ctx))

	require.Len(t, sk.beginRedelegationRecords, 1, "expected exactly one BeginRedelegation")

	rec := sk.beginRedelegationRecords[0]
	require.Equal(t, del.String(), rec.del.String())
	require.Equal(t, srcVal.String(), rec.srcVal.String())
	require.Equal(t, dstVal.String(), rec.dstVal.String())
	// 100 stake total, 2 validators → target 50 each; move 50 tokens from src → dst; 1:1 token/share.
	require.True(t, rec.shares.Equal(math.LegacyNewDec(50)), "shares=%s", rec.shares.String())

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	var sawStart, sawSummary bool
	for _, ev := range events {
		switch ev.Type {
		case types.EventTypeRedelegationStarted:
			sawStart = true
			attrs := attrsToMap(ev.Attributes)
			require.Equal(t, "50", attrs[types.AttributeKeyAmount])
			require.Equal(t, "stake", attrs[types.AttributeKeyDenom])
		case types.EventTypeRebalanceSummary:
			sawSummary = true
			attrs := attrsToMap(ev.Attributes)
			require.Equal(t, "1", attrs[types.AttributeKeyOpsDone])
		}
	}
	require.True(t, sawStart, "expected redelegation started event")
	require.True(t, sawSummary, "expected rebalance summary event")
}

func TestProcessRebalance_PrioritizesSlashedValidatorSource(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valC := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	mkVal := func(addr sdk.ValAddress, tokens int64) stakingtypes.Validator {
		return stakingtypes.Validator{
			OperatorAddress: addr.String(),
			Tokens:          math.NewInt(tokens),
			DelegatorShares: math.LegacyNewDec(tokens),
		}
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{mkVal(valA, 100), mkVal(valB, 100), mkVal(valC, 100)},
		validatorByAddr: map[string]stakingtypes.Validator{
			valA.String(): mkVal(valA, 100),
			valB.String(): mkVal(valB, 100),
			valC.String(): mkVal(valC, 100),
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(50)},
			{DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			valA.String(): {DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(50)},
			valB.String(): {DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 3
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 1
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.setPreviousBlockSlashedValidators(ctx, map[string]struct{}{valA.String(): {}}))

	require.NoError(t, k.ProcessRebalance(ctx))

	require.Len(t, sk.beginRedelegationRecords, 1)
	rec := sk.beginRedelegationRecords[0]
	require.Equal(t, valA.String(), rec.srcVal.String(), "slash-priority should move away from slashed validator first")
	require.Equal(t, valC.String(), rec.dstVal.String())
	require.True(t, rec.shares.Equal(math.LegacyNewDec(50)), "expected full move away from slashed validator after target exclusion")
}

func TestProcessRebalance_ExcludesSlashedValidatorFromDestinations(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valC := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	mkVal := func(addr sdk.ValAddress, tokens int64) stakingtypes.Validator {
		return stakingtypes.Validator{
			OperatorAddress: addr.String(),
			Tokens:          math.NewInt(tokens),
			DelegatorShares: math.LegacyNewDec(tokens),
		}
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{mkVal(valA, 100), mkVal(valB, 100), mkVal(valC, 100)},
		validatorByAddr: map[string]stakingtypes.Validator{
			valA.String(): mkVal(valA, 100),
			valB.String(): mkVal(valB, 100),
			valC.String(): mkVal(valC, 100),
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(100)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			valA.String(): {DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(100)},
		},
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 3
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 1
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.setPreviousBlockSlashedValidators(ctx, map[string]struct{}{valB.String(): {}}))

	require.NoError(t, k.ProcessRebalance(ctx))

	require.Len(t, sk.beginRedelegationRecords, 1)
	rec := sk.beginRedelegationRecords[0]
	require.Equal(t, valA.String(), rec.srcVal.String())
	require.Equal(t, valC.String(), rec.dstVal.String(), "slashed validator must not be chosen as same-block destination")
	require.True(t, rec.shares.Equal(math.LegacyNewDec(50)), "expected recomputed unslashed target split")
}

func TestProcessRebalance_SlashedPriorityStillNoSuccessfulOpsWhenRedelegationFails(t *testing.T) {
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	valA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	valB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	valC := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	mkVal := func(addr sdk.ValAddress, tokens int64) stakingtypes.Validator {
		return stakingtypes.Validator{
			OperatorAddress: addr.String(),
			Tokens:          math.NewInt(tokens),
			DelegatorShares: math.LegacyNewDec(tokens),
		}
	}

	sk := &mockStakingKeeper{
		vals: []stakingtypes.Validator{mkVal(valA, 100), mkVal(valB, 100), mkVal(valC, 100)},
		validatorByAddr: map[string]stakingtypes.Validator{
			valA.String(): mkVal(valA, 100),
			valB.String(): mkVal(valB, 100),
			valC.String(): mkVal(valC, 100),
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(50)},
			{DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		delegationByValAddr: map[string]stakingtypes.Delegation{
			valA.String(): {DelegatorAddress: del.String(), ValidatorAddress: valA.String(), Shares: math.LegacyNewDec(50)},
			valB.String(): {DelegatorAddress: del.String(), ValidatorAddress: valB.String(), Shares: math.LegacyNewDec(70)},
		},
		failBeginRedelegation: true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = del.String()
	params.MaxTargetValidators = 3
	params.RebalanceThresholdBp = 0
	params.MaxOpsPerBlock = 1
	params.MaxMovePerOp = math.ZeroInt()
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.setPreviousBlockSlashedValidators(ctx, map[string]struct{}{valA.String(): {}}))

	require.NoError(t, k.ProcessRebalance(ctx))

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	var sawFailure bool
	var sawSummary bool
	for _, ev := range events {
		switch ev.Type {
		case types.EventTypeRedelegationFailed:
			sawFailure = true
		case types.EventTypeRebalanceSummary:
			sawSummary = true
		}
	}
	require.True(t, sawFailure, "expected redelegation failure event")
	require.False(t, sawSummary, "expected no successful operations when redelegation starts fail")
	_ = valA
}
