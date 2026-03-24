package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Event types for the bridge module.
const (
	EventTypeBridgeMint = "bridge_mint"
	EventTypeBridgeBurn = "bridge_burn"
)

// Attribute keys for bridge events.
const (
	AttributeKeyRecipient = "recipient"
	AttributeKeySender    = "sender"
	AttributeKeyAmount    = "amount"
	AttributeKeyDenom     = "denom"
)

// EmitBridgeMintEvent emits an event when tokens are minted via bridge.
func EmitBridgeMintEvent(ctx sdk.Context, recipient string, amount string, denom string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		EventTypeBridgeMint,
		sdk.NewAttribute(AttributeKeyRecipient, recipient),
		sdk.NewAttribute(AttributeKeyAmount, amount),
		sdk.NewAttribute(AttributeKeyDenom, denom),
	))
}

// EmitBridgeBurnEvent emits an event when tokens are burned via bridge.
func EmitBridgeBurnEvent(ctx sdk.Context, sender string, amount string, denom string) {
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		EventTypeBridgeBurn,
		sdk.NewAttribute(AttributeKeySender, sender),
		sdk.NewAttribute(AttributeKeyAmount, amount),
		sdk.NewAttribute(AttributeKeyDenom, denom),
	))
}
