package poolrebalancer

import (
	"time"

	mod "github.com/cosmos/evm/x/poolrebalancer"
)

// RunEndBlock runs only poolrebalancer EndBlocker on s.ctx (same store view as direct keeper tests).
//
// Prefer RunBeginThenEndBlock for normal cases: CompletePendingUndelegations needs the transient
// credit sum from BeginBlock whenever there are matured pending undelegation queue entries.
// Use RunEndBlock only to assert EndBlock-only failure (missing snapshot) or when you know there are
// no matured undelegation batches for ctx.BlockTime().
func (s *KeeperIntegrationTestSuite) RunEndBlock() error {
	return mod.EndBlocker(s.ctx, s.poolKeeper)
}

// RunBeginThenEndBlock runs poolrebalancer BeginBlocker then EndBlocker on s.ctx.
// This matches production ABCI ordering and must be used when tests may have matured undelegations
// (completion time <= block time), including after WithBlockTime jumps.
//
// If only redelegations mature (no undelegation queue rows), BeginBlock is a no-op for credits;
// CompletePendingUndelegations returns early when there are no matured undelegation batches.
func (s *KeeperIntegrationTestSuite) RunBeginThenEndBlock() error {
	if err := mod.BeginBlocker(s.ctx, s.poolKeeper); err != nil {
		return err
	}
	return mod.EndBlocker(s.ctx, s.poolKeeper)
}

// WithBlockTime moves the suite context clock without advancing the full network.
// It is used when we only need time-based maturity behavior.
func (s *KeeperIntegrationTestSuite) WithBlockTime(t time.Time) {
	s.ctx = s.ctx.WithBlockTime(t)
}

// WithBlockHeight moves the suite context height without refreshing from the network.
// It is used when tests need previous-block semantics over the same in-memory store view.
func (s *KeeperIntegrationTestSuite) WithBlockHeight(h int64) {
	s.ctx = s.ctx.WithBlockHeight(h)
}
