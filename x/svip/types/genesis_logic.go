package types

import (
	"time"

	sdkmath "cosmossdk.io/math"
)

// DefaultGenesisState returns the default SVIP genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:                  DefaultParams(),
		TotalDistributed:        sdkmath.ZeroInt(),
		ActivationTime:          time.Time{},
		PoolBalanceAtActivation: sdkmath.ZeroInt(),
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.TotalDistributed.IsNegative() {
		return ErrPoolExhausted.Wrap("total_distributed cannot be negative")
	}
	return nil
}
