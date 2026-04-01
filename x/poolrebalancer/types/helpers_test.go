package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestParamsValidate_RejectsThresholdAbove10000(t *testing.T) {
	p := DefaultParams()
	p.RebalanceThresholdBp = 10001

	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "rebalance_threshold_bp")
}

func TestParamsValidate_RejectsNegativeMaxMovePerOp(t *testing.T) {
	p := DefaultParams()
	p.MaxMovePerOp = math.NewInt(-1)

	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_move_per_op")
}

func TestParamsValidate_RejectsInvalidPoolDelegatorAddress(t *testing.T) {
	p := DefaultParams()
	p.PoolDelegatorAddress = "not-a-valid-bech32"

	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "pool_delegator_address")
}

func TestParamsValidate_RejectsZeroMaxOpsPerBlock(t *testing.T) {
	p := DefaultParams()
	p.MaxOpsPerBlock = 0

	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_ops_per_block")
}

func TestParamsValidate_RejectsZeroMaxTargetValidators(t *testing.T) {
	p := DefaultParams()
	p.MaxTargetValidators = 0

	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_target_validators")
}
