package keeper

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestCompletePendingUndelegations_RemovesQueueAndIndex(t *testing.T) {
	ctx, k := newTestKeeper(t)

	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))
	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	denom := "stake"

	completion := ctx.BlockTime().Add(-time.Second)
	coin := sdk.NewCoin(denom, math.NewInt(123))
	entry := types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: val.String(),
		Balance:          coin,
		CompletionTime:   completion,
	}
	require.NoError(t, k.SetPendingUndelegation(ctx, entry))

	queueKey := types.GetPendingUndelegationQueueKey(completion, del)
	indexKey := types.GetPendingUndelegationByValIndexKey(val, completion, denom, del)

	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(queueKey)
	require.NoError(t, err)
	require.NotNil(t, bz)

	require.NoError(t, k.CompletePendingUndelegations(ctx))

	bz, err = store.Get(queueKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	bz, err = store.Get(indexKey)
	require.NoError(t, err)
	require.Nil(t, bz)

	// Idempotency.
	require.NoError(t, k.CompletePendingUndelegations(ctx))
}
