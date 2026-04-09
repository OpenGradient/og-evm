package keeper

import (
	pooltypes "github.com/cosmos/evm/x/poolrebalancer/types"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
)

// Compile-time contract: vm keeper must satisfy poolrebalancer's minimal EVM interface.
var _ pooltypes.EVMKeeper = (*evmkeeper.Keeper)(nil)
