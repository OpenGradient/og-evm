package types

const (
	// Event types.
	EventTypeRebalanceSummary       = "rebalance_summary"
	EventTypeRedelegationStarted    = "redelegation_started"
	EventTypeRedelegationFailed     = "redelegation_failed"
	EventTypeRedelegationsCompleted = "redelegations_completed"

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
	AttributeKeyReason         = "reason"
)
