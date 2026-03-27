package types

const (
	// Event types.
	EventTypeRebalanceSummary       = "rebalance_summary"
	EventTypeRedelegationStarted    = "redelegation_started"
	EventTypeRedelegationFailed     = "redelegation_failed"
	EventTypeUndelegationStarted    = "undelegation_started"
	EventTypeUndelegationFailed     = "undelegation_failed"
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
	AttributeKeyReason         = "reason"
)
