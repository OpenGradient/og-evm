package keeper

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/contracts"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/testutil"
)

func TestShouldRunScheduler(t *testing.T) {
	key := storetypes.NewKVStoreKey("scheduler_test")
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("scheduler_transient")).WithBlockHeader(cmtproto.Header{Height: 12})

	params := vmtypes.EVMSchedulerParams{
		Enabled:        true,
		TargetContract: "0x0000000000000000000000000000000000001000",
		GasCap:         vmtypes.DefaultSchedulerGasCap,
		MaxOps:         vmtypes.DefaultSchedulerMaxOps,
		CadenceBlocks:  3,
	}
	require.True(t, shouldRunScheduler(ctx, params))

	params.CadenceBlocks = 5
	require.False(t, shouldRunScheduler(ctx, params))

	params.CadenceBlocks = 3
	params.Enabled = false
	require.False(t, shouldRunScheduler(ctx, params))

	params.Enabled = true
	params.TargetContract = ""
	require.False(t, shouldRunScheduler(ctx, params))
}

func TestPokeCalldata(t *testing.T) {
	data := pokeCalldata(32)
	pokeMethod, ok := contracts.StakedBondVaultContract.ABI.Methods["poke"]
	require.True(t, ok)
	require.Equal(t, pokeMethod.ID, pokeSelector)
	require.Len(t, data, 36)
	require.Equal(t, pokeSelector, data[:4])
	require.Equal(t, byte(32), data[35])
}

func TestEVMCallGasLimit(t *testing.T) {
	limit, err := evmCallGasLimit(nil)
	require.NoError(t, err)
	require.NotZero(t, limit)

	limit, err = evmCallGasLimit(big.NewInt(123))
	require.NoError(t, err)
	require.Equal(t, uint64(123), limit)

	_, err = evmCallGasLimit(big.NewInt(0))
	require.ErrorContains(t, err, "greater than zero")
}
