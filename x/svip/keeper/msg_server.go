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
	if current.Activated && !msg.Params.Activated {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "cannot deactivate SVIP after activation")
	}
	if current.Activated && current.HalfLifeSeconds > 0 && msg.Params.HalfLifeSeconds > 0 {
		// Reject if half_life changes by more than 50%
		oldHL := float64(current.HalfLifeSeconds)
		newHL := float64(msg.Params.HalfLifeSeconds)
		ratio := newHL / oldHL
		if ratio < 0.5 || ratio > 1.5 {
			return nil, types.ErrHalfLifeChange
		}
	}
	if msg.Params.HalfLifeSeconds > 0 && msg.Params.HalfLifeSeconds < 31536000 {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "half_life_seconds must be >= 1 year (31536000s)")
	}

	if err := s.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// Activate activates SVIP reward distribution. Governance-only.
func (s msgServer) Activate(goCtx context.Context, msg *types.MsgActivate) (*types.MsgActivateResponse, error) {
	if s.authority.String() != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", s.authority.String(), msg.Authority)
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	params := s.GetParams(ctx)
	if params.Activated {
		return nil, types.ErrAlreadyActivated
	}

	if params.HalfLifeSeconds <= 0 {
		return nil, errorsmod.Wrap(govtypes.ErrInvalidProposalMsg, "half_life_seconds must be set before activation")
	}

	// Snapshot pool balance
	poolBalance := s.getPoolBalance(ctx)
	if poolBalance.IsZero() {
		return nil, types.ErrPoolNotFunded
	}
	s.SetPoolBalanceAtActivation(ctx, poolBalance)

	// Activate
	params.Activated = true
	if err := s.SetParams(ctx, params); err != nil {
		return nil, err
	}
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

	params := s.GetParams(ctx)
	if !params.Activated {
		return nil, types.ErrNotYetActivated
	}

	// Re-snapshot pool balance and restart the decay curve
	poolBalance := s.getPoolBalance(ctx)
	if poolBalance.IsZero() {
		return nil, types.ErrPoolNotFunded
	}
	s.SetPoolBalanceAtActivation(ctx, poolBalance)
	s.SetActivationTime(ctx, ctx.BlockTime())
	s.SetLastBlockTime(ctx, ctx.BlockTime())

	// Reset cumulative counters for the new curve
	s.SetTotalDistributed(ctx, sdkmath.ZeroInt())
	s.SetTotalPausedSeconds(ctx, 0)

	// Clear paused state if reactivating while paused
	if params.Paused {
		params.Paused = false
		if err := s.SetParams(ctx, params); err != nil {
			return nil, err
		}
	}

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
	params := s.GetParams(ctx)
	wasPaused := params.Paused
	params.Paused = msg.Paused
	if err := s.SetParams(ctx, params); err != nil {
		return nil, err
	}

	// On unpause: accumulate paused duration + reset LastBlockTime
	if wasPaused && !msg.Paused {
		lastBlock := s.GetLastBlockTime(ctx)
		pausedGap := int64(ctx.BlockTime().Sub(lastBlock).Seconds())
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
