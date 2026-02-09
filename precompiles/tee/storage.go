package tee

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// ============ Storage Slot Prefixes ============
// Each data type needs its own prefix to avoid collisions

const (
	// TEE Info slots
	slotTEEOwner          byte = 0x01
	slotTEEFlags          byte = 0x02 // active, teeType, timestamps
	slotTEEPublicKey      byte = 0x03
	slotTEEPaymentAddress byte = 0x04
	slotTEEEndpoint       byte = 0x05
	slotTEEPCRHash        byte = 0x06
	slotTEETLSCert        byte = 0x07 // TLS certificate

	// Admin slots
	slotAdminFlag  byte = 0x10 // mapping(address => bool)
	slotAdminList  byte = 0x11 // address[]
	slotAdminCount byte = 0x12

	// TEE Type slots
	slotTEETypeFlags byte = 0x20 // mapping(uint8 => flags)
	slotTEETypeName  byte = 0x21 // mapping(uint8 => string)
	slotTEETypeList  byte = 0x22 // uint8[]
	slotTEETypeCount byte = 0x23

	// PCR Registry slots
	slotPCRFlags   byte = 0x30 // mapping(bytes32 => flags)
	slotPCRVersion byte = 0x31 // mapping(bytes32 => string)
	slotPCRList    byte = 0x32 // bytes32[]
	slotPCRCount   byte = 0x33

	// Active TEE list slots
	slotActiveTEEList  byte = 0x40 // bytes32[]
	slotActiveTEECount byte = 0x41
	slotActiveTEEIndex byte = 0x42 // mapping(bytes32 => uint256) for O(1) removal

	// TEE by owner slots
	slotTEEByOwner      byte = 0x50 // mapping(address => bytes32[])
	slotTEEByOwnerCount byte = 0x51

	// TEE by type slots
	slotTEEByType      byte = 0x60 // mapping(uint8 => bytes32[])
	slotTEEByTypeCount byte = 0x61

	// AWS Certificate slot - stores full certificate bytes
	slotAWSRootCert byte = 0x70

	// Settlement tracking slots - for replay protection
	slotSettlementUsed byte = 0x80 // mapping(bytes32 => bool)
)

