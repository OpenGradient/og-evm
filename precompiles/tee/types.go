package tee

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// PCRMeasurements stores truncated PCR values (32 bytes each)
// AWS Nitro produces SHA-384 (48 bytes), we store first 32 bytes
type PCRMeasurements struct {
	PCR0 [32]byte
	PCR1 [32]byte
	PCR2 [32]byte
}

// TEEInfo contains all registration data for a TEE
type TEEInfo struct {
	TEEId         common.Hash
	Owner         common.Address
	PublicKey     []byte
	PCRs          PCRMeasurements
	Active        bool
	RegisteredAt  *big.Int
	LastUpdatedAt *big.Int
}

// ABIPCRMeasurements is the ABI-compatible PCR struct
type ABIPCRMeasurements struct {
	Pcr0 [32]byte `abi:"pcr0"`
	Pcr1 [32]byte `abi:"pcr1"`
	Pcr2 [32]byte `abi:"pcr2"`
}

// ABITEEInfo is the ABI-compatible TEEInfo struct
type ABITEEInfo struct {
	TeeId         [32]byte           `abi:"teeId"`
	Owner         common.Address     `abi:"owner"`
	PublicKey     []byte             `abi:"publicKey"`
	Pcrs          ABIPCRMeasurements `abi:"pcrs"`
	Active        bool               `abi:"active"`
	RegisteredAt  *big.Int           `abi:"registeredAt"`
	LastUpdatedAt *big.Int           `abi:"lastUpdatedAt"`
}

// ToABI converts TEEInfo to ABI-compatible format
func (t *TEEInfo) ToABI() ABITEEInfo {
	return ABITEEInfo{
		TeeId:     t.TEEId,
		Owner:     t.Owner,
		PublicKey: t.PublicKey,
		Pcrs: ABIPCRMeasurements{
			Pcr0: t.PCRs.PCR0,
			Pcr1: t.PCRs.PCR1,
			Pcr2: t.PCRs.PCR2,
		},
		Active:        t.Active,
		RegisteredAt:  t.RegisteredAt,
		LastUpdatedAt: t.LastUpdatedAt,
	}
}

// FromABIPCRs converts ABI PCRs to internal format
func FromABIPCRs(abi ABIPCRMeasurements) PCRMeasurements {
	return PCRMeasurements{
		PCR0: abi.Pcr0,
		PCR1: abi.Pcr1,
		PCR2: abi.Pcr2,
	}
}
