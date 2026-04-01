package keeper

import (
	"bytes"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestQueryParams_RoundTrip(t *testing.T) {
	ctx, k := newTestKeeper(t)

	params := types.DefaultParams()
	params.MaxOpsPerBlock = 7
	require.NoError(t, k.SetParams(ctx, params))

	qs := NewQueryServer(k)
	res, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint32(7), res.Params.MaxOpsPerBlock)
}

func TestQueryPendingRedelegations_DecodesProtoValues(t *testing.T) {
	ctx, k := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	srcVal := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dstVal := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))

	entry := types.PendingRedelegation{
		DelegatorAddress:    del.String(),
		SrcValidatorAddress: srcVal.String(),
		DstValidatorAddress: dstVal.String(),
		Amount:              sdk.NewCoin("stake", math.NewInt(5)),
		CompletionTime:      ctx.BlockTime().Add(time.Hour),
	}
	require.NoError(t, k.SetPendingRedelegation(ctx, entry))

	qs := NewQueryServer(k)
	res, err := qs.PendingRedelegations(ctx, &types.QueryPendingRedelegationsRequest{
		Pagination: &sdkquery.PageRequest{Limit: 1},
	})
	require.NoError(t, err)
	require.Len(t, res.Redelegations, 1)
	require.Equal(t, entry.DelegatorAddress, res.Redelegations[0].DelegatorAddress)
}

func TestQueryPendingUndelegations_PaginatesByQueueBuckets(t *testing.T) {
	ctx, k := newTestKeeper(t)
	ctx = ctx.WithBlockTime(time.Unix(2_000, 0))

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))

	// Bucket 1: earlier completion time, two entries in the same queue key.
	completion1 := ctx.BlockTime().Add(time.Minute)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: sdk.ValAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(1)),
		CompletionTime:   completion1,
	}))
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: sdk.ValAddress(bytes.Repeat([]byte{3}, 20)).String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(2)),
		CompletionTime:   completion1,
	}))

	// Bucket 2: later completion time.
	completion2 := ctx.BlockTime().Add(2 * time.Minute)
	require.NoError(t, k.SetPendingUndelegation(ctx, types.PendingUndelegation{
		DelegatorAddress: del.String(),
		ValidatorAddress: sdk.ValAddress(bytes.Repeat([]byte{4}, 20)).String(),
		Balance:          sdk.NewCoin("stake", math.NewInt(3)),
		CompletionTime:   completion2,
	}))

	qs := NewQueryServer(k)
	res, err := qs.PendingUndelegations(ctx, &types.QueryPendingUndelegationsRequest{
		Pagination: &sdkquery.PageRequest{Limit: 1},
	})
	require.NoError(t, err)

	// Pagination is over queue keys, not individual entries. With Limit=1, we can still receive
	// multiple undelegation entries if the first queue bucket contains more than one entry.
	require.GreaterOrEqual(t, len(res.Undelegations), 2)
}

func TestQueryPendingRedelegations_NilRequest(t *testing.T) {
	ctx, k := newTestKeeper(t)
	qs := NewQueryServer(k)

	_, err := qs.PendingRedelegations(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestQueryPendingUndelegations_NilRequest(t *testing.T) {
	ctx, k := newTestKeeper(t)
	qs := NewQueryServer(k)

	_, err := qs.PendingUndelegations(ctx, nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
