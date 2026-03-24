package types

const (
	ModuleName = "bridge"
	StoreKey   = ModuleName
	RouterKey  = ModuleName
)

// KV store key prefixes
const (
	prefixParams = iota + 1
	prefixTotalMinted
	prefixTotalBurned
)

var (
	ParamsStoreKey      = []byte{prefixParams}
	TotalMintedStoreKey = []byte{prefixTotalMinted}
	TotalBurnedStoreKey = []byte{prefixTotalBurned}
)
