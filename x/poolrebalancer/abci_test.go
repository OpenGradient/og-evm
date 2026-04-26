package poolrebalancer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// endBlockerMockEVM satisfies types.EVMKeeper for module-level ABCI tests.
type endBlockerMockEVM struct{}

func (endBlockerMockEVM) CallEVM(
	_ sdk.Context,
	_ abi.ABI,
	_, _ common.Address,
	_ bool,
	_ *big.Int,
	_ string,
	_ ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	return &evmtypes.MsgEthereumTxResponse{}, nil
}

func (endBlockerMockEVM) IsContract(sdk.Context, common.Address) bool { return true }

func newEndBlockerTestKeeper(t *testing.T, sk types.StakingKeeper) (sdk.Context, keeper.Keeper, *storetypes.KVStoreKey) {
	stakingDeps, ok := sk.(interface {
		types.StakingKeeper
		types.StakingQuerier
	})
	require.True(t, ok)
	ctx, k, storeKey, _ := newEndBlockerTestKeeperWithDeps(t, stakingDeps, nil, endBlockerMockEVM{})
	return ctx, k, storeKey
}

func newEndBlockerTestKeeperWithDeps(
	t *testing.T,
	sk interface {
		types.StakingKeeper
		types.StakingQuerier
	},
	dq types.DistributionKeeper,
	evm types.EVMKeeper,
) (sdk.Context, keeper.Keeper, *storetypes.KVStoreKey, *storetypes.TransientStoreKey) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))

	k := keeper.NewKeeper(cdc, storeService, tKey, sk, sk, dq, authority, evm, nil)
	return ctx, k, storeKey, tKey
}

// recordingEndBlockerEVM appends each invoked CommunityPool method name for ordering assertions.
type recordingEndBlockerEVM struct {
	methods []string
}

func (m *recordingEndBlockerEVM) CallEVM(
	_ sdk.Context,
	_ abi.ABI,
	_, _ common.Address,
	_ bool,
	_ *big.Int,
	method string,
	_ ...any,
) (*evmtypes.MsgEthereumTxResponse, error) {
	m.methods = append(m.methods, method)
	return &evmtypes.MsgEthereumTxResponse{}, nil
}

func (recordingEndBlockerEVM) IsContract(sdk.Context, common.Address) bool { return true }

func newEndBlockerTestKeeperWithRecordingEVM(t *testing.T, sk types.StakingKeeper, evm *recordingEndBlockerEVM) (sdk.Context, keeper.Keeper, *storetypes.KVStoreKey) {
	stakingDeps, ok := sk.(interface {
		types.StakingKeeper
		types.StakingQuerier
	})
	require.True(t, ok)
	ctx, k, storeKey, _ := newEndBlockerTestKeeperWithDeps(t, stakingDeps, nil, evm)
	return ctx, k, storeKey
}

func indexOfMethod(methods []string, name string) int {
	for i, m := range methods {
		if m == name {
			return i
		}
	}
	return -1
}

// stakingKeeperOpError implements types.StakingKeeper for EndBlocker tests; fails GetBondedValidatorsByPower.
type stakingKeeperOpError struct{}

func (stakingKeeperOpError) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return nil, errors.New("mock staking operational error")
}

func (stakingKeeperOpError) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return nil, nil
}

