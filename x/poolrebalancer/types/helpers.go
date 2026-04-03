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

// Validate validates a pending redelegation record (e.g. for genesis import).
func (pr PendingRedelegation) Validate() error {
	if _, err := sdk.AccAddressFromBech32(pr.DelegatorAddress); err != nil {
		return fmt.Errorf("invalid delegator_address: %w", err)
	}
	srcVal, err := sdk.ValAddressFromBech32(pr.SrcValidatorAddress)
	if err != nil {
		return fmt.Errorf("invalid src_validator_address: %w", err)
	}
	dstVal, err := sdk.ValAddressFromBech32(pr.DstValidatorAddress)
	if err != nil {
		return fmt.Errorf("invalid dst_validator_address: %w", err)
	}
	if srcVal.Equals(dstVal) {
		return fmt.Errorf("src_validator_address and dst_validator_address must differ")
	}
	if err := pr.Amount.Validate(); err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}
	if !pr.Amount.IsPositive() {
		return fmt.Errorf("amount must be positive")
	}
	if pr.CompletionTime.IsZero() {
		return fmt.Errorf("completion_time must be set")
	}
	return nil
}

// Validate validates a pending undelegation record (e.g. for genesis import).
func (pu PendingUndelegation) Validate() error {
	if _, err := sdk.AccAddressFromBech32(pu.DelegatorAddress); err != nil {
		return fmt.Errorf("invalid delegator_address: %w", err)
	}
	if _, err := sdk.ValAddressFromBech32(pu.ValidatorAddress); err != nil {
		return fmt.Errorf("invalid validator_address: %w", err)
	}
	if err := pu.Balance.Validate(); err != nil {
		return fmt.Errorf("invalid balance: %w", err)
	}
	if !pu.Balance.IsPositive() {
		return fmt.Errorf("balance must be positive")
	}
	if pu.CompletionTime.IsZero() {
		return fmt.Errorf("completion_time must be set")
	}
	return nil
}

// Validate validates the genesis state.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	for i, pr := range gs.PendingRedelegations {
		if err := pr.Validate(); err != nil {
			return fmt.Errorf("pending_redelegations[%d]: %w", i, err)
		}
	}
	for i, pu := range gs.PendingUndelegations {
		if err := pu.Validate(); err != nil {
			return fmt.Errorf("pending_undelegations[%d]: %w", i, err)
		}
	}
	return nil
}