// Settlement timestamp validation constants
const (
	MaxSettlementAge    uint64 = 3600 // 1 hour max age
	FutureTimeTolerance uint64 = 300  // 5 minutes future tolerance
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

// ============ Slot Computation Helpers ============

func (s *Storage) computeSlot(prefix byte, key common.Hash) common.Hash {
	data := make([]byte, 33)
	data[0] = prefix
	copy(data[1:], key.Bytes())
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeSlotAddress(prefix byte, addr common.Address) common.Hash {
	data := make([]byte, 21)
	data[0] = prefix
	copy(data[1:], addr.Bytes())
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeSlotUint8(prefix byte, id uint8) common.Hash {
	data := make([]byte, 2)
	data[0] = prefix
	data[1] = id
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeSlotWithIndex(prefix byte, key common.Hash, index uint64) common.Hash {
	data := make([]byte, 41)
	data[0] = prefix
	copy(data[1:33], key.Bytes())
	binary.BigEndian.PutUint64(data[33:], index)
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeSlotAddressIndex(prefix byte, addr common.Address, index uint64) common.Hash {
	data := make([]byte, 29)
	data[0] = prefix
	copy(data[1:21], addr.Bytes())
	binary.BigEndian.PutUint64(data[21:], index)
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeSlotUint8Index(prefix byte, id uint8, index uint64) common.Hash {
	data := make([]byte, 10)
	data[0] = prefix
	data[1] = id
	binary.BigEndian.PutUint64(data[2:], index)
	return crypto.Keccak256Hash(data)
}

func (s *Storage) computeBaseSlot(prefix byte) common.Hash {
	return crypto.Keccak256Hash([]byte{prefix})
}

// ============ Admin Management ============

func (s *Storage) IsAdmin(addr common.Address) bool {
	slot := s.computeSlotAddress(slotAdminFlag, addr)
	val := s.stateDB.GetState(s.address, slot)
	return val[31] == 1
}

func (s *Storage) AddAdmin(addr common.Address) error {
	if s.IsAdmin(addr) {
		return ErrAdminAlreadyExists
	}

	// Set admin flag
	slot := s.computeSlotAddress(slotAdminFlag, addr)
	s.stateDB.SetState(s.address, slot, common.BytesToHash([]byte{1}))

	// Add to list
	countSlot := s.computeBaseSlot(slotAdminCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	listSlot := s.computeSlotWithIndex(slotAdminList, common.Hash{}, count)
	s.stateDB.SetState(s.address, listSlot, common.BytesToHash(addr.Bytes()))

	// Increment count
	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))

	return nil
}

func (s *Storage) RemoveAdmin(addr common.Address) error {
	if !s.IsAdmin(addr) {
		return ErrAdminNotFound
	}

	// Check not last admin
	admins := s.GetAdmins()
	if len(admins) <= 1 {
		return ErrCannotRemoveLastAdmin
	}

	// Remove admin flag
	slot := s.computeSlotAddress(slotAdminFlag, addr)
	s.stateDB.SetState(s.address, slot, common.Hash{})

	return nil
}

func (s *Storage) GetAdmins() []common.Address {
	countSlot := s.computeBaseSlot(slotAdminCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	admins := make([]common.Address, 0, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotWithIndex(slotAdminList, common.Hash{}, i)
		addrHash := s.stateDB.GetState(s.address, listSlot)
		addr := common.BytesToAddress(addrHash.Bytes()[12:])

		// Only include if still admin
		if s.IsAdmin(addr) {
			admins = append(admins, addr)
		}
	}
	return admins
}

// ============ TEE Type Management ============

func (s *Storage) AddTEEType(typeId uint8, name string, timestamp *big.Int) error {
	if s.TEETypeExists(typeId) {
		return ErrTEETypeExists
	}

	// Store type flags: active (1 byte) + addedAt (8 bytes)
	flagsSlot := s.computeSlotUint8(slotTEETypeFlags, typeId)
	flags := make([]byte, 32)
	flags[0] = 1 // active
	binary.BigEndian.PutUint64(flags[8:16], timestamp.Uint64())
	s.stateDB.SetState(s.address, flagsSlot, common.BytesToHash(flags))

	// Store name in SEPARATE slot
	s.storeStringUint8(slotTEETypeName, typeId, name)

	// Add to list
	countSlot := s.computeBaseSlot(slotTEETypeCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	listSlot := s.computeSlotUint8Index(slotTEETypeList, 0, count)
	s.stateDB.SetState(s.address, listSlot, common.BytesToHash([]byte{typeId}))

	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))

	return nil
}

func (s *Storage) TEETypeExists(typeId uint8) bool {
	flagsSlot := s.computeSlotUint8(slotTEETypeFlags, typeId)
	val := s.stateDB.GetState(s.address, flagsSlot)
	return val != (common.Hash{})
}

func (s *Storage) IsValidTEEType(typeId uint8) bool {
	flagsSlot := s.computeSlotUint8(slotTEETypeFlags, typeId)
	val := s.stateDB.GetState(s.address, flagsSlot)
	return val[0] == 1 // active flag
}

func (s *Storage) DeactivateTEEType(typeId uint8) error {
	if !s.TEETypeExists(typeId) {
		return ErrTEETypeNotFound
	}

	flagsSlot := s.computeSlotUint8(slotTEETypeFlags, typeId)
	val := s.stateDB.GetState(s.address, flagsSlot)
	flags := val.Bytes()
	flags[0] = 0 // deactivate
	s.stateDB.SetState(s.address, flagsSlot, common.BytesToHash(flags))

	return nil
}

func (s *Storage) GetTEETypes() []TEETypeInfo {
	countSlot := s.computeBaseSlot(slotTEETypeCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	types := make([]TEETypeInfo, 0, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotUint8Index(slotTEETypeList, 0, i)
		typeIdHash := s.stateDB.GetState(s.address, listSlot)
		typeId := typeIdHash[31]

		flagsSlot := s.computeSlotUint8(slotTEETypeFlags, typeId)
		val := s.stateDB.GetState(s.address, flagsSlot)
		flags := val.Bytes()

		name := s.loadStringUint8(slotTEETypeName, typeId)

		types = append(types, TEETypeInfo{
			TypeId:  typeId,
			Name:    name,
			Active:  flags[0] == 1,
			AddedAt: new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[8:16])),
		})
	}
	return types
}

// ============ PCR Registry ============

func (s *Storage) ApprovePCR(pcrHash common.Hash, version string, expiresAt *big.Int, timestamp *big.Int) error {
	flagsSlot := s.computeSlot(slotPCRFlags, pcrHash)

	// Store flags: active (1 byte) + approvedAt (8 bytes) + expiresAt (8 bytes)
	flags := make([]byte, 32)
	flags[0] = 1 // active
	binary.BigEndian.PutUint64(flags[8:16], timestamp.Uint64())
	if expiresAt != nil {
		binary.BigEndian.PutUint64(flags[16:24], expiresAt.Uint64())
	}
	s.stateDB.SetState(s.address, flagsSlot, common.BytesToHash(flags))

	// Store version string in SEPARATE slot
	s.storeString(slotPCRVersion, pcrHash, version)

	// Add to list
	countSlot := s.computeBaseSlot(slotPCRCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	listSlot := s.computeSlotWithIndex(slotPCRList, common.Hash{}, count)
	s.stateDB.SetState(s.address, listSlot, pcrHash)

	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))

	return nil
}

func (s *Storage) RevokePCR(pcrHash common.Hash) error {
	flagsSlot := s.computeSlot(slotPCRFlags, pcrHash)
	val := s.stateDB.GetState(s.address, flagsSlot)

	if val == (common.Hash{}) {
		return ErrPCRNotFound
	}

	flags := val.Bytes()
	flags[0] = 0 // deactivate
	s.stateDB.SetState(s.address, flagsSlot, common.BytesToHash(flags))

	return nil
}

func (s *Storage) SetPCRExpiry(pcrHash common.Hash, expiresAt *big.Int) error {
	flagsSlot := s.computeSlot(slotPCRFlags, pcrHash)
	val := s.stateDB.GetState(s.address, flagsSlot)

	if val == (common.Hash{}) {
		return ErrPCRNotFound
	}

	flags := val.Bytes()
	binary.BigEndian.PutUint64(flags[16:24], expiresAt.Uint64())
	s.stateDB.SetState(s.address, flagsSlot, common.BytesToHash(flags))

	return nil
}

func (s *Storage) IsPCRApproved(pcrHash common.Hash, currentTime *big.Int) bool {
	flagsSlot := s.computeSlot(slotPCRFlags, pcrHash)
	val := s.stateDB.GetState(s.address, flagsSlot)

	if val == (common.Hash{}) {
		return false
	}

	flags := val.Bytes()

	// Check active
	if flags[0] != 1 {
		return false
	}

	// Check expiry
	expiresAt := binary.BigEndian.Uint64(flags[16:24])
	if expiresAt > 0 && currentTime.Uint64() > expiresAt {
		return false
	}

	return true
}

func (s *Storage) GetPCRDetails(pcrHash common.Hash) (ApprovedPCR, bool) {
	flagsSlot := s.computeSlot(slotPCRFlags, pcrHash)
	val := s.stateDB.GetState(s.address, flagsSlot)

	if val == (common.Hash{}) {
		return ApprovedPCR{}, false
	}

	flags := val.Bytes()
	version := s.loadString(slotPCRVersion, pcrHash)

	return ApprovedPCR{
		PCRHash:    pcrHash,
		Active:     flags[0] == 1,
		ApprovedAt: new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[8:16])),
		ExpiresAt:  new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[16:24])),
		Version:    version,
	}, true
}

