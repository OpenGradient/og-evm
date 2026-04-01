package poolrebalancer

import (
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/testutil/integration/evm/network"
	testkeyring "github.com/cosmos/evm/testutil/keyring"

	poolrebalancerkeeper "github.com/cosmos/evm/x/poolrebalancer/keeper"
	poolrebalancertypes "github.com/cosmos/evm/x/poolrebalancer/types"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type KeeperIntegrationTestSuite struct {
	suite.Suite

	create  network.CreateEvmApp
	options []network.ConfigOption

	network    *network.UnitTestNetwork
	keyring    testkeyring.Keyring
	poolKeeper poolrebalancerkeeper.Keeper
	ctx        sdk.Context

	poolDel      sdk.AccAddress
	validators   []stakingtypes.Validator
	bondDenom    string
	unbondingSec time.Duration
	maxEntries   uint32
}

// NewKeeperIntegrationTestSuite wires app factory + optional network config for each test case.
func NewKeeperIntegrationTestSuite(create network.CreateEvmApp, options ...network.ConfigOption) *KeeperIntegrationTestSuite {
	return &KeeperIntegrationTestSuite{
		create:  create,
		options: options,
	}
}

func (s *KeeperIntegrationTestSuite) SetupTest() {
	if s.create == nil {
		panic("Create app must be set")
	}

	s.keyring = testkeyring.New(2)
	opts := []network.ConfigOption{
		network.WithPreFundedAccounts(s.keyring.GetAllAccAddrs()...),
	}
	opts = append(opts, s.options...)

	s.network = network.NewUnitTestNetwork(s.create, opts...)
	s.ctx = s.network.GetContext()

	// Keep unbonding short so maturity/cleanup tests run quickly.
	s.unbondingSec = 30 * time.Second
	s.maxEntries = 100

	s.configureStakingParamsForTests()
	s.configurePoolKeeper()
	s.captureBaselineInfo()
}

func (s *KeeperIntegrationTestSuite) configureStakingParamsForTests() {
	sk := s.network.App.GetStakingKeeper()
	sp, err := sk.GetParams(s.ctx)
	s.Require().NoError(err)
	sp.UnbondingTime = s.unbondingSec
	sp.MaxEntries = s.maxEntries
	s.Require().NoError(sk.SetParams(s.ctx, sp))
}

// configurePoolKeeper builds a keeper bound to the same stores as the app under test.
func (s *KeeperIntegrationTestSuite) configurePoolKeeper() {
	// This keeper shares module KV stores with the app; no mocked state.
	poolKey := s.network.App.GetKey(poolrebalancertypes.StoreKey)
	storeService := runtime.NewKVStoreService(poolKey)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	s.poolKeeper = poolrebalancerkeeper.NewKeeper(
		s.network.App.AppCodec(),
		storeService,
		s.network.App.GetStakingKeeper(),
		authority,
		nil,
		s.network.App.GetAccountKeeper(),
	)
}

// captureBaselineInfo caches common fixtures used by most test cases.
func (s *KeeperIntegrationTestSuite) captureBaselineInfo() {
	s.validators = s.network.GetValidators()
	s.Require().NotEmpty(s.validators, "network should initialize bonded validators")

	bondDenom, err := s.network.App.GetStakingKeeper().BondDenom(s.ctx)
	s.Require().NoError(err)
	s.bondDenom = bondDenom

	// UnitTestNetwork seeds delegations for the first test account; use it as pool delegator.
	s.poolDel = s.keyring.GetAccAddr(0)

	// Guard rail: no stake means rebalancer has nothing to do.
	_, total, err := s.poolKeeper.GetDelegatorStakeByValidator(s.ctx, s.poolDel)
	s.Require().NoError(err)
	s.Require().True(total.IsPositive(), "expected pool delegator stake to be > 0")
}

// NextBlock0 advances one block with no extra time offset.
func (s *KeeperIntegrationTestSuite) NextBlock0() {
	s.Require().NoError(s.network.NextBlockAfter(0))
}

// EnableRebalancer writes module params for the current test.
func (s *KeeperIntegrationTestSuite) EnableRebalancer(params poolrebalancertypes.Params) {
	s.Require().NoError(s.poolKeeper.SetParams(s.ctx, params))
}

// DisabledParams returns default params with pool delegator cleared.
func (s *KeeperIntegrationTestSuite) DisabledParams() poolrebalancertypes.Params {
	p := poolrebalancertypes.DefaultParams()
	p.PoolDelegatorAddress = ""
	return p
}

// DefaultEnabledParams returns a baseline enabled config with per-test overrides.
func (s *KeeperIntegrationTestSuite) DefaultEnabledParams(thresholdBP uint32, maxOpsPerBlock uint32, maxMovePerOp sdkmath.Int, useUndelegateFallback bool) poolrebalancertypes.Params {
	p := poolrebalancertypes.DefaultParams()
	p.PoolDelegatorAddress = s.poolDel.String()
	p.MaxTargetValidators = uint32(len(s.validators))
	p.RebalanceThresholdBp = thresholdBP
	p.MaxOpsPerBlock = maxOpsPerBlock
	p.MaxMovePerOp = maxMovePerOp
	p.UseUndelegateFallback = useUndelegateFallback
	return p
}
