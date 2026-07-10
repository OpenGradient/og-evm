package stakingguard

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// NewStakingMsgServer wraps a staking MsgServer so that every delegation-family and
// validator-creation call routed through it (e.g. by the staking precompile) is checked
// against the PoA policy before reaching the underlying server.
func NewStakingMsgServer(inner stakingtypes.MsgServer, allowed StakingPolicyFunc) stakingtypes.MsgServer {
	return &stakingMsgServer{MsgServer: inner, allowed: allowed}
}

type stakingMsgServer struct {
	stakingtypes.MsgServer // EditValidator + UpdateParams fall through unguarded
	allowed                StakingPolicyFunc
}

func (s *stakingMsgServer) CreateValidator(ctx context.Context, msg *stakingtypes.MsgCreateValidator) (*stakingtypes.MsgCreateValidatorResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.CreateValidator(ctx, msg)
}

func (s *stakingMsgServer) Delegate(ctx context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.Delegate(ctx, msg)
}

func (s *stakingMsgServer) Undelegate(ctx context.Context, msg *stakingtypes.MsgUndelegate) (*stakingtypes.MsgUndelegateResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.Undelegate(ctx, msg)
}

func (s *stakingMsgServer) BeginRedelegate(ctx context.Context, msg *stakingtypes.MsgBeginRedelegate) (*stakingtypes.MsgBeginRedelegateResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.BeginRedelegate(ctx, msg)
}

func (s *stakingMsgServer) CancelUnbondingDelegation(ctx context.Context, msg *stakingtypes.MsgCancelUnbondingDelegation) (*stakingtypes.MsgCancelUnbondingDelegationResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.CancelUnbondingDelegation(ctx, msg)
}

// NewSlashingMsgServer wraps a slashing MsgServer so that MsgUnjail routed through it
// (e.g. by the slashing precompile) is rejected under the PoA policy.
func NewSlashingMsgServer(inner slashingtypes.MsgServer, allowed StakingPolicyFunc) slashingtypes.MsgServer {
	return &slashingMsgServer{MsgServer: inner, allowed: allowed}
}

type slashingMsgServer struct {
	slashingtypes.MsgServer // UpdateParams falls through unguarded
	allowed                 StakingPolicyFunc
}

func (s *slashingMsgServer) Unjail(ctx context.Context, msg *slashingtypes.MsgUnjail) (*slashingtypes.MsgUnjailResponse, error) {
	if err := CheckMsg(sdk.UnwrapSDKContext(ctx), msg, s.allowed); err != nil {
		return nil, err
	}
	return s.MsgServer.Unjail(ctx, msg)
}
