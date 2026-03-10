package evmd

import (
	"context"
	"fmt"
	"slices"

	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	// LegacyUpgradeName is kept to preserve compatibility with already-published
	// upgrade proposal names from earlier deployments.
	LegacyUpgradeName = "v0.5.0-to-v0.6.0"

	// UpgradeName defines the on-chain upgrade name for enabling missing EVM
	// preinstalls on an already-running network and optionally activating the
	// latest static precompile.
	UpgradeName = "v0.6.0-enable-missing-preinstalls"

	// OptionalStaticPrecompileAddress is enabled during the upgrade if missing.
	OptionalStaticPrecompileAddress = vmtypes.TEEPrecompileAddress
)

func (app EVMD) RegisterUpgradeHandlers() {
	// Legacy sample upgrade (kept intact for compatibility with existing plans).
	app.UpgradeKeeper.SetUpgradeHandler(
		LegacyUpgradeName,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			sdkCtx.Logger().Debug("this is a debug level message to test that verbose logging mode has properly been enabled during a chain upgrade")

			app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
				Description: "Example description",
				DenomUnits: []*banktypes.DenomUnit{
					{
						Denom:    "atest",
						Exponent: 0,
						Aliases:  nil,
					},
					{
						Denom:    "test",
						Exponent: 18,
						Aliases:  nil,
					},
				},
				Base:    "atest",
				Display: "test",
				Name:    "Test Token",
				Symbol:  "TEST",
				URI:     "example_uri",
				URIHash: "example_uri_hash",
			})

			// (Required for NON-18 denom chains *only)
			// Update EVM params to add Extended denom options
			// Ensure that this corresponds to the EVM denom
			// (typically the bond denom)
			evmParams := app.EVMKeeper.GetParams(sdkCtx)
			evmParams.ExtendedDenomOptions = &vmtypes.ExtendedDenomOptions{ExtendedDenom: "atest"}
			if err := app.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
				return nil, err
			}

			// Initialize EvmCoinInfo in the module store. Chains bootstrapped before
			// v0.5.0 binaries never stored this information (it lived only in process
			// globals), so migrating nodes would otherwise see an empty EvmCoinInfo on
			// upgrade.
			if err := app.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
				return nil, err
			}

			return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
		},
	)

	// New upgrade to backfill preinstalls and optionally activate new static precompile.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeName,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			sdkCtx := sdk.UnwrapSDKContext(ctx)

			// Optionally activate a new static precompile if it is available in
			// the binary and not yet active in chain state.
			evmParams := app.EVMKeeper.GetParams(sdkCtx)
			if !slices.Contains(evmParams.ActiveStaticPrecompiles, OptionalStaticPrecompileAddress) {
				if !slices.Contains(vmtypes.AvailableStaticPrecompiles, OptionalStaticPrecompileAddress) {
					return nil, fmt.Errorf("cannot activate static precompile %s: address not available in binary", OptionalStaticPrecompileAddress)
				}
				evmParams.ActiveStaticPrecompiles = append(evmParams.ActiveStaticPrecompiles, OptionalStaticPrecompileAddress)
				if err := app.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
					return nil, err
				}
			}

			// Add only missing default preinstalls. If an address has no code hash
			// but already has an account, abort to avoid mutating unknown state.
			missingPreinstalls := make([]vmtypes.Preinstall, 0, len(vmtypes.DefaultPreinstalls))
			for _, preinstall := range vmtypes.DefaultPreinstalls {
				addr := common.HexToAddress(preinstall.Address)
				codeHash := app.EVMKeeper.GetCodeHash(sdkCtx, addr)
				if !vmtypes.IsEmptyCodeHash(codeHash.Bytes()) {
					continue
				}

				accAddress := sdk.AccAddress(addr.Bytes())
				if acc := app.AccountKeeper.GetAccount(sdkCtx, accAddress); acc != nil {
					// Skip addresses that already have an account (e.g. a funded EOA)
					// rather than halting the chain
					sdkCtx.Logger().Warn(
						"skipping preinstall: account exists with empty code hash",
						"address", preinstall.Address,
					)
					continue
				}

				missingPreinstalls = append(missingPreinstalls, preinstall)
			}

			if len(missingPreinstalls) > 0 {
				if err := app.EVMKeeper.AddPreinstalls(sdkCtx, missingPreinstalls); err != nil {
					return nil, err
				}
			}

			// Ensure EvmCoinInfo is present in the module store. This is idempotent
			// and required because this upgrade path may run on chains that never
			// executed the LegacyUpgradeName handler (which also calls this).
			if err := app.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
				return nil, err
			}
			return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}

	if (upgradeInfo.Name == LegacyUpgradeName || upgradeInfo.Name == UpgradeName) && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{},
		}
		// configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}
}
