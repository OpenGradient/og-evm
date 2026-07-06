package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/evm/x/svip/types"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// msgServer implements the SVIP MsgServer interface.
type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the SVIP MsgServer interface.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = msgServer{}

// UpdateParams updates the SVIP module parameters. Governance-only.
func (s msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Governance guardrails
	current := s.GetParams(ctx)

	// After activation, half_life must remain strictly positive.
	if s.GetActivated(ctx) && msg.Params.HalfLifeSeconds <= 0 {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "half_life_seconds must be > 0 while module is activated")
	}

	if s.GetActivated(ctx) && current.HalfLifeSeconds > 0 && msg.Params.HalfLifeSeconds > 0 {
		// Reject a half-life change of more than 50% in either direction. The division form
		// avoids the int64 overflow the cross-multiplied 2*newHL vs 3*oldHL form can hit.
		oldHL := current.HalfLifeSeconds
		newHL := msg.Params.HalfLifeSeconds
		if newHL < oldHL/2 || newHL > oldHL+oldHL/2 {
			return nil, types.ErrHalfLifeChange
		}
	}
	if msg.Params.HalfLifeSeconds > 0 &&
		(msg.Params.HalfLifeSeconds < types.MinHalfLifeSeconds || msg.Params.HalfLifeSeconds > types.MaxHalfLifeSeconds) {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidProposalMsg,
			"half_life_seconds must be within [%d, %d]", types.MinHalfLifeSeconds, types.MaxHalfLifeSeconds)
	}

	if err := s.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	// If activated, recompute the decay factor for the new half-life. The scheduled
	// remaining S stays as it is, so the curve continues from its current level at the new rate.
	if s.GetActivated(ctx) && msg.Params.HalfLifeSeconds > 0 {
		d, err := ComputeDecayFactor(msg.Params.HalfLifeSeconds)
		if err != nil {
			return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, err.Error())
		}
		s.SetDecayFactor(ctx, d)
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// Activate activates SVIP reward distribution. Governance-only.
func (s msgServer) Activate(goCtx context.Context, msg *types.MsgActivate) (*types.MsgActivateResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	if s.GetActivated(ctx) {
		return nil, types.ErrAlreadyActivated
	}

	params := s.GetParams(ctx)
	if params.HalfLifeSeconds <= 0 {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "half_life_seconds must be set before activation")
	}

	// Snapshot pool balance
	poolBalance := s.getPoolBalance(ctx)
	if poolBalance.IsZero() {
		return nil, types.ErrPoolNotFunded
	}
	s.SetPoolBalanceAtActivation(ctx, poolBalance)

	// Seed the decay curve: S starts at the pool balance, d from the half-life.
	if err := s.initDecayCurve(ctx, params.HalfLifeSeconds, poolBalance); err != nil {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, err.Error())
	}

	// Clear any stale pause state before activating.
	s.SetPaused(ctx, false)
	s.SetTotalPausedSeconds(ctx, 0)

	// Activate
	s.SetActivated(ctx, true)
	s.SetActivationTime(ctx, ctx.BlockTime())
	s.SetLastBlockTime(ctx, ctx.BlockTime())

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"svip_activated",
		sdk.NewAttribute("pool_balance", poolBalance.String()),
		sdk.NewAttribute("half_life_seconds", fmt.Sprintf("%d", params.HalfLifeSeconds)),
	))

	return &types.MsgActivateResponse{}, nil
}

// Reactivate restarts the SVIP decay curve with a fresh pool snapshot. Governance-only.
func (s msgServer) Reactivate(goCtx context.Context, msg *types.MsgReactivate) (*types.MsgReactivateResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !s.GetActivated(ctx) {
		return nil, types.ErrNotYetActivated
	}

	// Make sure half_life is valid before restarting the curve.
	params := s.GetParams(ctx)
	if params.HalfLifeSeconds <= 0 {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "half_life_seconds must be set before reactivation")
	}

	// Re-snapshot pool balance and restart the decay curve
	poolBalance := s.getPoolBalance(ctx)
	if poolBalance.IsZero() {
		return nil, types.ErrPoolNotFunded
	}
	s.SetPoolBalanceAtActivation(ctx, poolBalance)
	s.SetActivationTime(ctx, ctx.BlockTime())
	s.SetLastBlockTime(ctx, ctx.BlockTime())

	// Restart the decay curve from scratch: S back to the pool balance, d from the half-life.
	if err := s.initDecayCurve(ctx, params.HalfLifeSeconds, poolBalance); err != nil {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, err.Error())
	}

	// Reset cumulative counters for the new curve
	s.SetTotalDistributed(ctx, sdkmath.ZeroInt())
	s.SetTotalPausedSeconds(ctx, 0)

	// Clear paused state if reactivating while paused
	s.SetPaused(ctx, false)

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"svip_reactivated",
		sdk.NewAttribute("pool_balance", poolBalance.String()),
		sdk.NewAttribute("half_life_seconds", fmt.Sprintf("%d", params.HalfLifeSeconds)),
	))

	return &types.MsgReactivateResponse{}, nil
}

// Pause sets or clears the emergency pause flag. Governance-only.
func (s msgServer) Pause(goCtx context.Context, msg *types.MsgPause) (*types.MsgPauseResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !s.GetActivated(ctx) {
		return nil, types.ErrNotYetActivated
	}

	wasPaused := s.GetPaused(ctx)
	s.SetPaused(ctx, msg.Paused)

	// On unpause, move the block anchor to now so the paused gap is left out of the next
	// block's decay; the curve S is unchanged across a pause. The paused-seconds counter is
	// tracked for observability only.
	if wasPaused && !msg.Paused {
		lastBlock := s.GetLastBlockTime(ctx)
		pausedGap := ctx.BlockTime().Unix() - lastBlock.Unix()
		if pausedGap > 0 {
			s.SetTotalPausedSeconds(ctx, s.GetTotalPausedSeconds(ctx)+pausedGap)
		}
		s.SetLastBlockTime(ctx, ctx.BlockTime())
	}

	return &types.MsgPauseResponse{}, nil
}

// FundPool deposits coins into the SVIP reward pool.
func (s msgServer) FundPool(goCtx context.Context, msg *types.MsgFundPool) (*types.MsgFundPoolResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	depositor, err := sdk.AccAddressFromBech32(msg.Depositor)
	if err != nil {
		return nil, err
	}

	// Validate that only the native denom is sent to the pool
	denom := s.getDenom(ctx)
	for _, coin := range msg.Amount {
		if coin.Denom != denom {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins, "invalid denom %s, expected %s", coin.Denom, denom)
		}
	}

	if err := s.bk.SendCoinsFromAccountToModule(ctx, depositor, types.ModuleName, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgFundPoolResponse{}, nil
}
