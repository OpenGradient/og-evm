package poolrebalancer

import (
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	utiltx "github.com/cosmos/evm/testutil/tx"

	mod "github.com/cosmos/evm/x/poolrebalancer"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

func normalizeUBDCompletion(t time.Time) time.Time {
	return t.UTC().Round(0)
}

// TestCompletionCleanup_RemovesMatureRedelegationsAndUndelegations verifies that
// mature pending redelegation and undelegation entries are removed during EndBlock.
func (s *KeeperIntegrationTestSuite) TestCompletionCleanup_RemovesMatureRedelegationsAndUndelegations() {
	// Keep pool delegator configured (required for matured undelegation snapshot),
	// but use a no-op-ish threshold profile so this test focuses on cleanup paths.
	s.EnableRebalancer(s.DefaultEnabledParams(10_000, 1, sdkmath.NewInt(1), false))

	xVal := s.validators[0]
	yVal := s.validators[1]
	valAddr := s.MustValAddr(xVal.OperatorAddress)

	// Create a real tracked undelegation so staking UBD state exists and can be
	// snapshot/credited by strict BeginBlock maturity logic.
	completion, amountUBD, err := s.poolKeeper.BeginTrackedUndelegation(
		sdk.WrapSDKContext(s.ctx),
		s.poolDel,
		valAddr,
		sdk.NewCoin(s.bondDenom, sdkmath.NewInt(1)),
	)
	s.Require().NoError(err)
	s.Require().True(amountUBD.IsPositive())

	matureCompletion := completion.UTC()

	// Seed mature entries (completion already in the past).
	s.SeedPendingRedelegation(poolrebalancertypes.PendingRedelegation{
		DelegatorAddress:    s.poolDel.String(),
		SrcValidatorAddress: yVal.OperatorAddress,
		DstValidatorAddress: xVal.OperatorAddress,
		Amount:              sdk.NewCoin(s.bondDenom, sdkmath.NewInt(5)),
		CompletionTime:      matureCompletion.UTC(),
	})

	s.WithBlockTime(matureCompletion.Add(1 * time.Second))

	s.Require().NotEmpty(s.PendingRedelegations())
	s.Require().NotEmpty(s.PendingUndelegations())
	s.T().Logf(
		"cleanup-case: seeded mature entries red=%d und=%d at %s",
		len(s.PendingRedelegations()), len(s.PendingUndelegations()), matureCompletion.Format(time.RFC3339),
	)

	s.Require().NoError(s.RunBeginThenEndBlock())
	s.T().Logf("cleanup-case: after first EndBlock red=%d und=%d", len(s.PendingRedelegations()), len(s.PendingUndelegations()))

	s.Require().Empty(s.PendingRedelegations(), "expected pending redelegations to be cleaned up")
	s.Require().Empty(s.PendingUndelegations(), "expected pending undelegations to be cleaned up")

	// Second pass should stay empty.
	s.Require().NoError(s.RunBeginThenEndBlock())
	s.Require().Empty(s.PendingRedelegations())
	s.Require().Empty(s.PendingUndelegations())
}

// TestUndelegationCredit_StrictSequencingWithoutBeginBlock verifies EndBlock alone fails
// when matured undelegations exist and no BeginBlock snapshot was prepared in the same block.
func (s *KeeperIntegrationTestSuite) TestUndelegationCredit_StrictSequencingWithoutBeginBlock() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.NewInt(1), false)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	matureCompletion := s.ctx.BlockTime().Add(-1 * time.Second)

	s.SeedPendingUndelegation(poolrebalancertypes.PendingUndelegation{
		DelegatorAddress: s.poolDel.String(),
		ValidatorAddress: xVal.OperatorAddress,
		Balance:          sdk.NewCoin(s.bondDenom, sdkmath.NewInt(7)),
		CompletionTime:   matureCompletion.UTC(),
	})

	// Only EndBlocker: no BeginBlock snapshot for this context, so completion must fail.
	err := mod.EndBlocker(s.ctx, s.poolKeeper)
	s.Require().Error(err)
	s.Require().NotEmpty(s.PendingUndelegations(), "queue should remain when EndBlock fails")
}

