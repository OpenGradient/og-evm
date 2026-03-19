package poolrebalancer

import (
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// TestCompletionCleanup_RemovesMatureRedelegationsAndUndelegations verifies that
// mature pending redelegation and undelegation entries are removed during EndBlock.
func (s *KeeperIntegrationTestSuite) TestCompletionCleanup_RemovesMatureRedelegationsAndUndelegations() {
	// Disable rebalancer so this test only exercises cleanup paths.
	s.EnableRebalancer(s.DisabledParams())

	xVal := s.validators[0]
	yVal := s.validators[1]

	matureCompletion := s.ctx.BlockTime().Add(-1 * time.Second)

	// Seed mature entries (completion already in the past).
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.NewInt(5)),
		CompletionTime:      matureCompletion.UTC(),
	})

	s.SeedPendingUndelegation(poolrebalancertypes.PendingUndelegation{
		DelegatorAddress:  s.poolDel.String(),
		ValidatorAddress:  xVal.OperatorAddress,
		Balance:            sdk.NewCoin(s.bondDenom, sdkmath.NewInt(7)),
		CompletionTime:    matureCompletion.UTC(),
	})

	s.Require().NotEmpty(s.PendingRedelegations())
	s.Require().NotEmpty(s.PendingUndelegations())
	s.T().Logf(
		"cleanup-case: seeded mature entries red=%d und=%d at %s",
		len(s.PendingRedelegations()), len(s.PendingUndelegations()), matureCompletion.Format(time.RFC3339),
	)

	s.Require().NoError(s.RunEndBlock())
	s.T().Logf("cleanup-case: after first EndBlock red=%d und=%d", len(s.PendingRedelegations()), len(s.PendingUndelegations()))

	s.Require().Empty(s.PendingRedelegations(), "expected pending redelegations to be cleaned up")
	s.Require().Empty(s.PendingUndelegations(), "expected pending undelegations to be cleaned up")

	// Second pass should stay empty.
	s.Require().NoError(s.RunEndBlock())
	s.Require().Empty(s.PendingRedelegations())
	s.Require().Empty(s.PendingUndelegations())
}

