package tee

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ============ PCR Types ============

// PCRMeasurements stores full PCR values (48 bytes each for SHA-384)
type PCRMeasurements struct {
	PCR0 []byte
	PCR1 []byte
	PCR2 []byte
}

// ApprovedPCR represents an approved PCR configuration
type ApprovedPCR struct {
	PCRHash    common.Hash
	Active     bool
	ApprovedAt *big.Int
	ExpiresAt  *big.Int // 0 = no expiry
	Version    string
}

// ============ TEE Type ============

// TEETypeInfo represents a type of TEE service
type TEETypeInfo struct {
	TypeId  uint8
	Name    string
	Active  bool
	AddedAt *big.Int
}

// ============ TEE Info ============

// TEEInfo contains all registration data for a TEE
type TEEInfo struct {
	TEEId          common.Hash
	Owner          common.Address
	PaymentAddress common.Address
	Endpoint       string
	PublicKey      []byte      // RSA signing key for settlement verification
	TLSCertificate []byte      // TLS certificate for HTTPS (from Nitriding)
	PCRHash        common.Hash // Reference to approved PCR
	TEEType        uint8
	Active         bool
	RegisteredAt   *big.Int
	LastUpdatedAt  *big.Int
}

// ============ Verification Request ============

// VerificationRequest bundles all parameters for signature verification
type VerificationRequest struct {
	TEEId        common.Hash
	RequestHash  common.Hash
	ResponseHash common.Hash
	Timestamp    *big.Int
	Signature    []byte
}

// ============ Settlement Info ============

// SettlementInfo contains details about a verified settlement
type SettlementInfo struct {
	TEEId      common.Hash
	InputHash  common.Hash
	OutputHash common.Hash
	Timestamp  *big.Int
	VerifiedAt *big.Int
}

// ============ ABI Types ============

// ABIPCRMeasurements is the ABI-compatible PCR struct
type ABIPCRMeasurements struct {
	Pcr0 []byte `abi:"pcr0"`
	Pcr1 []byte `abi:"pcr1"`
	Pcr2 []byte `abi:"pcr2"`
}

// ABIApprovedPCR is the ABI-compatible ApprovedPCR struct
type ABIApprovedPCR struct {
	PcrHash    [32]byte `abi:"pcrHash"`
	Active     bool     `abi:"active"`
	ApprovedAt *big.Int `abi:"approvedAt"`
	ExpiresAt  *big.Int `abi:"expiresAt"`
	Version    string   `abi:"version"`
}

// ABITEETypeInfo is the ABI-compatible TEETypeInfo struct
type ABITEETypeInfo struct {
	TypeId  uint8    `abi:"typeId"`
	Name    string   `abi:"name"`
	Active  bool     `abi:"active"`
	AddedAt *big.Int `abi:"addedAt"`
}

// ABITEEInfo is the ABI-compatible TEEInfo struct
type ABITEEInfo struct {
	TeeId          [32]byte       `abi:"teeId"`
	Owner          common.Address `abi:"owner"`
	PaymentAddress common.Address `abi:"paymentAddress"`
	Endpoint       string         `abi:"endpoint"`
	PublicKey      []byte         `abi:"publicKey"`
	TlsCertificate []byte         `abi:"tlsCertificate"`
	PcrHash        [32]byte       `abi:"pcrHash"`
	TeeType        uint8          `abi:"teeType"`
	Active         bool           `abi:"active"`
	RegisteredAt   *big.Int       `abi:"registeredAt"`
	LastUpdatedAt  *big.Int       `abi:"lastUpdatedAt"`
}

// ABIVerificationRequest is the ABI-compatible VerificationRequest struct
type ABIVerificationRequest struct {
	TeeId        [32]byte `abi:"teeId"`
	RequestHash  [32]byte `abi:"requestHash"`
	ResponseHash [32]byte `abi:"responseHash"`
	Timestamp    *big.Int `abi:"timestamp"`
	Signature    []byte   `abi:"signature"`
}

// ============ Conversion Functions ============

// ToABI converts TEEInfo to ABI-compatible format
func (t *TEEInfo) ToABI() ABITEEInfo {
	return ABITEEInfo{
		TeeId:          t.TEEId,
		Owner:          t.Owner,
		PaymentAddress: t.PaymentAddress,
		Endpoint:       t.Endpoint,
		PublicKey:      t.PublicKey,
		TlsCertificate: t.TLSCertificate,
		PcrHash:        t.PCRHash,
		TeeType:        t.TEEType,
		Active:         t.Active,
		RegisteredAt:   t.RegisteredAt,
		LastUpdatedAt:  t.LastUpdatedAt,
	}
}

// ToABI converts ApprovedPCR to ABI-compatible format
func (p *ApprovedPCR) ToABI() ABIApprovedPCR {
	return ABIApprovedPCR{
		PcrHash:    p.PCRHash,
		Active:     p.Active,
		ApprovedAt: p.ApprovedAt,
		ExpiresAt:  p.ExpiresAt,
		Version:    p.Version,
	}
}

// ToABI converts TEETypeInfo to ABI-compatible format
func (t *TEETypeInfo) ToABI() ABITEETypeInfo {
	return ABITEETypeInfo{
		TypeId:  t.TypeId,
		Name:    t.Name,
		Active:  t.Active,
		AddedAt: t.AddedAt,
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

// FromABIVerificationRequest converts ABI request to internal format
func FromABIVerificationRequest(abi ABIVerificationRequest) VerificationRequest {
	return VerificationRequest{
		TEEId:        abi.TeeId,
		RequestHash:  abi.RequestHash,
		ResponseHash: abi.ResponseHash,
		Timestamp:    abi.Timestamp,
		Signature:    abi.Signature,
	}
}
