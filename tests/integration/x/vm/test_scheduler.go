package vm

import (
	"math/big"
	"strings"

	"github.com/cosmos/evm/contracts"
	testcontracts "github.com/cosmos/evm/precompiles/testutil/contracts"
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
			ConstructorArgs: []interface{}{admin, "Staked Bond", "stBOND"},
		},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())

	params := s.Network.App.GetEVMKeeper().GetParams(s.Network.GetContext())
	params.Scheduler = evmtypes.EVMSchedulerParams{
		Enabled:        true,
		TargetContract: vaultAddr.Hex(),
		GasCap:         1_000_000,
		MaxOps:         7,
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

func (s *KeeperTestSuite) TestEVMSchedulerRevertDoesNotHaltBlock() {
	s.SetupTest()

	counter, err := testcontracts.LoadCounterContract()
	s.Require().NoError(err)
	counterAddr, err := s.Factory.DeployContract(
		s.Keyring.GetPrivKey(0),
		evmtypes.EvmTxArgs{},
		testutiltypes.ContractDeploymentData{Contract: counter},
	)
	s.Require().NoError(err)
	s.Require().NoError(s.Network.NextBlock())

	params := s.Network.App.GetEVMKeeper().GetParams(s.Network.GetContext())
	params.Scheduler = evmtypes.EVMSchedulerParams{
		Enabled:        true,
		TargetContract: counterAddr.Hex(),
		GasCap:         500_000,
		MaxOps:         1,
		CadenceBlocks:  1,
	}
	s.Require().NoError(s.Network.App.GetEVMKeeper().SetParams(s.Network.GetContext(), params))
	s.Network.App.GetEVMKeeper().RunScheduler(s.Network.GetContext())

	data, err := counter.ABI.Pack("getCounter")
	s.Require().NoError(err)
	res, err := s.Network.App.GetEVMKeeper().CallEVMWithData(
		s.Network.GetContext(),
		erc20types.ModuleAddress,
		&counterAddr,
		data,
		false,
		nil,
	)
	s.Require().NoError(err)
	out, err := counter.ABI.Unpack("getCounter", res.Ret)
	s.Require().NoError(err)
	s.Require().Equal(0, big.NewInt(0).Cmp(out[0].(*big.Int)))
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