func (s *Storage) GetActivePCRs(currentTime *big.Int) []common.Hash {
	countSlot := s.computeBaseSlot(slotPCRCount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	pcrs := make([]common.Hash, 0, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotWithIndex(slotPCRList, common.Hash{}, i)
		pcrHash := s.stateDB.GetState(s.address, listSlot)

		if s.IsPCRApproved(pcrHash, currentTime) {
			pcrs = append(pcrs, pcrHash)
		}
	}
	return pcrs
}

// ============ TEE Storage ============

func (s *Storage) StoreTEE(info TEEInfo) {
	teeId := info.TEEId

	// Store owner
	s.stateDB.SetState(s.address, s.computeSlot(slotTEEOwner, teeId),
		common.BytesToHash(common.LeftPadBytes(info.Owner.Bytes(), 32)))

	// Store flags: active (1 byte) + teeType (1 byte) + registeredAt (8 bytes) + lastUpdatedAt (8 bytes)
	flags := make([]byte, 32)
	if info.Active {
		flags[0] = 1
	}
	flags[1] = info.TEEType
	if info.RegisteredAt != nil {
		binary.BigEndian.PutUint64(flags[8:16], info.RegisteredAt.Uint64())
	}
	if info.LastUpdatedAt != nil {
		binary.BigEndian.PutUint64(flags[16:24], info.LastUpdatedAt.Uint64())
	}
	s.stateDB.SetState(s.address, s.computeSlot(slotTEEFlags, teeId), common.BytesToHash(flags))

	// Store payment address
	s.stateDB.SetState(s.address, s.computeSlot(slotTEEPaymentAddress, teeId),
		common.BytesToHash(common.LeftPadBytes(info.PaymentAddress.Bytes(), 32)))

	// Store endpoint (uses separate slot prefix)
	s.storeString(slotTEEEndpoint, teeId, info.Endpoint)

	// Store PCR hash
	s.stateDB.SetState(s.address, s.computeSlot(slotTEEPCRHash, teeId), info.PCRHash)

	// Store public key (uses separate slot prefix)
	s.storeBytes(slotTEEPublicKey, teeId, info.PublicKey)

	// Store TLS certificate
	s.storeBytes(slotTEETLSCert, teeId, info.TLSCertificate)

	// Add to active list if active
	if info.Active {
		s.addToActiveTEEList(teeId)
	}

	// Add to owner's list
	s.addToOwnerList(info.Owner, teeId)

	// Add to type list
	s.addToTypeList(info.TEEType, teeId)
}

func (s *Storage) LoadTEE(teeId common.Hash) (TEEInfo, bool) {
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEOwner, teeId))
	if ownerHash == (common.Hash{}) {
		return TEEInfo{}, false
	}

	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEFlags, teeId))
	flags := flagsHash.Bytes()

	paymentHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEPaymentAddress, teeId))
	pcrHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEPCRHash, teeId))

	endpoint := s.loadString(slotTEEEndpoint, teeId)
	publicKey := s.loadBytes(slotTEEPublicKey, teeId)
	tlsCertificate := s.loadBytes(slotTEETLSCert, teeId)

	return TEEInfo{
		TEEId:          teeId,
		Owner:          common.BytesToAddress(ownerHash.Bytes()[12:32]),
		PaymentAddress: common.BytesToAddress(paymentHash.Bytes()[12:32]),
		Endpoint:       endpoint,
		PublicKey:      publicKey,
		TLSCertificate: tlsCertificate,
		PCRHash:        pcrHash,
		TEEType:        flags[1],
		Active:         flags[0] == 1,
		RegisteredAt:   new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[8:16])),
		LastUpdatedAt:  new(big.Int).SetUint64(binary.BigEndian.Uint64(flags[16:24])),
	}, true
}

