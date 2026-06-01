package utils

import (
	"fmt"

	evmaddress "github.com/cosmos/evm/encoding/address"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func GovAuthority() string {
	authority, err := evmaddress.NewEvmCodec(sdk.GetConfig().GetBech32AccountAddrPrefix()).
		BytesToString(authtypes.NewModuleAddress(govtypes.ModuleName))
	if err != nil {
		panic(fmt.Errorf("failed to encode gov authority address: %w", err))
	}

	return authority
}
