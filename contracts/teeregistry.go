package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// TEERegistryJSON are the compiled bytes of the TEERegistryContract
	//
	//go:embed solidity/TEERegistry.json
	TEERegistryJSON []byte

	// TEERegistryContract is the compiled TEERegistry contract
	TEERegistryContract evmtypes.CompiledContract
)

func init() {
	var err error
	if TEERegistryContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		TEERegistryJSON,
	); err != nil {
		panic(err)
	}
}