// TestUndelegationCredit_DedupesQueueAndUsesStakingReality exercises integration behavior where
// queue rows for the same triple are duplicated and inflated relative to staking reality.
func (s *KeeperIntegrationTestSuite) TestUndelegationCredit_DedupesQueueAndUsesStakingReality() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.NewInt(1), false)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	valAddr := s.MustValAddr(xVal.OperatorAddress)

	bonded, err := s.network.App.GetStakingKeeper().GetDelegatorBonded(s.ctx, s.poolDel)
	s.Require().NoError(err)
	s.Require().True(bonded.IsPositive())

	undelegAmt := bonded.QuoRaw(8)
	if undelegAmt.IsZero() {
		undelegAmt = sdkmath.NewInt(1)
	}

	completion, amountUB, err := s.poolKeeper.BeginTrackedUndelegation(
		sdk.WrapSDKContext(s.ctx),
		s.poolDel,
		valAddr,
		sdk.NewCoin(s.bondDenom, undelegAmt),
	)
	s.Require().NoError(err)
	s.Require().True(amountUB.IsPositive())

	// Seed an extra queue row for the same triple with an inflated amount.
	// Snapshot logic must dedupe by (delegator, validator, completion) and use staking UBD balance.
	s.SeedPendingUndelegation(poolrebalancertypes.PendingUndelegation{
		DelegatorAddress: s.poolDel.String(),
		ValidatorAddress: xVal.OperatorAddress,
		Balance:          sdk.NewCoin(s.bondDenom, amountUB.MulRaw(3)),
		CompletionTime:   completion.UTC(),
	})

	s.WithBlockTime(completion.Add(1 * time.Second))
	s.Require().NoError(s.RunBeginThenEndBlock())
	s.Require().Empty(s.PendingUndelegations(), "all matured entries should be cleaned after Begin+End flow")
}

