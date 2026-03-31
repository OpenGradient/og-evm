package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ModuleName is the name of the poolrebalancer module (used in store keys and routing).
	ModuleName = "poolrebalancer"

	// StoreKey is the default store key for the poolrebalancer module (same as ModuleName).
	StoreKey = ModuleName

	// RouterKey is the top-level router key for the module.
	RouterKey = ModuleName
)

// Store key prefixes (single-byte prefixes).
var (
	ParamsKey = []byte{0x01} // module params

	// Pending redelegation tracking.
	// Primary key: (delegator, denom, dstValidator, srcValidator, completionTime)
	PendingRedelegationKey = []byte{0x11}
	// Index by source validator: (srcValidator, completionTime, denom, dstValidator, delegator)
	PendingRedelegationBySrcIndexKey = []byte{0x12}
	// Queue by completion time: completionTime -> list of pending redelegation entries
	PendingRedelegationQueueKey = []byte{0x13}

	// Pending undelegation tracking.
	// Queue: (completionTime, delegator) -> queued undelegation entries
	PendingUndelegationQueueKey = []byte{0x21}
	// Index by validator: (validator, completionTime, denom, delegator)
	PendingUndelegationByValIndexKey = []byte{0x22}
)

// GetPendingRedelegationKey returns the primary key for a pending redelegation.
// Key format: prefix | lengthPrefixed(delegator) | lengthPrefixed(denom) | lengthPrefixed(dstValidator) | lengthPrefixed(srcValidator) | completionTime.
func GetPendingRedelegationKey(del sdk.AccAddress, denom string, srcVal, dstVal sdk.ValAddress, completion time.Time) []byte {
	key := make([]byte, 0)
	key = append(key, PendingRedelegationKey...)
	key = append(key, address.MustLengthPrefix(del)...)
	key = append(key, address.MustLengthPrefix([]byte(denom))...)
	key = append(key, address.MustLengthPrefix(dstVal)...)
	key = append(key, address.MustLengthPrefix(srcVal)...)
	key = append(key, sdk.FormatTimeBytes(completion)...)
	return key
}

// GetPendingRedelegationBySrcIndexKey returns the index key for lookup by source validator.
// Key format: prefix | lengthPrefixed(srcValidator) | lengthPrefixed(completionTime) | lengthPrefixed(denom) | lengthPrefixed(dstVal) | lengthPrefixed(delegator).
func GetPendingRedelegationBySrcIndexKey(srcVal sdk.ValAddress, completion time.Time, denom string, dstVal sdk.ValAddress, del sdk.AccAddress) []byte {
	key := make([]byte, 0)
	key = append(key, PendingRedelegationBySrcIndexKey...)
	key = append(key, address.MustLengthPrefix(srcVal)...)
	key = append(key, address.MustLengthPrefix(sdk.FormatTimeBytes(completion))...)
	key = append(key, address.MustLengthPrefix([]byte(denom))...)
	key = append(key, address.MustLengthPrefix(dstVal)...)
	key = append(key, address.MustLengthPrefix(del)...)
	return key
}

// GetPendingRedelegationQueueKey returns the queue key for a given completion time.
// Used to iterate pending redelegations that mature at or before a given time.
func GetPendingRedelegationQueueKey(completion time.Time) []byte {
	key := make([]byte, 0)
	key = append(key, PendingRedelegationQueueKey...)
	key = append(key, sdk.FormatTimeBytes(completion)...)
	return key
}

// ParsePendingRedelegationQueueKey parses the completion time from a pending redelegation queue key.
// Key format: PendingRedelegationQueueKey (0x13) + FormatTimeBytes(completion).
func ParsePendingRedelegationQueueKey(key []byte) (time.Time, error) {
	if len(key) <= len(PendingRedelegationQueueKey) {
		return time.Time{}, fmt.Errorf("invalid pending redelegation queue key length")
	}
	return sdk.ParseTimeBytes(key[len(PendingRedelegationQueueKey):])
}

// GetPendingRedelegationPrefix returns the key prefix for (delegator, denom, dstValidator).
// Used by HasImmatureRedelegationTo to prefix-scan all completion times for this triple.
func GetPendingRedelegationPrefix(del sdk.AccAddress, denom string, dstVal sdk.ValAddress) []byte {
	key := make([]byte, 0)
	key = append(key, PendingRedelegationKey...)
	key = append(key, address.MustLengthPrefix(del)...)
	key = append(key, address.MustLengthPrefix([]byte(denom))...)
	key = append(key, address.MustLengthPrefix(dstVal)...)
	return key
}

// GetPendingUndelegationQueueKey returns the queue key for (completionTime, delegator).
// Key format: prefix | lengthPrefixed(completionTime) | lengthPrefixed(delegator).
func GetPendingUndelegationQueueKey(completion time.Time, del sdk.AccAddress) []byte {
	key := make([]byte, 0)
	key = append(key, PendingUndelegationQueueKey...)
	key = append(key, address.MustLengthPrefix(sdk.FormatTimeBytes(completion))...)
	key = append(key, address.MustLengthPrefix(del)...)
	return key
}

// GetPendingUndelegationQueueKeyByTime returns the undelegation queue prefix for a given completion time.
// Key format: PendingUndelegationQueueKey (0x21) + lengthPrefixed(FormatTimeBytes(completion)).
// This is used as an end key when iterating all queued undelegations up to a given time.
func GetPendingUndelegationQueueKeyByTime(completion time.Time) []byte {
	key := make([]byte, 0)
	key = append(key, PendingUndelegationQueueKey...)
	key = append(key, address.MustLengthPrefix(sdk.FormatTimeBytes(completion))...)
	return key
}

// ParsePendingUndelegationQueueKeyForCompletionTime parses the completion time from a pending undelegation queue key.
// Key format: PendingUndelegationQueueKey (0x21) + lengthPrefixed(timeBytes) + lengthPrefixed(delegator).
func ParsePendingUndelegationQueueKeyForCompletionTime(key []byte) (time.Time, error) {
	offset := len(PendingUndelegationQueueKey)
	if len(key) <= offset {
		return time.Time{}, fmt.Errorf("invalid pending undelegation queue key length")
	}
	timeLen := int(key[offset])
	offset++
	if len(key) < offset+timeLen {
		return time.Time{}, fmt.Errorf("invalid pending undelegation queue key time length")
	}
	timeBytes := key[offset : offset+timeLen]
	return sdk.ParseTimeBytes(timeBytes)
}

// GetPendingUndelegationByValIndexKey returns the index key for lookup by validator.
// Key format: prefix | lengthPrefixed(validator) | lengthPrefixed(completionTime) | lengthPrefixed(denom) | lengthPrefixed(delegator).
func GetPendingUndelegationByValIndexKey(val sdk.ValAddress, completion time.Time, denom string, del sdk.AccAddress) []byte {
	key := make([]byte, 0)
	key = append(key, PendingUndelegationByValIndexKey...)
	key = append(key, address.MustLengthPrefix(val)...)
	key = append(key, address.MustLengthPrefix(sdk.FormatTimeBytes(completion))...)
	key = append(key, address.MustLengthPrefix([]byte(denom))...)
	key = append(key, address.MustLengthPrefix(del)...)
	return key
}
