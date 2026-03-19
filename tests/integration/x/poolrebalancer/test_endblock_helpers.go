package poolrebalancer

import (
	mod "github.com/cosmos/evm/x/poolrebalancer"
	"time"
)

// RunEndBlock executes poolrebalancer EndBlock on the suite context.
// Tests in this package mutate keeper state directly on s.ctx, so we call
// EndBlocker directly to stay on that same context/store view.
func (s *KeeperIntegrationTestSuite) RunEndBlock() error {
	return mod.EndBlocker(s.ctx, s.poolKeeper)
}

// WithBlockTime moves the suite context clock without advancing the full network.
// It is used when we only need time-based maturity behavior.
func (s *KeeperIntegrationTestSuite) WithBlockTime(t time.Time) {
	s.ctx = s.ctx.WithBlockTime(t)
}

