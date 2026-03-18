package types

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		PoolDelegatorAddress:  "", // empty = rebalancer disabled until set
		MaxTargetValidators:   uint32(30),
		RebalanceThresholdBp:  uint32(50), // 0.5%
		MaxOpsPerBlock:        uint32(5),
		MaxMovePerOp:          math.ZeroInt(), // 0 means no cap
		UseUndelegateFallback: true,
	}
}

// Validate validates the params.
func (p Params) Validate() error {
	if p.PoolDelegatorAddress != "" {
		if _, err := sdk.AccAddressFromBech32(p.PoolDelegatorAddress); err != nil {
			return fmt.Errorf("invalid pool_delegator_address: %w", err)
		}
	}
	if p.MaxTargetValidators == 0 {
		return fmt.Errorf("max_target_validators must be positive")
	}
	if p.RebalanceThresholdBp > 10_000 {
		return fmt.Errorf("rebalance_threshold_bp cannot exceed 10000")
	}
	if p.MaxOpsPerBlock == 0 {
		return fmt.Errorf("max_ops_per_block must be positive")
	}
	if !p.MaxMovePerOp.IsNil() && p.MaxMovePerOp.IsNegative() {
		return fmt.Errorf("max_move_per_op cannot be negative")
	}
	return nil
}

// DefaultGenesisState returns a default genesis state.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

// Validate validates the genesis state.
func (gs *GenesisState) Validate() error {
	return gs.Params.Validate()
}
