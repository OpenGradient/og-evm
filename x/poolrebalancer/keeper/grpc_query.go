package keeper

import (
	"context"

	"cosmossdk.io/store/prefix"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = QueryServer{}

type QueryServer struct {
	k Keeper
}

func NewQueryServer(k Keeper) QueryServer {
	return QueryServer{k: k}
}

func (qs QueryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := qs.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

func (qs QueryServer) PendingRedelegations(
	ctx context.Context,
	req *types.QueryPendingRedelegationsRequest,
) (*types.QueryPendingRedelegationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	store := runtime.KVStoreAdapter(qs.k.storeService.OpenKVStore(ctx))
	pstore := prefix.NewStore(store, types.PendingRedelegationKey)

	var out []types.PendingRedelegation
	pageRes, err := query.Paginate(pstore, req.Pagination, func(key, value []byte) error {
		var entry types.PendingRedelegation
		if err := qs.k.cdc.Unmarshal(value, &entry); err != nil {
			return err
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryPendingRedelegationsResponse{
		Redelegations: out,
		Pagination:    pageRes,
	}, nil
}

func (qs QueryServer) PendingUndelegations(
	ctx context.Context,
	req *types.QueryPendingUndelegationsRequest,
) (*types.QueryPendingUndelegationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	// query.Paginate walks queue store keys (completion_time, delegator); each value is a
	// QueuedUndelegation batch. limit/next_key count buckets, not undelegations, so one page
	// can return many undelegations when a bucket has multiple entries (see query.proto).
	store := runtime.KVStoreAdapter(qs.k.storeService.OpenKVStore(ctx))
	pstore := prefix.NewStore(store, types.PendingUndelegationQueueKey)

	var out []types.PendingUndelegation
	pageRes, err := query.Paginate(pstore, req.Pagination, func(key, value []byte) error {
		var queued types.QueuedUndelegation
		if err := qs.k.cdc.Unmarshal(value, &queued); err != nil {
			return err
		}
		out = append(out, queued.Entries...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryPendingUndelegationsResponse{
		Undelegations: out,
		Pagination:    pageRes,
	}, nil
}
