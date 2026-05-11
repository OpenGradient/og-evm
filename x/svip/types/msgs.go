package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgActivate{}
	_ sdk.Msg = &MsgReactivate{}
	_ sdk.Msg = &MsgPause{}
	_ sdk.Msg = &MsgFundPool{}
)

func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	return m.Params.Validate()
}

func (m *MsgActivate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	return nil
}

func (m *MsgReactivate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	return nil
}

func (m *MsgPause) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	return nil
}

func (m *MsgFundPool) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Depositor); err != nil {
		return errorsmod.Wrap(err, "invalid depositor address")
	}
	coins := sdk.Coins(m.Amount)
	if !coins.IsValid() || coins.IsZero() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidCoins, "invalid fund amount")
	}
	return nil
}

// GetSignBytes implementations for EIP-712 support
func (m MsgUpdateParams) GetSignBytes() []byte  { return AminoCdc.MustMarshalJSON(&m) }
func (m MsgActivate) GetSignBytes() []byte      { return AminoCdc.MustMarshalJSON(&m) }
func (m MsgReactivate) GetSignBytes() []byte     { return AminoCdc.MustMarshalJSON(&m) }
func (m MsgPause) GetSignBytes() []byte          { return AminoCdc.MustMarshalJSON(&m) }
func (m MsgFundPool) GetSignBytes() []byte       { return AminoCdc.MustMarshalJSON(&m) }