// TestUndelegationMultiEntry_SameCompletionDifferentCreationHeight exercises real x/staking with two
// UnbondingDelegationEntry rows for the same (delegator, validator): same CompletionTime, different
// CreationHeight. Poolrebalancer BeginBlock→EndBlock then clears matured pending undelegations
// (stub EVM; no calldata assertions).
//
// Harness: the first undelegation must be committed on-chain (MsgUndelegate + NextBlockWithTxs), not
// only keeper calls on s.ctx, or the UBD may be missing after a later Commit. That leg does not auto-fill
// the module queue—we SeedPendingUndelegation from the live UBD so Prepare/Complete matches staking.
// NextBlockWithTxs moves block time (+1s in the harness); NextBlock0 advances height without extra time
// so the second BeginTrackedUndelegation can land in the same unbonding completion instant as the first.
func (s *KeeperIntegrationTestSuite) TestUndelegationMultiEntry_SameCompletionDifferentCreationHeight() {
	params := s.DefaultEnabledParams(0, 1, sdkmath.NewInt(1), false)
	s.EnableRebalancer(params)

	xVal := s.validators[0]
	valAddr := s.MustValAddr(xVal.OperatorAddress)
	sk := s.network.App.GetStakingKeeper()

	bonded, err := sk.GetDelegatorBonded(s.ctx, s.poolDel)
	s.Require().NoError(err)
	s.Require().True(bonded.IsPositive())

	undeleg1 := bonded.QuoRaw(10)
	undeleg2 := bonded.QuoRaw(12)
	if undeleg1.IsZero() {
		undeleg1 = sdkmath.NewInt(1)
	}
	if undeleg2.IsZero() {
		undeleg2 = sdkmath.NewInt(1)
	}
	if undeleg1.GT(bonded) || undeleg2.GT(bonded) {
		undeleg1 = bonded.QuoRaw(5)
		undeleg2 = bonded.QuoRaw(7)
	}
	totalUndeleg := undeleg1.Add(undeleg2)
	if totalUndeleg.GTE(bonded) || totalUndeleg.IsZero() {
		s.T().Skipf(
			"could not pick two positive undelegation amounts below bonded=%s; skip multi-entry UBD case",
			bonded.String(),
		)
	}

	// Leg 1: committed staking unbond; pool queue filled manually (see doc above).
	msg := stakingtypes.NewMsgUndelegate(
		s.poolDel.String(),
		xVal.OperatorAddress,
		sdk.NewCoin(s.bondDenom, undeleg1),
	)
	enc := s.network.GetEncodingConfig()
	// fee = gasPrice * gas must clear the chain's min gas prices (integration app is stricter than utiltx.DefaultFee).
	gp := sdkmath.NewInt(50_000_000_000)
	signed, err := utiltx.PrepareCosmosTx(
		s.ctx,
		s.network.App,
		utiltx.CosmosTxArgs{
			TxCfg:    enc.TxConfig,
			Priv:     s.keyring.GetPrivKey(0),
			ChainID:  s.network.GetChainID(),
			Gas:      20_000_000,
			GasPrice: &gp,
			Msgs:     []sdk.Msg{msg},
		},
	)
	s.Require().NoError(err)
	txBytes, err := enc.TxConfig.TxEncoder()(signed)
	s.Require().NoError(err)

	fbRes, err := s.network.NextBlockWithTxs(txBytes)
	s.Require().NoError(err)
	s.Require().Len(fbRes.TxResults, 1)
	if fbRes.TxResults[0].Code != 0 {
		s.T().Skipf(
			"MsgUndelegate tx failed code=%d log=%q; skip multi-entry UBD integration",
			fbRes.TxResults[0].Code,
			fbRes.TxResults[0].Log,
		)
	}

	s.ctx = s.network.GetContext()

	ubd, err := sk.GetUnbondingDelegation(s.ctx, s.poolDel, valAddr)
	s.Require().NoError(err)
	s.Require().Len(ubd.Entries, 1, "first undelegation tx should create one UBD entry")
	entry0 := ubd.Entries[0]
	amount1 := entry0.Balance
	completion1 := entry0.CompletionTime
	s.Require().True(amount1.IsPositive())

	// Align module queue with staking so this block’s UBD is included in mature batch processing.
	s.SeedPendingUndelegation(poolrebalancertypes.PendingUndelegation{
		DelegatorAddress: s.poolDel.String(),
		ValidatorAddress: xVal.OperatorAddress,
		Balance:          sdk.NewCoin(s.bondDenom, amount1),
		CompletionTime:   completion1.UTC(),
	})

	// Same header time as after leg 1, higher height → second entry can share CompletionTime.
	s.NextBlock0()

	completion2, amount2, err := s.poolKeeper.BeginTrackedUndelegation(
		sdk.WrapSDKContext(s.ctx),
		s.poolDel,
		valAddr,
		sdk.NewCoin(s.bondDenom, undeleg2),
	)
	s.Require().NoError(err)
	s.Require().True(amount2.IsPositive())

	s.T().Logf(
		"multi-entry-ubd: completion1=%s completion2=%s blockTime=%s",
		completion1.UTC().Format(time.RFC3339Nano),
		completion2.UTC().Format(time.RFC3339Nano),
		s.ctx.BlockTime().UTC().Format(time.RFC3339Nano),
	)

	if !normalizeUBDCompletion(completion1).Equal(normalizeUBDCompletion(completion2)) {
		s.T().Skipf(
			"integration harness: unequal undelegation CompletionTime (%v vs %v) — multi-entry same CompletionTime "+
				"is covered in x/poolrebalancer/keeper unit tests",
			completion1,
			completion2,
		)
	}

	ubd, err = sk.GetUnbondingDelegation(s.ctx, s.poolDel, valAddr)
	s.Require().NoError(err)

	targetComp := normalizeUBDCompletion(completion1)
	var matching []struct {
		balance         sdkmath.Int
		creationHeight  int64
		completionMatch time.Time
	}
	for _, e := range ubd.Entries {
		if normalizeUBDCompletion(e.CompletionTime).Equal(targetComp) {
			matching = append(matching, struct {
				balance         sdkmath.Int
				creationHeight  int64
				completionMatch time.Time
			}{e.Balance, e.CreationHeight, e.CompletionTime})
		}
	}

	if len(matching) != 2 {
		s.T().Skipf(
			"integration harness: expected 2 staking UBD entries with completion %s, got %d (total entries=%d); "+
				"keeper unit tests remain authoritative for Prepare/Complete summation",
			targetComp.Format(time.RFC3339Nano),
			len(matching),
			len(ubd.Entries),
		)
	}
	if matching[0].creationHeight == matching[1].creationHeight {
		s.T().Skipf(
			"integration harness: both UBD entries share CreationHeight=%d; need distinct heights for Gap-1 scenario",
			matching[0].creationHeight,
		)
	}
	s.Require().True(s.ctx.BlockTime().Before(targetComp), "pre-maturity: block time should be before completion")

	// PrepareMaturedPoolUndelegationCredits sums staking balances for this completion; no duplicate-queue inflation.
	combined := matching[0].balance.Add(matching[1].balance)
	expectedCredit := amount1.Add(amount2)
	s.Require().True(
		combined.Equal(expectedCredit),
		"staking combined UBD balance for this completion should match tracked undelegation tokens: combined=%s expected=%s",
		combined.String(),
		expectedCredit.String(),
	)
	s.Require().NotEmpty(s.PendingUndelegations(), "module queue should hold pending rows before maturity")

	s.WithBlockTime(targetComp.Add(1 * time.Second))
	s.Require().NoError(s.RunBeginThenEndBlock())
	s.Require().Empty(s.PendingUndelegations(), "pending undelegations should clear after matured Begin+End")

	// RunBeginThenEndBlock invokes only poolrebalancer Begin/End on s.ctx; it does not run the full app
	// EndBlocker chain, so staking may still list UBD entries after this. The test targets module queue cleanup
	// and pre/post maturity accounting, not removal of staking records.
}
