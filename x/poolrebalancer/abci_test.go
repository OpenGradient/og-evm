package poolrebalancer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
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
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
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
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	storeService := runtime.NewKVStoreService(storeKey)
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	authority := sdk.AccAddress(bytes.Repeat([]byte{9}, 20))

	k := keeper.NewKeeper(cdc, storeService, tKey, sk, authority, endBlockerMockEVM{}, nil)
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
