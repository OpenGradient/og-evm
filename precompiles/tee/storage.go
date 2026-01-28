package tee

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// Storage slot prefixes
const (
	slotOwner     byte = 0x01
	slotFlags     byte = 0x02
	slotPCR0      byte = 0x03
	slotPCR1      byte = 0x04
	slotPCR2      byte = 0x05
	slotPublicKey byte = 0x06
)

// Storage handles TEE state persistence
type Storage struct {
	stateDB vm.StateDB
	address common.Address
}

// NewStorage creates a new storage instance
func NewStorage(stateDB vm.StateDB, precompileAddr common.Address) *Storage {
	return &Storage{
		stateDB: stateDB,
		address: precompileAddr,
	}
}

// computeSlot generates a storage slot from prefix and teeId
func (s *Storage) computeSlot(prefix byte, teeId common.Hash) common.Hash {
	data := make([]byte, 33)
	data[0] = prefix
	copy(data[1:], teeId.Bytes())
	return crypto.Keccak256Hash(data)
}

// computeSlotWithIndex generates a storage slot with an additional index
func (s *Storage) computeSlotWithIndex(prefix byte, teeId common.Hash, index uint64) common.Hash {
	data := make([]byte, 41)
	data[0] = prefix
	copy(data[1:33], teeId.Bytes())
	binary.BigEndian.PutUint64(data[33:], index)
	return crypto.Keccak256Hash(data)
}

// ============ Write Operations ============

// StoreTEE saves a complete TEE record
func (s *Storage) StoreTEE(info TEEInfo) {
	teeId := info.TEEId

	// Store owner (slot 0x01)
	s.stateDB.SetState(
		s.address,
		s.computeSlot(slotOwner, teeId),
		common.BytesToHash(common.LeftPadBytes(info.Owner.Bytes(), 32)),
	)

	// Store flags: active (1 byte) + registeredAt (8 bytes) + lastUpdatedAt (8 bytes)
	flags := make([]byte, 32)
	if info.Active {
		flags[0] = 1
	}
	if info.RegisteredAt != nil {
		binary.BigEndian.PutUint64(flags[8:16], info.RegisteredAt.Uint64())
	}
	if info.LastUpdatedAt != nil {
		binary.BigEndian.PutUint64(flags[16:24], info.LastUpdatedAt.Uint64())
	}
	s.stateDB.SetState(s.address, s.computeSlot(slotFlags, teeId), common.BytesToHash(flags))

	// Store PCRs
	s.stateDB.SetState(s.address, s.computeSlot(slotPCR0, teeId), info.PCRs.PCR0)
	s.stateDB.SetState(s.address, s.computeSlot(slotPCR1, teeId), info.PCRs.PCR1)
	s.stateDB.SetState(s.address, s.computeSlot(slotPCR2, teeId), info.PCRs.PCR2)

	// Store public key (variable length)
	s.storeBytes(teeId, info.PublicKey)
}

// storeBytes stores variable-length data in chunks
func (s *Storage) storeBytes(teeId common.Hash, data []byte) {
	// Store length at base slot
	baseSlot := s.computeSlot(slotPublicKey, teeId)
	s.stateDB.SetState(s.address, baseSlot, common.BigToHash(big.NewInt(int64(len(data)))))

	// Store data in 32-byte chunks
	for i := 0; i < len(data); i += 32 {
		chunk := [32]byte{}
		end := i + 32
		if end > len(data) {
			end = len(data)
		}
		copy(chunk[:], data[i:end])

		chunkSlot := s.computeSlotWithIndex(slotPublicKey, teeId, uint64(i/32)+1)
		s.stateDB.SetState(s.address, chunkSlot, chunk)
	}
}

// SetActive updates the active status
func (s *Storage) SetActive(teeId common.Hash, active bool, timestamp *big.Int) error {
	if !s.Exists(teeId) {
		return ErrTEENotFound
	}

	// Load current flags
	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotFlags, teeId))
	flags := flagsHash.Bytes()

	// Update active flag
	if active {
		flags[0] = 1
	} else {
		flags[0] = 0
	}

	// Update lastUpdatedAt
	binary.BigEndian.PutUint64(flags[16:24], timestamp.Uint64())

	s.stateDB.SetState(s.address, s.computeSlot(slotFlags, teeId), common.BytesToHash(flags))
	return nil
}

// ============ Read Operations ============

// Exists checks if a TEE is registered
func (s *Storage) Exists(teeId common.Hash) bool {
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotOwner, teeId))
	return ownerHash != (common.Hash{})
}

// LoadTEE retrieves a complete TEE record
func (s *Storage) LoadTEE(teeId common.Hash) (TEEInfo, bool) {
	// Load owner
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotOwner, teeId))
	if ownerHash == (common.Hash{}) {
		return TEEInfo{}, false
	}

	// Load flags
	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotFlags, teeId))
	flags := flagsHash.Bytes()

	// Load PCRs
	pcr0 := s.stateDB.GetState(s.address, s.computeSlot(slotPCR0, teeId))
	pcr1 := s.stateDB.GetState(s.address, s.computeSlot(slotPCR1, teeId))
	pcr2 := s.stateDB.GetState(s.address, s.computeSlot(slotPCR2, teeId))

	// Load public key
	publicKey := s.loadBytes(teeId)

	return TEEInfo{
		TEEId:         teeId,
		Owner:         common.BytesToAddress(ownerHash.Bytes()[12:32]),
		Active:        flags[0] == 1,
		RegisteredAt:  new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[8:16])),
		LastUpdatedAt: new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[16:24])),
		PCRs: PCRMeasurements{
			PCR0: pcr0,
			PCR1: pcr1,
			PCR2: pcr2,
		},
		PublicKey: publicKey,
	}, true
}

// loadBytes retrieves variable-length data
func (s *Storage) loadBytes(teeId common.Hash) []byte {
	baseSlot := s.computeSlot(slotPublicKey, teeId)
	lengthHash := s.stateDB.GetState(s.address, baseSlot)
	length := new(big.Int).SetBytes(lengthHash.Bytes()).Int64()

	if length == 0 {
		return nil
	}

	data := make([]byte, length)
	for i := int64(0); i < length; i += 32 {
		chunkSlot := s.computeSlotWithIndex(slotPublicKey, teeId, uint64(i/32)+1)
		chunk := s.stateDB.GetState(s.address, chunkSlot)

		end := i + 32
		if end > length {
			end = length
		}
		copy(data[i:end], chunk.Bytes()[:end-i])
	}

	return data
}

// GetOwner returns the owner address of a TEE
func (s *Storage) GetOwner(teeId common.Hash) (common.Address, bool) {
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotOwner, teeId))
	if ownerHash == (common.Hash{}) {
		return common.Address{}, false
	}
	return common.BytesToAddress(ownerHash.Bytes()[12:32]), true
}

// IsActive checks if a TEE is active
func (s *Storage) IsActive(teeId common.Hash) bool {
	if !s.Exists(teeId) {
		return false
	}
	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotFlags, teeId))
	return flagsHash.Bytes()[0] == 1
}

// GetPublicKey returns the public key for a TEE
func (s *Storage) GetPublicKey(teeId common.Hash) ([]byte, error) {
	if !s.Exists(teeId) {
		return nil, ErrTEENotFound
	}
	return s.loadBytes(teeId), nil
}
