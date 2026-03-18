package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	AminoCdc  = codec.NewAminoCodec(amino)
)

const (
	updateParamsName = "cosmos/evm/svip/MsgUpdateParams"
	activateName     = "cosmos/evm/svip/MsgActivate"
	reactivateName   = "cosmos/evm/svip/MsgReactivate"
	pauseName        = "cosmos/evm/svip/MsgPause"
	fundPoolName     = "cosmos/evm/svip/MsgFundPool"
)

func init() {
	RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

// RegisterInterfaces registers the SVIP module's interface types.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgActivate{},
		&MsgReactivate{},
		&MsgPause{},
		&MsgFundPool{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// RegisterLegacyAminoCodec registers the SVIP module's types on the given LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, updateParamsName, nil)
	cdc.RegisterConcrete(&MsgActivate{}, activateName, nil)
	cdc.RegisterConcrete(&MsgReactivate{}, reactivateName, nil)
	cdc.RegisterConcrete(&MsgPause{}, pauseName, nil)
	cdc.RegisterConcrete(&MsgFundPool{}, fundPoolName, nil)
}
