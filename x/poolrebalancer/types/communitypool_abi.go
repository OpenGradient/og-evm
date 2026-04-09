package types

import (
	"bytes"

	"github.com/ethereum/go-ethereum/accounts/abi"

	_ "embed"
)

var (
	//go:embed communitypool_abi.json
	communityPoolABIBz []byte

	// CommunityPoolABI contains the minimal ABI for pool automation: stake, harvest, credit,
	// reconcileStakedBuckets, and view getters for totalStaked / pendingRebalanceUnbondReserve.
	CommunityPoolABI abi.ABI
)

func init() {
	var err error
	CommunityPoolABI, err = abi.JSON(bytes.NewReader(communityPoolABIBz))
	if err != nil {
		panic(err)
	}
}