func (s *Storage) Exists(teeId common.Hash) bool {
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEOwner, teeId))
	return ownerHash != (common.Hash{})
}

func (s *Storage) GetOwner(teeId common.Hash) (common.Address, bool) {
	ownerHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEOwner, teeId))
	if ownerHash == (common.Hash{}) {
		return common.Address{}, false
	}
	return common.BytesToAddress(ownerHash.Bytes()[12:32]), true
}

func (s *Storage) IsActive(teeId common.Hash) bool {
	if !s.Exists(teeId) {
		return false
	}
	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEFlags, teeId))
	return flagsHash.Bytes()[0] == 1
}

func (s *Storage) SetActive(teeId common.Hash, active bool, timestamp *big.Int) error {
	if !s.Exists(teeId) {
		return ErrTEENotFound
	}

	flagsHash := s.stateDB.GetState(s.address, s.computeSlot(slotTEEFlags, teeId))
	flags := flagsHash.Bytes()

	wasActive := flags[0] == 1

	if active {
		flags[0] = 1
	} else {
		flags[0] = 0
	}
	binary.BigEndian.PutUint64(flags[16:24], timestamp.Uint64())

	s.stateDB.SetState(s.address, s.computeSlot(slotTEEFlags, teeId), common.BytesToHash(flags))

	// Update active list
	if active && !wasActive {
		s.addToActiveTEEList(teeId)
	} else if !active && wasActive {
		s.removeFromActiveTEEList(teeId)
	}

	return nil
}

func (s *Storage) GetPublicKey(teeId common.Hash) ([]byte, error) {
	if !s.Exists(teeId) {
		return nil, ErrTEENotFound
	}
	return s.loadBytes(slotTEEPublicKey, teeId), nil
}

