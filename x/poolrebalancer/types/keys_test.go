package types

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func TestModuleEVMAddress_Derivation(t *testing.T) {
	expected := common.BytesToAddress(authtypes.NewModuleAddress(ModuleName).Bytes())
	require.Equal(t, expected, ModuleEVMAddress)
}
