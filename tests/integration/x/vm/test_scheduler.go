package vm

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/contracts"
	testconstants "github.com/cosmos/evm/testutil/constants"
	testutiltypes "github.com/cosmos/evm/testutil/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func (s *KeeperTestSuite) TestEVMSchedulerPokesConfiguredVault() {
	s.SetupTest()

	admin := s.Keyring.GetAddr(0)
	vaultAddr, err := s.Factory.DeployContract(
		s.Keyring.GetPrivKey(0),
		evmtypes.EvmTxArgs{},
		testutiltypes.ContractDeploymentData{
			Contract:        contracts.StakedBondVaultContract,
			ConstructorArgs: []interface{}{admin, common.HexToAddress(testconstants.WEVMOSContractMainnet), "Staked Bond", "stBOND"},
		},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())

	params := s.Network.App.GetEVMKeeper().GetParams(s.Network.GetContext())
	params.Scheduler = evmtypes.EVMSchedulerParams{
		Enabled:        true,
		TargetContract: vaultAddr.Hex(),
		GasCap:         1_000_000,
		MaxOps:         evmtypes.SchedulerPokeStepCount,
		CadenceBlocks:  1,
	}
	s.Require().NoError(s.Network.App.GetEVMKeeper().SetParams(s.Network.GetContext(), params))
	s.Network.App.GetEVMKeeper().RunScheduler(s.Network.GetContext())

	data, err := contracts.StakedBondVaultContract.ABI.Pack("pokeCount")
	s.Require().NoError(err)
	res, err := s.Network.App.GetEVMKeeper().CallEVMWithData(
		s.Network.GetContext(),
		erc20types.ModuleAddress,
		&vaultAddr,
		data,
		false,
		nil,
	)
	s.Require().NoError(err)
	out, err := contracts.StakedBondVaultContract.ABI.Unpack("pokeCount", res.Ret)
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(1).Cmp(out[0].(*big.Int)), "events: %v", s.Network.GetContext().EventManager().Events())
}

// TestEVMSchedulerRevertDoesNotHaltBlock exercises an actual reverting poke: the vault is
// deployed with its poke paused, so the scheduler's poke(uint256) call reverts. The scheduler
// should catch that revert, emit success=false, and leave the vault's pokeCount untouched,
// without halting the block.
func (s *KeeperTestSuite) TestEVMSchedulerRevertDoesNotHaltBlock() {
	s.SetupTest()

	vault := s.deployStakedBondVault()

	// Pause the vault's poke so any scheduler poke reverts with "P_PAUSED".
	_, err := s.executeVaultTx(s.Keyring.GetPrivKey(0), vault, nil, "setPaused", false, false, true)
	s.Require().NoError(err)

	ctx := s.Network.GetContext()
	params := s.Network.App.GetEVMKeeper().GetParams(ctx)
	params.Scheduler = evmtypes.EVMSchedulerParams{
		Enabled:        true,
		TargetContract: vault.Hex(),
		GasCap:         1_000_000,
		MaxOps:         evmtypes.SchedulerPokeStepCount,
		CadenceBlocks:  1,
	}
	s.Require().NoError(s.Network.App.GetEVMKeeper().SetParams(ctx, params))

	// The poke reverts inside RunScheduler; it should be caught rather than halting the block.
	s.Require().NotPanics(func() {
		s.Network.App.GetEVMKeeper().RunScheduler(ctx)
	})

	// The scheduler should have recorded a failed poke.
	attrs := map[string]string{}
	for _, event := range ctx.EventManager().Events() {
		if event.Type != "evm_scheduler" {
			continue
		}
		for _, attr := range event.Attributes {
			attrs[attr.Key] = attr.Value
		}
	}
	s.Require().Equal("false", attrs["success"], "events: %v", ctx.EventManager().Events())

	// pokeCount should still be 0, since the reverting poke committed nothing.
	s.Require().Zero(s.vaultBig(vault, "pokeCount").Sign())
}

func (s *KeeperTestSuite) TestEVMSchedulerCorruptParamsDoNotHaltBlock() {
	s.SetupTest()

	ctx := s.Network.GetContext()
	store := ctx.KVStore(s.Network.App.GetKey(evmtypes.StoreKey))
	store.Set(evmtypes.KeyPrefixParams, []byte{0xff})

	s.Require().NotPanics(func() {
		s.Network.App.GetEVMKeeper().RunScheduler(ctx)
	})

	attrs := map[string]string{}
	for _, event := range ctx.EventManager().Events() {
		if event.Type != "evm_scheduler" {
			continue
		}
		for _, attr := range event.Attributes {
			attrs[attr.Key] = attr.Value
		}
	}

	s.Require().Equal("", attrs["target_contract"])
	s.Require().Equal("false", attrs["success"])
	s.Require().Equal("0", attrs["gas_used"])
	s.Require().True(strings.Contains(attrs["error"], "panic"), "events: %v", ctx.EventManager().Events())
}
