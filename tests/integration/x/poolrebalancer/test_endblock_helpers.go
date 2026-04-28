package poolrebalancer

import (
	"time"

	mod "github.com/cosmos/evm/x/poolrebalancer"
)

// RunEndBlock runs only poolrebalancer EndBlocker on s.ctx (same store view as direct keeper tests).
//
// Prefer RunBeginThenEndBlock for normal cases to match production ordering.
// Use RunEndBlock only to assert EndBlock-only failure (missing slash snapshot) or for
// focused cases where BeginBlock effects are intentionally excluded.
func (s *KeeperIntegrationTestSuite) RunEndBlock() error {
	return mod.EndBlocker(s.ctx, s.poolKeeper)
}

// RunBeginThenEndBlock runs poolrebalancer BeginBlocker then EndBlocker on s.ctx.
// This matches production ABCI ordering and should be the default for integration cases.
// It is required for tests that rely on previous-block slash snapshot semantics.
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
