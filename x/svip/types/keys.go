package types

const (
	ModuleName = "svip"
	StoreKey   = ModuleName
	RouterKey  = ModuleName
)

// KV store key prefixes
const (
	prefixParams = iota + 1
	prefixTotalDistributed
	prefixActivationTime
	prefixLastBlockTime
	prefixPoolBalanceAtActivation
	prefixTotalPausedSeconds
	prefixActivated
	prefixPaused
	prefixScheduledRemaining
	prefixDecayFactor
)

var (
	ParamsKey                  = []byte{prefixParams}
	TotalDistributedKey        = []byte{prefixTotalDistributed}
	ActivationTimeKey          = []byte{prefixActivationTime}
	LastBlockTimeKey           = []byte{prefixLastBlockTime}
	PoolBalanceAtActivationKey = []byte{prefixPoolBalanceAtActivation}
	TotalPausedSecondsKey      = []byte{prefixTotalPausedSeconds}
	ActivatedKey               = []byte{prefixActivated}
	PausedKey                  = []byte{prefixPaused}
	// ScheduledRemainingKey stores the remaining pool on the decay curve (LegacyDec),
	// stepped forward each block as S = S * d^dt.
	ScheduledRemainingKey = []byte{prefixScheduledRemaining}
	// DecayFactorKey stores the per-second decay factor d (LegacyDec), recomputed only at
	// (re)activation and when the half-life changes.
	DecayFactorKey = []byte{prefixDecayFactor}
)
