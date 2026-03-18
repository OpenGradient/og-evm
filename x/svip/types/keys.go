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
)

var (
	ParamsKey                  = []byte{prefixParams}
	TotalDistributedKey        = []byte{prefixTotalDistributed}
	ActivationTimeKey          = []byte{prefixActivationTime}
	LastBlockTimeKey           = []byte{prefixLastBlockTime}
	PoolBalanceAtActivationKey = []byte{prefixPoolBalanceAtActivation}
	TotalPausedSecondsKey      = []byte{prefixTotalPausedSeconds}
)
