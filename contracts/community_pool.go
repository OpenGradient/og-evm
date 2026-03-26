package contracts

import (
	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// LoadCommunityPool loads the compiled CommunityPool contract artifact.
func LoadCommunityPool() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("solidity/pool/CommunityPool.json")
}

