package poolrebalancer

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/cosmos/evm/x/poolrebalancer/keeper"
	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func newEndBlockerTestKeeper(t *testing.T, sk types.StakingKeeper) (sdk.Context, keeper.Keeper, *storetypes.KVStoreKey) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))

	k := keeper.NewKeeper(cdc, storeService, sk, authority, nil, nil)
	return ctx, k, storeKey
}

// stakingKeeperOpError implements types.StakingKeeper for EndBlocker tests; fails GetBondedValidatorsByPower.
type stakingKeeperOpError struct{}

func (stakingKeeperOpError) GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error) {
	return nil, errors.New("mock staking operational error")
}

func (stakingKeeperOpError) GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error) {
	return nil, nil
}

func (stakingKeeperOpError) GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error) {
	return stakingtypes.Validator{}, errors.New("validator not found")
}

func (stakingKeeperOpError) GetDelegation(ctx context.Context, delegatorAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error) {
	return stakingtypes.Delegation{}, errors.New("delegation not found")
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
