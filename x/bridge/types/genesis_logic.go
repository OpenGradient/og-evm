package types

import (
	sdkmath "cosmossdk.io/math"
)

// DefaultGenesisState returns the default bridge genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:      DefaultParams(),
		TotalMinted: sdkmath.ZeroInt(),
		TotalBurned: sdkmath.ZeroInt(),
	}
}

// Validate performs basic genesis state validation.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if gs.TotalMinted.IsNegative() {
		return ErrInvalidAmount.Wrap("total_minted cannot be negative")
	}
	if gs.TotalBurned.IsNegative() {
		return ErrInvalidAmount.Wrap("total_burned cannot be negative")
	}
	return nil
}
