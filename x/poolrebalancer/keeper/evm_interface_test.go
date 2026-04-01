package keeper

import (
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	pooltypes "github.com/cosmos/evm/x/poolrebalancer/types"
)

// Compile-time contract: vm keeper must satisfy poolrebalancer's minimal EVM interface.
var _ pooltypes.EVMKeeper = (*evmkeeper.Keeper)(nil)
