package stakingguard

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// maxAuthzNestingDepth bounds authz.MsgExec recursion to avoid pathological nesting.
const maxAuthzNestingDepth = 8

// Decorator is an ante decorator that enforces the PoA staking policy on the Cosmos tx
// path, recursing into authz.MsgExec so wrapped staking messages cannot bypass the guard.
// It complements the vendored PoA ante (which blocks the delegation family) by also
// blocking MsgCreateValidator / MsgUnjail and unwrapping authz.
type Decorator struct {
	allowed StakingPolicyFunc
}

// NewDecorator returns a staking-guard ante decorator.
func NewDecorator(allowed StakingPolicyFunc) Decorator {
	return Decorator{allowed: allowed}
}

// AnteHandle implements sdk.AnteDecorator.
func (d Decorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := d.checkRecursive(ctx, msg, 0); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// checkRecursive applies the policy to msg, descending into authz.MsgExec payloads.
func (d Decorator) checkRecursive(ctx sdk.Context, msg sdk.Msg, depth int) error {
	if exec, ok := msg.(*authz.MsgExec); ok {
		if depth >= maxAuthzNestingDepth {
			return ErrDelegationRestricted.Wrap("authz nesting too deep")
		}
		inner, err := exec.GetMessages()
		if err != nil {
			return err
		}
		for _, im := range inner {
			if err := d.checkRecursive(ctx, im, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return CheckMsg(ctx, msg, d.allowed)
}
