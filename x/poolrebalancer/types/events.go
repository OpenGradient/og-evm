package types

const (
	// Event types.
	EventTypeRebalanceSummary       = "rebalance_summary"
	EventTypeRedelegationStarted    = "redelegation_started"
	EventTypeUndelegationStarted    = "undelegation_started"
	EventTypeRedelegationsCompleted = "redelegations_completed"
	EventTypeUndelegationsCompleted = "undelegations_completed"

	// Common attributes.
	AttributeKeyDelegator      = "delegator"
	AttributeKeyValidator      = "validator"
	AttributeKeySrcValidator   = "src_validator"
	AttributeKeyDstValidator   = "dst_validator"
	AttributeKeyAmount         = "amount"
	AttributeKeyDenom          = "denom"
	AttributeKeyCompletionTime = "completion_time"
	AttributeKeyCount          = "count"
	AttributeKeyOpsDone        = "ops_done"
	AttributeKeyUseFallback    = "use_undelegate_fallback"
)