func (m stakingKeeperOpError) DelegatorDelegations(ctx context.Context, req *stakingtypes.QueryDelegatorDelegationsRequest) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	delegations, err := m.GetDelegatorDelegations(ctx, sdk.MustAccAddressFromBech32(req.DelegatorAddr), 0)
	if err != nil {
		return nil, err
	}
	start := 0
	if req != nil && req.Pagination != nil && len(req.Pagination.Key) > 0 {
		parsed, err := strconv.Atoi(string(req.Pagination.Key))
		if err != nil {
			return nil, err
		}
		start = parsed
	}
	if start > len(delegations) {
		start = len(delegations)
	}
	limit := len(delegations)
	if req != nil && req.Pagination != nil && req.Pagination.Limit > 0 && int(req.Pagination.Limit) < limit {
		limit = int(req.Pagination.Limit)
	}
	end := start + limit
	if end > len(delegations) {
		end = len(delegations)
	}
	responses := make([]stakingtypes.DelegationResponse, 0, end-start)
	for _, delegation := range delegations[start:end] {
		responses = append(responses, stakingtypes.DelegationResponse{Delegation: delegation})
	}
	var nextKey []byte
	if end < len(delegations) {
		nextKey = []byte(strconv.Itoa(end))
	}
	return &stakingtypes.QueryDelegatorDelegationsResponse{
		DelegationResponses: responses,
		Pagination:          &query.PageResponse{NextKey: nextKey},
	}, nil
}

func (stakingKeeperOpError) GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, errors.New("validator not found")
}

func (stakingKeeperOpError) GetDelegation(ctx context.Context, delegatorAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error) {
	return stakingtypes.Delegation{}, errors.New("delegation not found")
}

func (stakingKeeperOpError) GetUnbondingDelegation(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.UnbondingDelegation, error) {
	return stakingtypes.UnbondingDelegation{}, stakingtypes.ErrNoUnbondingDelegation
}

func (stakingKeeperOpError) BeginRedelegation(ctx context.Context, delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress, sharesAmount math.LegacyDec) (time.Time, error) {
	return time.Time{}, errors.New("not implemented")
}

func (stakingKeeperOpError) Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount math.LegacyDec) (time.Time, math.Int, error) {
	return time.Time{}, math.ZeroInt(), errors.New("not implemented")
}

func (stakingKeeperOpError) UnbondingTime(ctx context.Context) (time.Duration, error) {
	return time.Hour, nil
}

func (stakingKeeperOpError) BondDenom(ctx context.Context) (string, error) {
	return "stake", nil
}

// stakingKeeperBeginEndFlow stubs GetUnbondingDelegation for BeginBlocker/EndBlocker tests that need
// a matching UBD in staking state (pool Del|val key).
type stakingKeeperBeginEndFlow struct {
	stakingKeeperOpError
	ubdByDelVal map[string]stakingtypes.UnbondingDelegation
}

func (m stakingKeeperBeginEndFlow) GetUnbondingDelegation(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.UnbondingDelegation, error) {
	if m.ubdByDelVal == nil {
		return stakingtypes.UnbondingDelegation{}, stakingtypes.ErrNoUnbondingDelegation
	}
	ubd, ok := m.ubdByDelVal[delAddr.String()+"|"+valAddr.String()]
	if !ok {
		return stakingtypes.UnbondingDelegation{}, stakingtypes.ErrNoUnbondingDelegation
	}
	return ubd, nil
}

type beginBlockSlashAwareStaking struct {
	stakingKeeperOpError
	vals        []stakingtypes.Validator
	delegations []stakingtypes.Delegation
}

func (m beginBlockSlashAwareStaking) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, nil
}

func (m beginBlockSlashAwareStaking) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return m.delegations, nil
}

type beginBlockDistributionQuerier struct {
	slashHeightsByValidator map[string]map[uint64]struct{}
}

func (m beginBlockDistributionQuerier) IterateValidatorSlashEventsBetween(ctx context.Context, val sdk.ValAddress, startingHeight, endingHeight uint64, handler func(height uint64, event distributiontypes.ValidatorSlashEvent) (stop bool)) {
	if heights, ok := m.slashHeightsByValidator[val.String()]; ok {
		for h := startingHeight; h <= endingHeight; h++ {
			if _, exists := heights[h]; exists {
				if handler(h, distributiontypes.ValidatorSlashEvent{ValidatorPeriod: h, Fraction: math.LegacyNewDec(1)}) {
					return
				}
			}
		}
	}
}

