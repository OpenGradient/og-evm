package keeper

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

type mockStakingKeeper struct {
	vals                 []stakingtypes.Validator
	validatorByAddr      map[string]stakingtypes.Validator
	delegations          []stakingtypes.Delegation
	delegationByValAddr  map[string]stakingtypes.Delegation
	failBeginRedelegation bool
	failUndelegate        bool
}

func (m *mockStakingKeeper) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, nil
}

func (m *mockStakingKeeper) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return m.delegations, nil
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
	if m.failBeginRedelegation {
		return time.Time{}, errors.New("mock begin redelegation failed")
	}
	return sdk.UnwrapSDKContext(ctx).BlockTime().Add(time.Hour), nil
}

func (m *mockStakingKeeper) Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount math.LegacyDec) (completionTime time.Time, amount math.Int, err error) {
	if m.failUndelegate {
		return time.Time{}, math.ZeroInt(), errors.New("mock undelegate failed")
	}
	return sdk.UnwrapSDKContext(ctx).BlockTime().Add(time.Hour), sharesAmount.TruncateInt(), nil
}

func (m *mockStakingKeeper) UnbondingTime(ctx context.Context) (time.Duration, error) {
	return time.Hour, nil
}

func (m *mockStakingKeeper) BondDenom(ctx context.Context) (string, error) {
	return "stake", nil
}

func newProcessRebalanceKeeper(t *testing.T, sk types.StakingKeeper) (sdk.Context, Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	ctx = ctx.WithBlockTime(time.Now().UTC())

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	k := NewKeeper(cdc, storeService, sk, authority)

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

func TestProcessRebalance_EmitsUndelegationFailedEvent(t *testing.T) {
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
		failUndelegate:        true,
	}

	ctx, k := newProcessRebalanceKeeper(t, sk)
	del, _, _ := setupBasicRebalanceState(t, ctx, k)

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.UseUndelegateFallback = true
	require.NoError(t, k.SetParams(ctx, params))

	require.NoError(t, k.ProcessRebalance(ctx))

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type != types.EventTypeUndelegationFailed {
			continue
		}
		found = true
		attrs := attrsToMap(ev.Attributes)
		require.Equal(t, del.String(), attrs[types.AttributeKeyDelegator])
		require.Equal(t, srcVal.String(), attrs[types.AttributeKeyValidator])
		require.Equal(t, "50", attrs[types.AttributeKeyAmount])
		require.Equal(t, "stake", attrs[types.AttributeKeyDenom])
		require.Contains(t, attrs[types.AttributeKeyReason], "mock undelegate failed")
	}
	require.True(t, found, "expected undelegation failure event")
}

