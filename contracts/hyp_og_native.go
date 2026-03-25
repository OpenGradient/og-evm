package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// HypOGNativeJSON are the compiled bytes of the HypOGNative contract.
	//
	//go:embed solidity/bridge/HypOGNative.json
	HypOGNativeJSON []byte

	// HypOGNativeContract is the compiled HypOGNative contract.
	HypOGNativeContract evmtypes.CompiledContract
)

func init() {
	var err error
	if HypOGNativeContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		HypOGNativeJSON,
	); err != nil {
		panic(err)
	}
}
