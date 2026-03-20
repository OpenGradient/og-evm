package types

import (
	"fmt"
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
		LastBlockTime:           time.Time{},
		TotalPausedSeconds:      0,
		Activated:               false,
		Paused:                  false,
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
	if gs.TotalPausedSeconds < 0 {
		return fmt.Errorf("total_paused_seconds cannot be negative: %d", gs.TotalPausedSeconds)
	}
	if gs.Activated && gs.Params.HalfLifeSeconds == 0 {
		return fmt.Errorf("half_life_seconds must be set when activated")
	}
	return nil
}