func (s *Storage) GetTLSCertificate(teeId common.Hash) ([]byte, error) {
	if !s.Exists(teeId) {
		return nil, ErrTEENotFound
	}
	return s.loadBytes(slotTEETLSCert, teeId), nil
}

// ============ Active TEE List ============

func (s *Storage) addToActiveTEEList(teeId common.Hash) {
	countSlot := s.computeBaseSlot(slotActiveTEECount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	// Store index for O(1) removal
	indexSlot := s.computeSlot(slotActiveTEEIndex, teeId)
	s.stateDB.SetState(s.address, indexSlot, common.BigToHash(big.NewInt(int64(count))))

	// Add to list
	listSlot := s.computeSlotWithIndex(slotActiveTEEList, common.Hash{}, count)
	s.stateDB.SetState(s.address, listSlot, teeId)

	// Increment count
	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))
}

func (s *Storage) removeFromActiveTEEList(teeId common.Hash) {
	indexSlot := s.computeSlot(slotActiveTEEIndex, teeId)
	indexHash := s.stateDB.GetState(s.address, indexSlot)
	index := new(big.Int).SetBytes(indexHash.Bytes()).Uint64()

	countSlot := s.computeBaseSlot(slotActiveTEECount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	if count == 0 {
		return
	}

	// Swap with last element
	lastIndex := count - 1
	if index != lastIndex {
		lastSlot := s.computeSlotWithIndex(slotActiveTEEList, common.Hash{}, lastIndex)
		lastTeeId := s.stateDB.GetState(s.address, lastSlot)

		// Move last to removed position
		listSlot := s.computeSlotWithIndex(slotActiveTEEList, common.Hash{}, index)
		s.stateDB.SetState(s.address, listSlot, lastTeeId)

		// Update last element's index
		lastIndexSlot := s.computeSlot(slotActiveTEEIndex, lastTeeId)
		s.stateDB.SetState(s.address, lastIndexSlot, common.BigToHash(big.NewInt(int64(index))))
	}

	// Clear removed element's index
	s.stateDB.SetState(s.address, indexSlot, common.Hash{})

	// Decrement count
	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count-1))))
}

func (s *Storage) GetActiveTEEs() []common.Hash {
	countSlot := s.computeBaseSlot(slotActiveTEECount)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	tees := make([]common.Hash, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotWithIndex(slotActiveTEEList, common.Hash{}, i)
		tees[i] = s.stateDB.GetState(s.address, listSlot)
	}
	return tees
}

// ============ TEE By Owner ============

func (s *Storage) addToOwnerList(owner common.Address, teeId common.Hash) {
	countSlot := s.computeSlotAddress(slotTEEByOwnerCount, owner)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	listSlot := s.computeSlotAddressIndex(slotTEEByOwner, owner, count)
	s.stateDB.SetState(s.address, listSlot, teeId)

	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))
}

func (s *Storage) GetTEEsByOwner(owner common.Address) []common.Hash {
	countSlot := s.computeSlotAddress(slotTEEByOwnerCount, owner)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	tees := make([]common.Hash, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotAddressIndex(slotTEEByOwner, owner, i)
		tees[i] = s.stateDB.GetState(s.address, listSlot)
	}
	return tees
}

// ============ TEE By Type ============

func (s *Storage) addToTypeList(teeType uint8, teeId common.Hash) {
	countSlot := s.computeSlotUint8(slotTEEByTypeCount, teeType)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	listSlot := s.computeSlotUint8Index(slotTEEByType, teeType, count)
	s.stateDB.SetState(s.address, listSlot, teeId)

	s.stateDB.SetState(s.address, countSlot, common.BigToHash(big.NewInt(int64(count+1))))
}

func (s *Storage) GetTEEsByType(teeType uint8) []common.Hash {
	countSlot := s.computeSlotUint8(slotTEEByTypeCount, teeType)
	countHash := s.stateDB.GetState(s.address, countSlot)
	count := new(big.Int).SetBytes(countHash.Bytes()).Uint64()

	tees := make([]common.Hash, count)
	for i := uint64(0); i < count; i++ {
		listSlot := s.computeSlotUint8Index(slotTEEByType, teeType, i)
		tees[i] = s.stateDB.GetState(s.address, listSlot)
	}
	return tees
}

// ============ AWS Certificate ============
// Stores full certificate bytes (PEM format) for dynamic updates

