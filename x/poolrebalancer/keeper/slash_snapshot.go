package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cosmos/evm/x/poolrebalancer/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
)

var errMissingPreviousBlockSlashedValidatorsSnapshot = errors.New("poolrebalancer: missing previous-block slashed validators snapshot")

func (k Keeper) setPreviousBlockSlashedValidators(ctx context.Context, validators map[string]struct{}) error {
	if k.transientKey == nil {
		return errors.New("poolrebalancer: transient key is nil")
	}

	keys := make([]string, 0, len(validators))
	for val := range validators {
		keys = append(keys, val)
	}
	sort.Strings(keys)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := sdkCtx.TransientStore(k.transientKey)
	store.Set(types.PreviousBlockSlashedValidatorsTransientKey, []byte(strings.Join(keys, "\n")))
	return nil
}

func (k Keeper) getPreviousBlockSlashedValidators(ctx context.Context) (map[string]struct{}, error) {
	if k.transientKey == nil {
		return nil, errors.New("poolrebalancer: transient key is nil")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bz := sdkCtx.TransientStore(k.transientKey).Get(types.PreviousBlockSlashedValidatorsTransientKey)
	if bz == nil {
		return nil, fmt.Errorf("%w (PreparePreviousBlockSlashedValidators not run?)", errMissingPreviousBlockSlashedValidatorsSnapshot)
	}
	if len(bz) == 0 {
		return map[string]struct{}{}, nil
	}

	lines := strings.Split(string(bz), "\n")
	out := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		if _, err := sdk.ValAddressFromBech32(line); err != nil {
			return nil, fmt.Errorf("invalid slashed validator address in snapshot: %w", err)
		}
		out[line] = struct{}{}
	}
	return out, nil
}

// getPreviousBlockSlashedValidatorsOrEmpty preserves pre-existing test/helper flows that invoke
// ProcessRebalance without a full BeginBlock pass by treating an absent snapshot as an empty set.
func (k Keeper) getPreviousBlockSlashedValidatorsOrEmpty(ctx context.Context) (map[string]struct{}, error) {
	slashed, err := k.getPreviousBlockSlashedValidators(ctx)
	if err != nil {
		if errors.Is(err, errMissingPreviousBlockSlashedValidatorsSnapshot) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	return slashed, nil
}

// PreparePreviousBlockSlashedValidators snapshots relevant validators with slash events at height blockHeight-1.
// It bounds work to validators relevant to poolrebalancer state: the pool delegator's current
// delegations plus the current target bonded validator set. Slash data is read directly from the
// distribution keeper via IterateValidatorSlashEventsBetween rather than going through query-layer
// pagination/response allocation.
func (k Keeper) PreparePreviousBlockSlashedValidators(ctx context.Context) error {
	if k.distrKeeper == nil {
		return k.setPreviousBlockSlashedValidators(ctx, map[string]struct{}{})
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	prevHeight := sdkCtx.BlockHeight() - 1
	if prevHeight <= 0 {
		return k.setPreviousBlockSlashedValidators(ctx, map[string]struct{}{})
	}

	candidates := make(map[string]struct{})

	poolDel, err := k.GetPoolDelegatorAddress(ctx)
	if err != nil {
		return err
	}
	if !poolDel.Empty() {
		delegations, err := k.stakingKeeper.GetDelegatorDelegations(ctx, poolDel, ^uint16(0))
		if err != nil {
			return err
		}
		for _, d := range delegations {
			if _, err := sdk.ValAddressFromBech32(d.ValidatorAddress); err != nil {
				return err
			}
			candidates[d.ValidatorAddress] = struct{}{}
		}
	}

	targetVals, err := k.GetTargetBondedValidators(ctx)
	if err != nil {
		return err
	}
	for _, val := range targetVals {
		candidates[val.String()] = struct{}{}
	}

	slashed := make(map[string]struct{})
	for valAddr := range candidates {
		val, err := sdk.ValAddressFromBech32(valAddr)
		if err != nil {
			return err
		}

		k.distrKeeper.IterateValidatorSlashEventsBetween(ctx, val, uint64(prevHeight), uint64(prevHeight), func(height uint64, _ distributiontypes.ValidatorSlashEvent) (stop bool) {
			slashed[valAddr] = struct{}{}
			return true
		})
	}

	return k.setPreviousBlockSlashedValidators(ctx, slashed)
}
