package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino = codec.NewLegacyAmino()

	// ModuleCdc is a module-local codec helper.
	// Most state and service encoding uses the app's configured codec; this exists mainly for JSON contexts.
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())

	// AminoCdc supports amino JSON for legacy msg encoding.
	AminoCdc = codec.NewAminoCodec(amino) //nolint:staticcheck
)

const (
	updateParamsName = "cosmos/evm/x/poolrebalancer/MsgUpdateParams"
)

func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces registers the module's interfaces with the registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgUpdateParams{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the module's types with the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, updateParamsName, nil)
}
