package stakingguard_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/evm/stakingguard"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var (
	vaultAddr  = sdk.AccAddress([]byte("vault_address_20byte"))
	strangerAd = sdk.AccAddress([]byte("stranger_addr_20byte"))
	valAddr    = sdk.ValAddress([]byte("validator_addr_20byt")).String()
)

// allowVault enforces the policy, permitting only the vault address.
func allowVault(sdk.Context) ([]sdk.AccAddress, bool) { return []sdk.AccAddress{vaultAddr}, true }

// allowNone enforces the policy with an empty allowlist (fail-closed for delegation).
func allowNone(sdk.Context) ([]sdk.AccAddress, bool) { return nil, true }

// notEnforced reports enforcement inactive (guard is a no-op).
func notEnforced(sdk.Context) ([]sdk.AccAddress, bool) { return nil, false }

func TestCheckMsg_ValidatorManagementBlocked(t *testing.T) {
	ctx := sdk.Context{}

	err := stakingguard.CheckMsg(ctx, &stakingtypes.MsgCreateValidator{ValidatorAddress: valAddr}, allowVault)
	require.ErrorIs(t, err, stakingguard.ErrValidatorCreationDisabled)

	err = stakingguard.CheckMsg(ctx, &slashingtypes.MsgUnjail{ValidatorAddr: valAddr}, allowVault)
	require.ErrorIs(t, err, stakingguard.ErrUnjailDisabled)
}

func TestCheckMsg_EditValidatorAllowed(t *testing.T) {
	// EditValidator cannot change set membership or power; it is permitted.
	err := stakingguard.CheckMsg(sdk.Context{}, &stakingtypes.MsgEditValidator{ValidatorAddress: valAddr}, allowNone)
	require.NoError(t, err)
}

func TestCheckMsg_DelegationFamilyAllowlist(t *testing.T) {
	ctx := sdk.Context{}

	msgs := []sdk.Msg{
		&stakingtypes.MsgDelegate{DelegatorAddress: vaultAddr.String(), ValidatorAddress: valAddr},
		&stakingtypes.MsgUndelegate{DelegatorAddress: vaultAddr.String(), ValidatorAddress: valAddr},
		&stakingtypes.MsgBeginRedelegate{DelegatorAddress: vaultAddr.String()},
		&stakingtypes.MsgCancelUnbondingDelegation{DelegatorAddress: vaultAddr.String(), ValidatorAddress: valAddr},
	}
	for _, m := range msgs {
		require.NoError(t, stakingguard.CheckMsg(ctx, m, allowVault), "vault delegation should be allowed: %T", m)
	}

	// A non-allowlisted delegator is rejected for the whole delegation family.
	for _, m := range msgs {
		var stranger sdk.Msg
		switch m.(type) {
		case *stakingtypes.MsgDelegate:
			stranger = &stakingtypes.MsgDelegate{DelegatorAddress: strangerAd.String(), ValidatorAddress: valAddr}
		case *stakingtypes.MsgUndelegate:
			stranger = &stakingtypes.MsgUndelegate{DelegatorAddress: strangerAd.String(), ValidatorAddress: valAddr}
		case *stakingtypes.MsgBeginRedelegate:
			stranger = &stakingtypes.MsgBeginRedelegate{DelegatorAddress: strangerAd.String()}
		case *stakingtypes.MsgCancelUnbondingDelegation:
			stranger = &stakingtypes.MsgCancelUnbondingDelegation{DelegatorAddress: strangerAd.String(), ValidatorAddress: valAddr}
		}
		require.ErrorIs(t, stakingguard.CheckMsg(ctx, stranger, allowVault), stakingguard.ErrDelegationRestricted)
	}
}

func TestCheckMsg_FailClosedOnEmptyAllowlist(t *testing.T) {
	// When enforcement is active but the allowlist is empty, even the vault is rejected
	// (fail-closed): delegation is only permitted for an explicitly allowlisted address.
	err := stakingguard.CheckMsg(sdk.Context{}, &stakingtypes.MsgDelegate{DelegatorAddress: vaultAddr.String(), ValidatorAddress: valAddr}, allowNone)
	require.ErrorIs(t, err, stakingguard.ErrDelegationRestricted)
}

func TestCheckMsg_NonStakingAllowed(t *testing.T) {
	// staking/slashing param updates and unrelated messages are not restricted.
	require.NoError(t, stakingguard.CheckMsg(sdk.Context{}, &stakingtypes.MsgUpdateParams{}, allowNone))
	require.NoError(t, stakingguard.CheckMsg(sdk.Context{}, &slashingtypes.MsgUpdateParams{}, allowNone))
}

func TestCheckMsg_DelegationInactiveWhenNotEnforced(t *testing.T) {
	ctx := sdk.Context{}
	// With enforcement inactive (no vault configured), the delegation family is unrestricted
	// (upstream/default behavior): even a stranger may delegate.
	require.NoError(t, stakingguard.CheckMsg(ctx, &stakingtypes.MsgDelegate{DelegatorAddress: strangerAd.String(), ValidatorAddress: valAddr}, notEnforced))

	// A nil policy is also treated as inactive for delegation.
	require.NoError(t, stakingguard.CheckMsg(ctx, &stakingtypes.MsgDelegate{DelegatorAddress: strangerAd.String(), ValidatorAddress: valAddr}, nil))
}

func TestCheckMsg_ValidatorManagementBlockedUnconditionally(t *testing.T) {
	ctx := sdk.Context{}
	// Validator creation and unjailing are a pure PoA invariant: they are rejected regardless
	// of the enforcement flag, including when no vault is configured (notEnforced) or the policy
	// is nil. This closes the migration-window / misconfiguration hole.
	for _, policy := range []stakingguard.StakingPolicyFunc{allowVault, allowNone, notEnforced, nil} {
		require.ErrorIs(t, stakingguard.CheckMsg(ctx, &stakingtypes.MsgCreateValidator{ValidatorAddress: valAddr}, policy), stakingguard.ErrValidatorCreationDisabled)
		require.ErrorIs(t, stakingguard.CheckMsg(ctx, &slashingtypes.MsgUnjail{ValidatorAddr: valAddr}, policy), stakingguard.ErrUnjailDisabled)
	}
}