func readSlashedSnapshot(ctx sdk.Context, tKey *storetypes.TransientStoreKey) []string {
	bz := ctx.TransientStore(tKey).Get(types.PreviousBlockSlashedValidatorsTransientKey)
	if bz == nil {
		return nil
	}
	if len(bz) == 0 {
		return []string{}
	}
	out := strings.Split(string(bz), "\n")
	sort.Strings(out)
	return out
}

func TestEndBlocker_ProcessRebalanceErrorIsNonHalting(t *testing.T) {
	ctx, k, _ := newEndBlockerTestKeeper(t, stakingKeeperOpError{})

	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	require.NoError(t, k.SetParams(ctx, params))

	err := EndBlocker(ctx, k)
	require.NoError(t, err, "ProcessRebalance failures should not halt EndBlocker")
}

func TestEndBlocker_InvalidParamsHaltsOnCleanup(t *testing.T) {
	ctx, k, storeKey := newEndBlockerTestKeeper(t, stakingKeeperOpError{})

	// CompletePendingUndelegations loads params before harvest/stake; invalid proto must halt EndBlock.
	ctx.KVStore(storeKey).Set(types.ParamsKey, []byte("not-a-valid-proto"))

	err := EndBlocker(ctx, k)
	require.Error(t, err, "params corruption should halt during pending undelegation completion")
}

func TestEndBlocker_CleanupErrorRemainsHalting(t *testing.T) {
	ctx, k, storeKey := newEndBlockerTestKeeper(t, stakingKeeperOpError{})
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	// Seed an invalid queued redelegation value so cleanup fails on unmarshal.
	maturedKey := types.GetPendingRedelegationQueueKey(now.Add(-time.Second))
	ctx.KVStore(storeKey).Set(maturedKey, []byte("not-a-valid-proto"))

	err := EndBlocker(ctx, k)
	require.Error(t, err, "cleanup failures should remain halting")
}

// stakingEndBlockSecondPass makes ProcessRebalance a no-op (no pool stake) while bonded targets exist.
type stakingEndBlockSecondPass struct {
	stakingKeeperOpError
}

func (stakingEndBlockSecondPass) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	valAddr := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	return []stakingtypes.Validator{{
		OperatorAddress: valAddr.String(),
		Tokens:          math.NewInt(1000),
		DelegatorShares: math.LegacyNewDec(1000),
		Status:          stakingtypes.Bonded,
	}}, nil
}

func (stakingEndBlockSecondPass) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return nil, nil
}

func countMethod(methods []string, name string) int {
	n := 0
	for _, m := range methods {
		if m == name {
			n++
		}
	}
	return n
}

func TestEndBlocker_SecondReconcileAfterProcessRebalanceWhenSecondPassEnabled(t *testing.T) {
	rec := &recordingEndBlockerEVM{}
	ctx, k, _ := newEndBlockerTestKeeperWithRecordingEVM(t, stakingEndBlockSecondPass{}, rec)
	k.SetCommunityPoolReconcileSecondPassForTesting(true)
	t.Cleanup(func() { k.SetCommunityPoolReconcileSecondPassForTesting(false) })
	ctx = ctx.WithBlockHeight(40)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, EndBlocker(ctx, k))

	require.GreaterOrEqual(t, countMethod(rec.methods, "reconcileStakedBuckets"), 2,
		"expected first reconcile after cleanup and second after successful ProcessRebalance no-op: %v", rec.methods)
}

