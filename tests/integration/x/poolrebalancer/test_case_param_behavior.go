package poolrebalancer

import (
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	poolrebalancerkeeper "github.com/cosmos/evm/x/poolrebalancer/keeper"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestMaxMovePerOp_CapsScheduledRedelegationAmount verifies that each queued
// redelegation operation amount respects max_move_per_op.
func (s *KeeperIntegrationTestSuite) TestMaxMovePerOp_CapsScheduledRedelegationAmount() {
	// Tiny per-op cap so queue entries are easy to inspect.
	maxMove := sdkmath.OneInt()

	params := s.DefaultEnabledParams(
		0,       // threshold: schedule on any drift
		5,       // multiple ops to validate per-op cap against queue entries
		maxMove, // cap
		false,   // disable fallback to isolate redelegations
	)
	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.T().Logf(
		"max-move-case: src=%s maxMovePerOp=%s maxOps=%d",
		src.OperatorAddress, maxMove.String(), params.MaxOpsPerBlock,
	)

	s.Require().NoError(s.RunEndBlock())

	// Read queue entries (per-op view), not primary entries (which can merge).
	storeService := runtime.NewKVStoreService(s.network.App.GetKey(poolrebalancertypes.StoreKey))
	store := runtime.KVStoreAdapter(storeService.OpenKVStore(s.ctx))
	iter := storetypes.KVStorePrefixIterator(store, poolrebalancertypes.PendingRedelegationQueueKey)
	defer iter.Close() //nolint:errcheck

	queueEntries := make([]poolrebalancertypes.PendingRedelegation, 0)
	for ; iter.Valid(); iter.Next() {
		var queued poolrebalancertypes.QueuedRedelegation
		s.Require().NoError(s.network.App.AppCodec().Unmarshal(iter.Value(), &queued))
		queueEntries = append(queueEntries, queued.Entries...)
	}

	s.Require().GreaterOrEqual(len(queueEntries), 2, "expected multiple queued redelegation ops")
	s.T().Logf("max-move-case: queued ops=%d", len(queueEntries))

	for _, e := range queueEntries {
		s.Require().True(
			e.Amount.Amount.LTE(maxMove),
			"queue entry amount %s exceeds max_move_per_op %s",
			e.Amount.Amount.String(),
			maxMove.String(),
		)
	}
}

// TestMaxTargetValidators_LimitsRedelegationDestinationsToTopN verifies that
// scheduled destinations remain inside the configured top-N target validator set.
func (s *KeeperIntegrationTestSuite) TestMaxTargetValidators_LimitsRedelegationDestinationsToTopN() {
	// Restrict destinations to top-2 bonded validators.
	params := s.DefaultEnabledParams(
		0, // threshold
		3, // allow a few ops in one block
		sdkmath.ZeroInt(),
		false,
	)
	params.MaxTargetValidators = 2
	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)

	targetVals, err := s.poolKeeper.GetTargetBondedValidators(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(targetVals, 2, "expected target set size to match MaxTargetValidators")
	s.T().Logf("target-set-case: src=%s targetSet=%d", src.OperatorAddress, len(targetVals))

	allowedDst := make(map[string]struct{}, len(targetVals))
	for _, v := range targetVals {
		allowedDst[v.String()] = struct{}{}
	}

	s.Require().NoError(s.RunEndBlock())

	pending := s.PendingRedelegations()
	s.Require().NotEmpty(pending, "expected pending redelegations to be scheduled")
	s.T().Logf("target-set-case: pending redelegations=%d", len(pending))

	for _, e := range pending {
		_, ok := allowedDst[e.DstValidatorAddress]
		s.Require().True(
			ok,
			"found destination %s outside top-N target set",
			e.DstValidatorAddress,
		)
	}
}

// TestPendingRedelegationsQuery_PaginatesAndReturnsEntries verifies the gRPC query server
// path returns pending entries with pagination in an integration environment.
func (s *KeeperIntegrationTestSuite) TestPendingRedelegationsQuery_PaginatesAndReturnsEntries() {
	params := s.DefaultEnabledParams(0, 2, sdkmath.ZeroInt(), false)
	s.EnableRebalancer(params)

	src := s.validators[0]
	s.DelegateExtraToValidator(src)
	s.Require().NoError(s.RunEndBlock())

	qs := poolrebalancerkeeper.NewQueryServer(s.poolKeeper)
	res, err := qs.PendingRedelegations(s.ctx, &poolrebalancertypes.QueryPendingRedelegationsRequest{
		Pagination: &sdkquery.PageRequest{Limit: 1},
	})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().NotEmpty(res.Redelegations, "expected paginated query to return at least one pending redelegation")
	s.Require().NotNil(res.Pagination, "expected pagination response metadata")
}

func (s *KeeperIntegrationTestSuite) TestPendingUndelegationsAndParamsQuery_IntegrationPaths() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.ZeroInt(), true)
	s.EnableRebalancer(params)

	// Create fallback conditions: immature incoming redelegation to x blocks src=x use.
	xVal := s.validators[0]
	yVal := s.validators[1]
	immatureCompletion := s.ctx.BlockTime().Add(s.unbondingSec).UTC()
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.OneInt()),
		CompletionTime:      immatureCompletion,
	})
	s.DelegateExtraToValidator(xVal)
	s.Require().NoError(s.RunEndBlock())

	qs := poolrebalancerkeeper.NewQueryServer(s.poolKeeper)

	// Params query: verifies the client-facing params contract in integration.
	pres, err := qs.Params(s.ctx, &poolrebalancertypes.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(uint32(1), pres.Params.MaxOpsPerBlock)
	s.Require().True(pres.Params.UseUndelegateFallback)

	// PendingUndelegations query: verifies paginated decode path.
	ures, err := qs.PendingUndelegations(s.ctx, &poolrebalancertypes.QueryPendingUndelegationsRequest{
		Pagination: &sdkquery.PageRequest{Limit: 1},
	})
	s.Require().NoError(err)
	s.Require().NotNil(ures)
	s.Require().NotEmpty(ures.Undelegations, "expected at least one pending undelegation from fallback path")
	s.Require().NotNil(ures.Pagination, "expected pagination metadata")
}
