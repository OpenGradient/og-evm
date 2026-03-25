package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// HypOPGCollateralJSON are the compiled bytes of the HypOPGCollateral contract.
	//
	//go:embed solidity/bridge/HypOPGCollateral.json
	HypOPGCollateralJSON []byte

	// HypOPGCollateralContract is the compiled HypOPGCollateral contract.
	HypOPGCollateralContract evmtypes.CompiledContract
)

func init() {
	var err error
	if HypOPGCollateralContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		HypOPGCollateralJSON,
	); err != nil {
		panic(err)
	}
}
