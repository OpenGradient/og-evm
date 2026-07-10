package types

import "fmt"

const (
	// MinHalfLifeSeconds is the shortest non-zero half-life (1 year). Anything shorter would
	// front-load emissions too aggressively.
	MinHalfLifeSeconds = 31536000
	// MaxHalfLifeSeconds caps the half-life at 100 years, past which the curve barely decays
	// and the fixed-point factor loses precision.
	MaxHalfLifeSeconds = 100 * MinHalfLifeSeconds
)

// DefaultParams returns the default SVIP module parameters.
func DefaultParams() Params {
	return Params{
		HalfLifeSeconds: 0, // set before activation
	}
}

// Validate checks that the SVIP parameters are valid. A zero half-life is permitted (the
// pre-activation / genesis default); any non-zero value must lie within [1 year, 100 years].
func (p Params) Validate() error {
	if p.HalfLifeSeconds < 0 {
		return fmt.Errorf("half_life_seconds cannot be negative: %d", p.HalfLifeSeconds)
	}
	if p.HalfLifeSeconds != 0 && (p.HalfLifeSeconds < MinHalfLifeSeconds || p.HalfLifeSeconds > MaxHalfLifeSeconds) {
		return fmt.Errorf("half_life_seconds must be 0 or within [%d, %d]: %d", MinHalfLifeSeconds, MaxHalfLifeSeconds, p.HalfLifeSeconds)
	}
	return nil
}
