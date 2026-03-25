package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

var (
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgSetAuthorizedContract{}
)

func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	return m.Params.Validate()
}

func (m *MsgSetAuthorizedContract) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return errorsmod.Wrap(err, "invalid authority address")
	}
	if m.ContractAddress != "" && !common.IsHexAddress(m.ContractAddress) {
		return errorsmod.Wrap(ErrInvalidAddress, "invalid contract address")
	}
	return nil
}

// GetSignBytes implements the LegacyMsg interface for EIP-712 support.
func (m MsgUpdateParams) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}

// GetSignBytes implements the LegacyMsg interface for EIP-712 support.
func (m MsgSetAuthorizedContract) GetSignBytes() []byte {
	return AminoCdc.MustMarshalJSON(&m)
}
