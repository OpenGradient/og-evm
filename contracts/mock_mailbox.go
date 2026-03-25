package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// MockMailboxJSON are the compiled bytes of the MockMailbox contract.
	//
	//go:embed solidity/bridge/MockMailbox.json
	MockMailboxJSON []byte

	// MockMailboxContract is the compiled MockMailbox contract.
	MockMailboxContract evmtypes.CompiledContract
)

func init() {
	var err error
	if MockMailboxContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		MockMailboxJSON,
	); err != nil {
		panic(err)
	}
}
