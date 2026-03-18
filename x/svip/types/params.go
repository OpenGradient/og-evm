package types

import "fmt"

// DefaultParams returns the default SVIP module parameters.
func DefaultParams() Params {
	return Params{
		Activated:       false,
		Paused:          false,
		HalfLifeSeconds: 0, // set on activation
	}
}

// Validate checks that the SVIP parameters are valid.
func (p Params) Validate() error {
	if p.HalfLifeSeconds < 0 {
		return fmt.Errorf("half_life_seconds cannot be negative: %d", p.HalfLifeSeconds)
	}
	if p.Activated && p.HalfLifeSeconds == 0 {
		return fmt.Errorf("half_life_seconds must be set when activated")
	}
	return nil
}
