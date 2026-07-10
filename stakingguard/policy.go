// Package stakingguard enforces this chain's Proof-of-Authority (PoA) staking policy on both
// the EVM path (as a staking/slashing MsgServer decorator the precompiles call into) and the
// Cosmos/authz path (as an ante decorator).
//
// The vendored PoA module (xrplevm/node/v10) only blocks the delegation family on the Cosmos
// ante path; it doesn't cover the EVM staking precompile, authz.MsgExec, or validator-
// management messages. This package fills those gaps from local code without touching the
// vendored module.
//
// The policy:
//   - MsgCreateValidator is ALWAYS rejected, unconditionally — independent of whether the
//     community-pool vault is configured. Creating validators is a pure PoA invariant with no
//     legitimate caller on this chain (validators join only via the gov PoA AddValidator flow),
//     so it must never depend on operational state that a misconfiguration or migration window
//     could clear.
//   - MsgUnjail is ALWAYS rejected, unconditionally, for the same reason: the validator
//     lifecycle is governance-managed.
//   - The delegation family (Delegate, Undelegate, BeginRedelegate, CancelUnbondingDelegation)
//     is allowed only when the delegator is in the allowlist, i.e. the community-pool vault. An
//     empty allowlist fails closed. This gate is only active once enforcement is on (the vault
//     is configured); before that, delegation is unrestricted (upstream behavior), which keeps
//     the upstream precompile test suites valid.
//   - MsgEditValidator, the staking/slashing UpdateParams, and everything else are allowed:
//     they can't change set membership or voting power, and the msg handlers already restrict
//     them to the operator or gov.
package stakingguard

import (
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// ModuleName is the error codespace for the staking guard.
const ModuleName = "stakingguard"

var (
	// ErrValidatorCreationDisabled is returned for MsgCreateValidator.
	ErrValidatorCreationDisabled = errorsmod.Register(ModuleName, 2,
		"creating validators is disabled on this PoA chain; validators are onboarded via governance")
	// ErrUnjailDisabled is returned for MsgUnjail.
	ErrUnjailDisabled = errorsmod.Register(ModuleName, 3,
		"unjailing is disabled on this PoA chain; validator lifecycle is governance-managed")
	// ErrDelegationRestricted is returned for delegation-family messages from a
	// non-allowlisted delegator.
	ErrDelegationRestricted = errorsmod.Register(ModuleName, 4,
		"delegation is restricted to the authorized community-pool vault on this PoA chain")
)

// StakingPolicyFunc reads live state at call time and returns the delegator addresses allowed
// to use the delegation family, plus whether the delegation allowlist is active at all.
// The allowlist turns on once the chain configures the community-pool vault (the EVM scheduler
// target contract); before that (default or test genesis) delegation is unrestricted so the
// upstream staking precompile suites stay valid. The real chain sets the vault at launch. Note
// this flag gates ONLY the delegation family — validator creation and unjailing are blocked
// unconditionally (see CheckMsg). The vendored PoA ante blocks Cosmos-tx delegation on its own
// regardless of this flag.
type StakingPolicyFunc func(ctx sdk.Context) (allowed []sdk.AccAddress, enforced bool)

// CheckMsg enforces the PoA staking policy on a single message. A nil error means the message
// is permitted.
func CheckMsg(ctx sdk.Context, msg sdk.Msg, policy StakingPolicyFunc) error {
	// Validator-set integrity is a pure PoA invariant with no legitimate caller on this chain,
	// so these are rejected unconditionally — independent of the policy/enforcement flag. This
	// closes the migration-window / misconfiguration hole where an unset vault target would
	// otherwise silently reopen validator creation and unjailing.
	switch msg.(type) {
	case *stakingtypes.MsgCreateValidator:
		return ErrValidatorCreationDisabled
	case *slashingtypes.MsgUnjail:
		return ErrUnjailDisabled
	}

	// The delegation family is gated on the allowlist, which is only populated once the
	// community-pool vault is configured. Before that, enforcement is inactive and delegation
	// is unrestricted (upstream behavior).
	var allowed []sdk.AccAddress
	enforced := false
	if policy != nil {
		allowed, enforced = policy(ctx)
	}
	if !enforced {
		return nil
	}
	switch m := msg.(type) {
	case *stakingtypes.MsgDelegate:
		return checkDelegator(m.DelegatorAddress, allowed)
	case *stakingtypes.MsgUndelegate:
		return checkDelegator(m.DelegatorAddress, allowed)
	case *stakingtypes.MsgBeginRedelegate:
		return checkDelegator(m.DelegatorAddress, allowed)
	case *stakingtypes.MsgCancelUnbondingDelegation:
		return checkDelegator(m.DelegatorAddress, allowed)
	default:
		// MsgEditValidator, x/staking & x/slashing MsgUpdateParams, and any non-staking
		// message are permitted.
		return nil
	}
}

// checkDelegator permits the delegation only if the delegator is in the allowlist.
func checkDelegator(delegator string, allowed []sdk.AccAddress) error {
	addr, err := sdk.AccAddressFromBech32(delegator)
	if err != nil {
		return errorsmod.Wrapf(ErrDelegationRestricted, "invalid delegator address %q", delegator)
	}
	for _, a := range allowed {
		if a.Equals(addr) {
			return nil
		}
	}
	return errorsmod.Wrapf(ErrDelegationRestricted, "delegator %s is not authorized to stake", delegator)
}