func (s *Storage) SetAWSRootCertificate(cert []byte) {
	s.storeBytes(slotAWSRootCert, common.Hash{}, cert)
}

func (s *Storage) GetAWSRootCertificate() []byte {
	return s.loadBytes(slotAWSRootCert, common.Hash{})
}

func (s *Storage) GetAWSRootCertificateHash() common.Hash {
	cert := s.GetAWSRootCertificate()
	if len(cert) == 0 {
		return common.Hash{}
	}
	return crypto.Keccak256Hash(cert)
}

func (s *Storage) HasAWSRootCertificate() bool {
	cert := s.GetAWSRootCertificate()
	return len(cert) > 0
}

// ============ Settlement Tracking ============
// For replay protection - tracks which settlements have been verified

func (s *Storage) IsSettlementUsed(settlementHash common.Hash) bool {
	slot := s.computeSlot(slotSettlementUsed, settlementHash)
	val := s.stateDB.GetState(s.address, slot)
	return val[31] == 1
}

func (s *Storage) MarkSettlementUsed(settlementHash common.Hash) {
	slot := s.computeSlot(slotSettlementUsed, settlementHash)
	s.stateDB.SetState(s.address, slot, common.BytesToHash([]byte{1}))
}

// ComputeSettlementHash computes a unique hash for a settlement
func ComputeSettlementHash(teeId common.Hash, inputHash, outputHash [32]byte, timestamp *big.Int) common.Hash {
	data := make([]byte, 128)
	copy(data[0:32], teeId.Bytes())
	copy(data[32:64], inputHash[:])
	copy(data[64:96], outputHash[:])
	timestampBytes := timestamp.Bytes()
	copy(data[128-len(timestampBytes):128], timestampBytes)
	return crypto.Keccak256Hash(data)
}

// ============ Byte/String Storage Helpers ============

// storeBytes stores variable-length bytes with a hash key
func (s *Storage) storeBytes(prefix byte, key common.Hash, data []byte) {
	// Use a sub-slot for length to avoid collision with other data
	lengthSlot := s.computeSlotWithIndex(prefix, key, 0)
	s.stateDB.SetState(s.address, lengthSlot, common.BigToHash(big.NewInt(int64(len(data)))))

	// Store data in chunks starting from index 1
	for i := 0; i < len(data); i += 32 {
		chunk := [32]byte{}
		end := i + 32
		if end > len(data) {
			end = len(data)
		}
		copy(chunk[:], data[i:end])

		chunkSlot := s.computeSlotWithIndex(prefix, key, uint64(i/32)+1)
		s.stateDB.SetState(s.address, chunkSlot, chunk)
	}
}

// loadBytes loads variable-length bytes with a hash key
func (s *Storage) loadBytes(prefix byte, key common.Hash) []byte {
	lengthSlot := s.computeSlotWithIndex(prefix, key, 0)
	lengthHash := s.stateDB.GetState(s.address, lengthSlot)
	length := new(big.Int).SetBytes(lengthHash.Bytes()).Int64()

	if length == 0 {
		return nil
	}

	data := make([]byte, length)
	for i := int64(0); i < length; i += 32 {
		chunkSlot := s.computeSlotWithIndex(prefix, key, uint64(i/32)+1)
		chunk := s.stateDB.GetState(s.address, chunkSlot)

		end := i + 32
		if end > length {
			end = length
		}
		copy(data[i:end], chunk.Bytes()[:end-i])
	}

	return data
}

// storeString stores a string with a hash key
func (s *Storage) storeString(prefix byte, key common.Hash, str string) {
	s.storeBytes(prefix, key, []byte(str))
}

// loadString loads a string with a hash key
func (s *Storage) loadString(prefix byte, key common.Hash) string {
	data := s.loadBytes(prefix, key)
	return string(data)
}

// storeStringUint8 stores a string with a uint8 key
func (s *Storage) storeStringUint8(prefix byte, id uint8, str string) {
	// Convert uint8 to hash for consistent slot computation
	key := common.BytesToHash([]byte{id})
	s.storeBytes(prefix, key, []byte(str))
}

// loadStringUint8 loads a string with a uint8 key
func (s *Storage) loadStringUint8(prefix byte, id uint8) string {
	key := common.BytesToHash([]byte{id})
	return string(s.loadBytes(prefix, key))
}
