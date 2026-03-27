package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/x/poolrebalancer/types"
)

func TestEmitRedelegationFailureEvent(t *testing.T) {
	ctx, k := newTestKeeper(t)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	src := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	dst := sdk.ValAddress(bytes.Repeat([]byte{3}, 20))
	coin := sdk.NewInt64Coin("stake", 42)
	reason := "begin redelegation failed"

	k.emitRedelegationFailureEvent(ctx, del, src, dst, coin, reason)

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	require.NotEmpty(t, events)

	ev := events[len(events)-1]
	require.Equal(t, types.EventTypeRedelegationFailed, ev.Type)

	attrs := map[string]string{}
	for _, attr := range ev.Attributes {
		attrs[string(attr.Key)] = string(attr.Value)
	}

	require.Equal(t, del.String(), attrs[types.AttributeKeyDelegator])
	require.Equal(t, src.String(), attrs[types.AttributeKeySrcValidator])
	require.Equal(t, dst.String(), attrs[types.AttributeKeyDstValidator])
	require.Equal(t, coin.Amount.String(), attrs[types.AttributeKeyAmount])
	require.Equal(t, coin.Denom, attrs[types.AttributeKeyDenom])
	require.Equal(t, reason, attrs[types.AttributeKeyReason])
}

func TestEmitUndelegationFailureEvent(t *testing.T) {
	ctx, k := newTestKeeper(t)

	del := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	val := sdk.ValAddress(bytes.Repeat([]byte{2}, 20))
	coin := sdk.NewInt64Coin("stake", 21)
	reason := "begin undelegation failed"

	k.emitUndelegationFailureEvent(ctx, del, val, coin, reason)

	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	require.NotEmpty(t, events)

	ev := events[len(events)-1]
	require.Equal(t, types.EventTypeUndelegationFailed, ev.Type)

	attrs := map[string]string{}
	for _, attr := range ev.Attributes {
		attrs[string(attr.Key)] = string(attr.Value)
	}

	require.Equal(t, del.String(), attrs[types.AttributeKeyDelegator])
	require.Equal(t, val.String(), attrs[types.AttributeKeyValidator])
	require.Equal(t, coin.Amount.String(), attrs[types.AttributeKeyAmount])
	require.Equal(t, coin.Denom, attrs[types.AttributeKeyDenom])
	require.Equal(t, reason, attrs[types.AttributeKeyReason])
}