func TestEndBlocker_CreditStakeableBeforeReconcileStakedBuckets(t *testing.T) {
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))

	now := time.Now().UTC()
	completion := now.Add(-time.Second)
	sk := stakingKeeperBeginEndFlow{
		ubdByDelVal: map[string]stakingtypes.UnbondingDelegation{
			poolDel.String() + "|" + val.String(): {
				DelegatorAddress: poolDel.String(),
				ValidatorAddress: val.String(),
				Entries: []stakingtypes.UnbondingDelegationEntry{
					{
						CompletionTime: completion,
						Balance:        math.NewInt(25),
					},
				},
			},
		},
	}
	rec := &recordingEndBlockerEVM{}
	ctx, k, _ := newEndBlockerTestKeeperWithRecordingEVM(t, sk, rec)
	ctx = ctx.WithBlockTime(now)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(50)),
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	require.NoError(t, BeginBlocker(ctx, k))
	require.NoError(t, EndBlocker(ctx, k))

	iCredit := indexOfMethod(rec.methods, "creditStakeableFromRebalance")
	iRecon := indexOfMethod(rec.methods, "reconcileStakedBuckets")
	require.NotEqual(t, -1, iCredit, "expected credit: %v", rec.methods)
	require.NotEqual(t, -1, iRecon, "expected reconcile: %v", rec.methods)
	require.Less(t, iCredit, iRecon, "credit must run before bucket reconcile: %v", rec.methods)
}

func TestBeginThenEndBlocker_MaturedPoolUndelegationFlow_Succeeds(t *testing.T) {
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))

	now := time.Now().UTC()
	completion := now.Add(-time.Second)
	sk := stakingKeeperBeginEndFlow{
		ubdByDelVal: map[string]stakingtypes.UnbondingDelegation{
			poolDel.String() + "|" + val.String(): {
				DelegatorAddress: poolDel.String(),
				ValidatorAddress: val.String(),
				Entries: []stakingtypes.UnbondingDelegationEntry{
					{
						CompletionTime: completion,
						Balance:        math.NewInt(25),
					},
				},
			},
		},
	}
	ctx, k, _ := newEndBlockerTestKeeper(t, sk)
	ctx = ctx.WithBlockTime(now)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	entry := types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(50)), // queue differs from staking balance
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	require.NoError(t, BeginBlocker(ctx, k))
	require.NoError(t, EndBlocker(ctx, k))
}

func TestBeginBlocker_HaltsWhenMaturedPoolUndelegationMissingUBD(t *testing.T) {
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))

	now := time.Now().UTC()
	ctx, k, _ := newEndBlockerTestKeeper(t, stakingKeeperBeginEndFlow{})
	ctx = ctx.WithBlockTime(now)

	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: poolDel.String(),
		ValidatorAddress: val.String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   now.Add(-time.Second),
	}))

	err := BeginBlocker(ctx, k)
	require.Error(t, err, "missing matured UBD must halt BeginBlock snapshot")
}

func TestBeginBlocker_PreparesPreviousBlockSlashedValidatorsSnapshot(t *testing.T) {
	poolDel := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	targetA := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	targetB := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	delegatedOnly := sdk.ValAddress(bytes.Repeat([]byte{4}, 20))

	sk := beginBlockSlashAwareStaking{
		vals: []stakingtypes.Validator{
			{OperatorAddress: targetA.String(), Tokens: math.NewInt(100), DelegatorShares: math.LegacyNewDec(100)},
			{OperatorAddress: targetB.String(), Tokens: math.NewInt(90), DelegatorShares: math.LegacyNewDec(90)},
		},
		delegations: []stakingtypes.Delegation{
			{DelegatorAddress: poolDel.String(), ValidatorAddress: delegatedOnly.String(), Shares: math.LegacyNewDec(10)},
		},
	}
	dq := beginBlockDistributionQuerier{
		slashHeightsByValidator: map[string]map[uint64]struct{}{
			targetB.String():      {9: {}},
			delegatedOnly.String(): {8: {}},
		},
	}

	ctx, k, _, tKey := newEndBlockerTestKeeperWithDeps(t, sk, dq, endBlockerMockEVM{})
	ctx = ctx.WithBlockHeight(10)
	params := types.DefaultParams()
	params.PoolDelegatorAddress = poolDel.String()
	params.MaxTargetValidators = 2
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, BeginBlocker(ctx, k))
	require.Equal(t, []string{targetB.String()}, readSlashedSnapshot(ctx, tKey))
}

