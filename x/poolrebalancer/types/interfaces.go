package types

import (
	"context"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper defines the subset of staking keeper methods used by poolrebalancer.
type StakingKeeper interface {
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
	GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	GetDelegation(ctx context.Context, delegatorAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.Delegation, error)
	BeginRedelegation(ctx context.Context, delAddr sdk.AccAddress, valSrcAddr, valDstAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (completionTime time.Time, err error)
	Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (completionTime time.Time, amount sdkmath.Int, err error)
	UnbondingTime(ctx context.Context) (time.Duration, error)
	BondDenom(ctx context.Context) (string, error)
}
