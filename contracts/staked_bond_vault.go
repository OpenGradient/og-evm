package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// StakedBondVaultJSON are the compiled bytes of the StakedBondVault contract.
	//
	//go:embed solidity/StakedBondVault.json
	StakedBondVaultJSON []byte

	// StakedBondVaultContract is the compiled ERC4626-style staking vault.
	StakedBondVaultContract evmtypes.CompiledContract
)

func init() {
	var err error
	if StakedBondVaultContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		StakedBondVaultJSON,
	); err != nil {
		panic(err)
	}
}
