package types

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/common"
)

// DefaultParams returns the default bridge module parameters.
func DefaultParams() Params {
	return Params{
		AuthorizedContract: "",
		HyperlaneMailbox:   "",
		BaseDomainId:       8453,
		Enabled:            false,
		MaxTransferAmount:  math.ZeroInt(),
	}
}

// Validate checks that the bridge parameters are valid.
func (p Params) Validate() error {
	if p.AuthorizedContract != "" && !common.IsHexAddress(p.AuthorizedContract) {
		return fmt.Errorf("invalid authorized_contract address: %s", p.AuthorizedContract)
	}
	if p.HyperlaneMailbox != "" && !common.IsHexAddress(p.HyperlaneMailbox) {
		return fmt.Errorf("invalid hyperlane_mailbox address: %s", p.HyperlaneMailbox)
	}
	if p.MaxTransferAmount.IsNegative() {
		return fmt.Errorf("max_transfer_amount cannot be negative: %s", p.MaxTransferAmount)
	}
	return nil
}
