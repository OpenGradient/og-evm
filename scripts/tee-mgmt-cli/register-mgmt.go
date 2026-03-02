package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ============================================================================
// Configuration (from .env file or environment)
// ============================================================================

func init() {
	loadEnvFile(".env")
}

func loadEnvFile(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" && value != "" {
			os.Setenv(key, value)
		}
	}
}

var (
	RPC_URL              = getEnvOrDefault("TEE_RPC_URL", "http://13.59.43.94:8545")
	TEE_REGISTRY_ADDRESS = getEnvOrDefault("TEE_REGISTRY_ADDRESS", "0x3d641a2791533b4a0000345ea8d509d01e1ec301")
	PRIVATE_KEY          = os.Getenv("TEE_PRIVATE_KEY")
)

// ============================================================================
// AccessControl Role Constants
// ============================================================================

var (
	DEFAULT_ADMIN_ROLE = [32]byte{}
	TEE_OPERATOR_ROLE  = crypto.Keccak256Hash([]byte("TEE_OPERATOR"))
)

// ============================================================================
// Method Selectors
// ============================================================================

var (
	SEL_GRANT_ROLE          = crypto.Keccak256([]byte("grantRole(bytes32,address)"))[:4]
	SEL_REVOKE_ROLE         = crypto.Keccak256([]byte("revokeRole(bytes32,address)"))[:4]
	SEL_HAS_ROLE            = crypto.Keccak256([]byte("hasRole(bytes32,address)"))[:4]
	SEL_ADD_TEE_TYPE        = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	SEL_DEACTIVATE_TEE_TYPE = crypto.Keccak256([]byte("deactivateTEEType(uint8)"))[:4]
	SEL_IS_VALID_TYPE       = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	SEL_APPROVE_PCR         = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,bytes32,uint256)"))[:4]
	SEL_REVOKE_PCR          = crypto.Keccak256([]byte("revokePCR(bytes32)"))[:4]
	SEL_IS_PCR_APPROVED     = crypto.Keccak256([]byte("isPCRApproved(bytes32)"))[:4]
	SEL_COMPUTE_PCR_HASH    = crypto.Keccak256([]byte("computePCRHash((bytes,bytes,bytes))"))[:4]
	SEL_GET_ACTIVE_PCRS     = crypto.Keccak256([]byte("getActivePCRs()"))[:4]
	SEL_SET_AWS_ROOT_CERT   = crypto.Keccak256([]byte("setAWSRootCertificate(bytes)"))[:4]
	SEL_REGISTER_TEE        = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]
	SEL_DEACTIVATE_TEE      = crypto.Keccak256([]byte("deactivateTEE(bytes32)"))[:4]
	SEL_ACTIVATE_TEE        = crypto.Keccak256([]byte("activateTEE(bytes32)"))[:4]
	SEL_GET_ACTIVE_TEES     = crypto.Keccak256([]byte("getActiveTEEs()"))[:4]
	SEL_GET_TEE             = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
	SEL_GET_PUBLIC_KEY      = crypto.Keccak256([]byte("getPublicKey(bytes32)"))[:4]
	SEL_GET_TLS_CERT        = crypto.Keccak256([]byte("getTLSCertificate(bytes32)"))[:4]
	SEL_IS_ACTIVE           = crypto.Keccak256([]byte("isActive(bytes32)"))[:4]
)

// ============================================================================
// Structs
// ============================================================================

type TEEInfo struct {
	Owner          common.Address
	PaymentAddress common.Address
	Endpoint       string
	PCRHash        [32]byte
	TEEType        uint8
	IsActive       bool
	RegisteredAt   time.Time
	LastUpdatedAt  time.Time
}

type AttestationResponse struct {
	PublicKey   string `json:"public_key"`
	EnclaveInfo *struct {
		InstanceType string `json:"instance_type"`
		Platform     string `json:"platform"`
		Version      string `json:"version"`
	} `json:"enclave_info,omitempty"`
	Measurements *struct {
		PCR0 string `json:"PCR0"`
		PCR1 string `json:"PCR1"`
		PCR2 string `json:"PCR2"`
	} `json:"measurements,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type MeasurementsFile struct {
	Measurements struct {
		PCR0 string `json:"PCR0"`
		PCR1 string `json:"PCR1"`
		PCR2 string `json:"PCR2"`
	} `json:"Measurements"`
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "list":
		cmdList()
	case "show":
		requireArg(2, "show <tee_id>")
		cmdShow(os.Args[2])
	case "register":
		cmdRegister()
	case "deactivate":
		requireArg(2, "deactivate <tee_id>")
		cmdDeactivate(os.Args[2])
	case "activate":
		requireArg(2, "activate <tee_id>")
		cmdActivate(os.Args[2])
	case "pcr-list":
		cmdPCRList()
	case "pcr-approve":
		cmdPCRApprove()
	case "pcr-revoke":
		requireArg(2, "pcr-revoke <pcr_hash>")
		cmdPCRRevoke(os.Args[2])
	case "pcr-check":
		requireArg(2, "pcr-check <pcr_hash>")
		cmdPCRCheck(os.Args[2])
	case "pcr-compute":
		cmdPCRCompute()
	case "type-list":
		cmdTypeList()
	case "type-add":
		requireArg(3, "type-add <type_id> <name>")
		cmdTypeAdd(os.Args[2], os.Args[3])
	case "type-deactivate":
		requireArg(2, "type-deactivate <type_id>")
		cmdTypeDeactivate(os.Args[2])
	case "set-aws-cert":
		requireArg(2, "set-aws-cert <cert_file>")
		cmdSetAWSCert(os.Args[2])
	case "add-admin":
		requireArg(2, "add-admin <address>")
		cmdAddAdmin(os.Args[2])
	case "add-operator":
		requireArg(2, "add-operator <address>")
		cmdAddOperator(os.Args[2])
	case "revoke-admin":
		requireArg(2, "revoke-admin <address>")
		cmdRevokeAdmin(os.Args[2])
	case "revoke-operator":
		requireArg(2, "revoke-operator <address>")
		cmdRevokeOperator(os.Args[2])
	case "check-role":
		requireArg(3, "check-role <admin|operator> <address>")
		cmdCheckRole(os.Args[2], os.Args[3])
	case "help", "--help", "-h":
		printHelp()
	default:
		fatal("Unknown command: %s. Use 'help' for usage.", os.Args[1])
	}
}

func requireArg(index int, usage string) {
	if len(os.Args) <= index {
		fatal("Usage: %s %s", os.Args[0], usage)
	}
}

// ============================================================================
// TEE COMMANDS
// ============================================================================

func cmdList() {
	fmt.Println("=== Active TEEs in Registry ===")
	fmt.Printf("Registry: %s\n", TEE_REGISTRY_ADDRESS)
	fmt.Printf("RPC: %s\n\n", RPC_URL)

	tees, err := callGetActiveTEEs()
	if err != nil {
		fatal("Failed to get active TEEs: %v", err)
	}

	fmt.Printf("Found %d active TEE(s)\n\n", len(tees))
	for i, teeId := range tees {
		fmt.Printf("  [%d] 0x%s\n", i+1, teeId)
	}
}

func cmdShow(teeIdStr string) {
	teeId := parseBytes32(teeIdStr)
	fmt.Printf("=== TEE Details: 0x%s ===\n", hex.EncodeToString(teeId[:]))

	info, err := callGetTEE(teeId)
	if err != nil {
		fatal("Failed to get TEE info: %v", err)
	}

	fmt.Printf("  Owner:          %s\n", info.Owner.Hex())
	fmt.Printf("  Payment Addr:   %s\n", info.PaymentAddress.Hex())
	fmt.Printf("  Endpoint:       %s\n", info.Endpoint)
	fmt.Printf("  PCR Hash:       0x%s\n", hex.EncodeToString(info.PCRHash[:]))
	fmt.Printf("  TEE Type:       %d (%s)\n", info.TEEType, getTEETypeName(info.TEEType))
	fmt.Printf("  Active:         %v\n", info.IsActive)
	fmt.Printf("  Registered:     %s UTC\n", info.RegisteredAt.UTC().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Last Updated:   %s UTC\n", info.LastUpdatedAt.UTC().Format("2006-01-02 15:04:05"))

	fmt.Println("\n  --- Public Key ---")
	if pubKey, err := callGetPublicKey(teeId); err == nil && len(pubKey) > 0 {
		fmt.Printf("  Size: %d bytes\n", len(pubKey))
		fmt.Printf("  Hex:  %s...\n", truncate(hex.EncodeToString(pubKey), 64))
	} else {
		fmt.Println("  Not available")
	}

	fmt.Println("\n  --- TLS Certificate ---")
	if tlsCert, err := callGetTLSCertificate(teeId); err == nil && len(tlsCert) > 0 {
		fmt.Printf("  Size: %d bytes\n", len(tlsCert))
		fmt.Printf("  Hash: 0x%s\n", hex.EncodeToString(crypto.Keccak256(tlsCert)))
	} else {
		fmt.Println("  Not available")
	}
}

func cmdRegister() {
	enclaveHost := getEnvOrDefault("ENCLAVE_HOST", "")
	enclavePort := getEnvOrDefault("ENCLAVE_PORT", "443")
	if enclaveHost == "" {
		fatal("ENCLAVE_HOST environment variable required")
	}

	account, err := getAccountAddress()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	paymentAddr := getEnvOrDefault("PAYMENT_ADDRESS", account)
	endpoint := getEnvOrDefault("TEE_ENDPOINT", fmt.Sprintf("https://%s", enclaveHost))
	teeType := uint8(parseUint(getEnvOrDefault("TEE_TYPE", "0")))

	fmt.Println("=== Registering TEE ===")
	fmt.Printf("  Enclave: %s:%s\n", enclaveHost, enclavePort)
	fmt.Printf("  Account: %s\n", account)
	fmt.Printf("  Payment: %s\n", paymentAddr)
	fmt.Printf("  Type:    %d\n\n", teeType)

	// Fetch attestation
	log("Fetching attestation document...")
	nonce := generateNonce()
	attestDoc, err := fetchAttestation(fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", enclaveHost, nonce))
	if err != nil {
		fatal("Failed to fetch attestation: %v", err)
	}
	attestBytes, _ := base64.StdEncoding.DecodeString(attestDoc)
	fmt.Printf("  ✅ Attestation: %d bytes\n", len(attestBytes))

	// Fetch signing key
	log("Fetching signing public key...")
	signingKey, err := fetchSigningPublicKey(enclaveHost)
	if err != nil {
		fatal("Failed to fetch signing key: %v", err)
	}
	fmt.Printf("  ✅ Signing Key: %d bytes\n", len(signingKey))

	// Fetch TLS cert
	log("Fetching TLS certificate...")
	tlsCert, err := fetchTLSCertificate(enclaveHost, enclavePort)
	if err != nil {
		fatal("Failed to fetch TLS cert: %v", err)
	}
	fmt.Printf("  ✅ TLS Cert: %d bytes\n", len(tlsCert))

	// Check if already registered
	expectedId := crypto.Keccak256Hash(signingKey)
	if active, _ := callIsActive(expectedId); active {
		fmt.Printf("\n⚠️  TEE already registered: 0x%s\n", hex.EncodeToString(expectedId[:]))
		return
	}

	// Register
	log("Sending registration transaction...")
	txHash, err := callRegisterTEE(account, attestBytes, signingKey, tlsCert, paymentAddr, endpoint, teeType)
	if err != nil {
		fatal("Failed to register: %v", err)
	}

	fmt.Printf("  TX: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Printf("\n✅ TEE registered! ID: 0x%s\n", hex.EncodeToString(expectedId[:]))
	} else {
		fmt.Println("\n❌ Registration failed")
	}
}

func cmdDeactivate(teeIdStr string) {
	teeId := parseBytes32(teeIdStr)
	account, _ := getAccountAddress()

	log("Deactivating TEE: 0x%s", hex.EncodeToString(teeId[:]))
	txHash, err := callDeactivateTEE(account, teeId)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "TEE deactivated")
}

func cmdActivate(teeIdStr string) {
	teeId := parseBytes32(teeIdStr)
	account, _ := getAccountAddress()

	log("Activating TEE: 0x%s", hex.EncodeToString(teeId[:]))
	txHash, err := callActivateTEE(account, teeId)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "TEE activated")
}

// ============================================================================
// PCR COMMANDS
// ============================================================================

func cmdPCRList() {
	fmt.Println("=== Active PCRs ===")
	pcrs, err := callGetActivePCRs()
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("Found %d active PCR(s)\n\n", len(pcrs))
	for i, h := range pcrs {
		fmt.Printf("  [%d] 0x%s\n", i+1, h)
	}
}

func cmdPCRApprove() {
	pcr0, pcr1, pcr2 := loadPCRs()
	version := getEnvOrDefault("PCR_VERSION", "v1.0.0")
	gracePeriod := new(big.Int)
	gracePeriod.SetString(getEnvOrDefault("GRACE_PERIOD", "0"), 10)

	var prevPCR [32]byte
	if p := os.Getenv("PREVIOUS_PCR"); p != "" {
		prevPCR = parseBytes32(p)
	}

	pcrHash, _ := callComputePCRHash(pcr0, pcr1, pcr2)

	fmt.Println("=== Approving PCR ===")
	fmt.Printf("  PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
	fmt.Printf("  Version:  %s\n\n", version)

	account, _ := getAccountAddress()
	txHash, err := callApprovePCR(account, pcr0, pcr1, pcr2, version, prevPCR, gracePeriod)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "PCR approved")
}

func cmdPCRRevoke(hashStr string) {
	pcrHash := parseBytes32(hashStr)
	account, _ := getAccountAddress()

	log("Revoking PCR: 0x%s", hex.EncodeToString(pcrHash[:]))
	txHash, err := callRevokePCR(account, pcrHash)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "PCR revoked")
}

func cmdPCRCheck(hashStr string) {
	pcrHash := parseBytes32(hashStr)
	approved, _ := callIsPCRApproved(pcrHash)
	if approved {
		fmt.Printf("✅ PCR 0x%s is APPROVED\n", hex.EncodeToString(pcrHash[:]))
	} else {
		fmt.Printf("❌ PCR 0x%s is NOT approved\n", hex.EncodeToString(pcrHash[:]))
	}
}

func cmdPCRCompute() {
	pcr0, pcr1, pcr2 := loadPCRs()
	pcrHash, err := callComputePCRHash(pcr0, pcr1, pcr2)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
}

// ============================================================================
// TYPE COMMANDS
// ============================================================================

func cmdTypeList() {
	fmt.Println("=== TEE Types ===")
	for i := uint8(0); i <= 10; i++ {
		if valid, _ := callIsValidTEEType(i); valid {
			fmt.Printf("  [%d] %s\n", i, getTEETypeName(i))
		}
	}
}

func cmdTypeAdd(idStr, name string) {
	typeId := uint8(parseUint(idStr))
	account, _ := getAccountAddress()

	log("Adding type %d: %s", typeId, name)
	txHash, err := callAddTEEType(account, typeId, name)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Type added")
}

func cmdTypeDeactivate(idStr string) {
	typeId := uint8(parseUint(idStr))
	account, _ := getAccountAddress()

	log("Deactivating type %d", typeId)
	txHash, err := callDeactivateTEEType(account, typeId)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Type deactivated")
}

// ============================================================================
// CERTIFICATE COMMANDS
// ============================================================================

func cmdSetAWSCert(certFile string) {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		fatal("Failed to read file: %v", err)
	}

	if bytes.Contains(certData, []byte("-----BEGIN")) {
		if block, _ := pem.Decode(certData); block != nil {
			certData = block.Bytes
		}
	}

	account, _ := getAccountAddress()
	log("Setting AWS cert (%d bytes)", len(certData))

	txHash, err := callSetAWSRootCertificate(account, certData)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "AWS cert set")
}

// ============================================================================
// ROLE COMMANDS
// ============================================================================

func cmdAddAdmin(addr string) {
	account, _ := getAccountAddress()
	log("Adding admin: %s", addr)
	txHash, err := callGrantRole(account, DEFAULT_ADMIN_ROLE, addr)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Admin added")
}

func cmdAddOperator(addr string) {
	account, _ := getAccountAddress()
	log("Adding operator: %s", addr)
	txHash, err := callGrantRole(account, TEE_OPERATOR_ROLE, addr)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Operator added")
}

func cmdRevokeAdmin(addr string) {
	account, _ := getAccountAddress()
	log("Revoking admin: %s", addr)
	txHash, err := callRevokeRole(account, DEFAULT_ADMIN_ROLE, addr)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Admin revoked")
}

func cmdRevokeOperator(addr string) {
	account, _ := getAccountAddress()
	log("Revoking operator: %s", addr)
	txHash, err := callRevokeRole(account, TEE_OPERATOR_ROLE, addr)
	if err != nil {
		fatal("Failed: %v", err)
	}
	fmt.Printf("TX: %s\n", txHash)
	printTxResult(waitForTx(txHash), "Operator revoked")
}

func cmdCheckRole(role, addr string) {
	var roleBytes [32]byte
	var roleName string

	switch strings.ToLower(role) {
	case "admin":
		roleBytes = DEFAULT_ADMIN_ROLE
		roleName = "DEFAULT_ADMIN_ROLE"
	case "operator":
		roleBytes = TEE_OPERATOR_ROLE
		roleName = "TEE_OPERATOR"
	default:
		fatal("Unknown role: %s (use 'admin' or 'operator')", role)
	}

	has, _ := callHasRole(roleBytes, addr)
	if has {
		fmt.Printf("✅ %s HAS %s\n", addr, roleName)
	} else {
		fmt.Printf("❌ %s does NOT have %s\n", addr, roleName)
	}
}

// ============================================================================
// HELP
// ============================================================================

func printHelp() {
	fmt.Printf(`TEE Registry CLI

Usage: %s <command> [args]

Config (.env file or environment):
  TEE_RPC_URL           RPC endpoint
  TEE_REGISTRY_ADDRESS  Contract address
  TEE_PRIVATE_KEY       Private key for signing

TEE Commands:
  list                  List active TEEs
  show <id>             Show TEE details
  register              Register TEE (needs ENCLAVE_HOST)
  activate <id>         Activate TEE
  deactivate <id>       Deactivate TEE

PCR Commands:
  pcr-list              List approved PCRs
  pcr-approve           Approve PCR (needs MEASUREMENTS_FILE or PCR0/1/2)
  pcr-revoke <hash>     Revoke PCR
  pcr-check <hash>      Check if approved
  pcr-compute           Compute hash from measurements

Type Commands:
  type-list             List TEE types
  type-add <id> <name>  Add type
  type-deactivate <id>  Deactivate type

Role Commands:
  add-admin <addr>      Grant admin role
  add-operator <addr>   Grant operator role
  revoke-admin <addr>   Revoke admin role
  revoke-operator <addr> Revoke operator role
  check-role <role> <addr> Check role (admin|operator)

Other:
  set-aws-cert <file>   Set AWS root certificate
  help                  Show this help

Examples:
  %s list
  ENCLAVE_HOST=13.59.207.188 %s register
  %s pcr-approve
  %s check-role admin 0x...
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

// ============================================================================
// CONTRACT CALLS
// ============================================================================

func callGetActiveTEEs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_TEES)
	if err != nil {
		return nil, err
	}
	return decodeBytes32Array(result)
}

func callGetTEE(teeId [32]byte) (*TEEInfo, error) {
	data := encodeBytes32(SEL_GET_TEE, teeId)
	result, err := ethCall(data)
	if err != nil || len(result) < 320 {
		return nil, fmt.Errorf("TEE not found")
	}

	info := &TEEInfo{
		Owner:          common.BytesToAddress(result[12:32]),
		PaymentAddress: common.BytesToAddress(result[44:64]),
		TEEType:        uint8(result[223]),
		IsActive:       result[255] == 1,
		RegisteredAt:   time.Unix(new(big.Int).SetBytes(result[256:288]).Int64(), 0),
		LastUpdatedAt:  time.Unix(new(big.Int).SetBytes(result[288:320]).Int64(), 0),
	}
	copy(info.PCRHash[:], result[160:192])

	// Decode endpoint
	offset := new(big.Int).SetBytes(result[64:96]).Uint64()
	if offset < uint64(len(result)) {
		length := new(big.Int).SetBytes(result[offset : offset+32]).Uint64()
		if offset+32+length <= uint64(len(result)) {
			info.Endpoint = string(result[offset+32 : offset+32+length])
		}
	}
	return info, nil
}

func callGetPublicKey(teeId [32]byte) ([]byte, error) {
	result, err := ethCall(encodeBytes32(SEL_GET_PUBLIC_KEY, teeId))
	if err != nil {
		return nil, err
	}
	return decodeDynamicBytes(result)
}

func callGetTLSCertificate(teeId [32]byte) ([]byte, error) {
	result, err := ethCall(encodeBytes32(SEL_GET_TLS_CERT, teeId))
	if err != nil {
		return nil, err
	}
	return decodeDynamicBytes(result)
}

func callIsActive(teeId [32]byte) (bool, error) {
	result, err := ethCall(encodeBytes32(SEL_IS_ACTIVE, teeId))
	return len(result) >= 32 && result[31] == 1, err
}

func callRegisterTEE(from string, attestation, signingKey, tlsCert []byte, paymentAddr, endpoint string, teeType uint8) (string, error) {
	bytesT, _ := abi.NewType("bytes", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	strT, _ := abi.NewType("string", "", nil)
	u8T, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{{Type: bytesT}, {Type: bytesT}, {Type: bytesT}, {Type: addrT}, {Type: strT}, {Type: u8T}}
	encoded, _ := args.Pack(attestation, signingKey, tlsCert, common.HexToAddress(paymentAddr), endpoint, teeType)
	return sendTx(from, append(SEL_REGISTER_TEE, encoded...))
}

func callDeactivateTEE(from string, teeId [32]byte) (string, error) {
	return sendTx(from, encodeBytes32(SEL_DEACTIVATE_TEE, teeId))
}

func callActivateTEE(from string, teeId [32]byte) (string, error) {
	return sendTx(from, encodeBytes32(SEL_ACTIVATE_TEE, teeId))
}

func callGetActivePCRs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_PCRS)
	if err != nil {
		return nil, err
	}
	return decodeBytes32Array(result)
}

func callComputePCRHash(pcr0, pcr1, pcr2 []byte) ([32]byte, error) {
	tupleT, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"}, {Name: "pcr1", Type: "bytes"}, {Name: "pcr2", Type: "bytes"},
	})
	args := abi.Arguments{{Type: tupleT}}
	encoded, _ := args.Pack(struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2})
	result, err := ethCall(append(SEL_COMPUTE_PCR_HASH, encoded...))
	var hash [32]byte
	if len(result) >= 32 {
		copy(hash[:], result[:32])
	}
	return hash, err
}

func callApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string, prevPCR [32]byte, grace *big.Int) (string, error) {
	tupleT, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"}, {Name: "pcr1", Type: "bytes"}, {Name: "pcr2", Type: "bytes"},
	})
	strT, _ := abi.NewType("string", "", nil)
	b32T, _ := abi.NewType("bytes32", "", nil)
	u256T, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{{Type: tupleT}, {Type: strT}, {Type: b32T}, {Type: u256T}}
	encoded, _ := args.Pack(struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}, version, prevPCR, grace)
	return sendTx(from, append(SEL_APPROVE_PCR, encoded...))
}

func callRevokePCR(from string, pcrHash [32]byte) (string, error) {
	return sendTx(from, encodeBytes32(SEL_REVOKE_PCR, pcrHash))
}

func callIsPCRApproved(pcrHash [32]byte) (bool, error) {
	result, err := ethCall(encodeBytes32(SEL_IS_PCR_APPROVED, pcrHash))
	return len(result) >= 32 && result[31] == 1, err
}

func callIsValidTEEType(typeId uint8) (bool, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}}.Pack(typeId)
	result, err := ethCall(append(SEL_IS_VALID_TYPE, encoded...))
	return len(result) >= 32 && result[31] == 1, err
}

func callAddTEEType(from string, typeId uint8, name string) (string, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	strT, _ := abi.NewType("string", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}, {Type: strT}}.Pack(typeId, name)
	return sendTx(from, append(SEL_ADD_TEE_TYPE, encoded...))
}

func callDeactivateTEEType(from string, typeId uint8) (string, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}}.Pack(typeId)
	return sendTx(from, append(SEL_DEACTIVATE_TEE_TYPE, encoded...))
}

func callSetAWSRootCertificate(from string, cert []byte) (string, error) {
	bytesT, _ := abi.NewType("bytes", "", nil)
	encoded, _ := abi.Arguments{{Type: bytesT}}.Pack(cert)
	return sendTx(from, append(SEL_SET_AWS_ROOT_CERT, encoded...))
}

func callHasRole(role [32]byte, account string) (bool, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	result, err := ethCall(append(SEL_HAS_ROLE, encoded...))
	return len(result) >= 32 && result[31] == 1, err
}

func callGrantRole(from string, role [32]byte, account string) (string, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	return sendTx(from, append(SEL_GRANT_ROLE, encoded...))
}

func callRevokeRole(from string, role [32]byte, account string) (string, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	return sendTx(from, append(SEL_REVOKE_ROLE, encoded...))
}

// ============================================================================
// RPC / TX HELPERS
// ============================================================================

func getAccountAddress() (string, error) {
	if PRIVATE_KEY != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(PRIVATE_KEY, "0x"))
		if err != nil {
			return "", err
		}
		return crypto.PubkeyToAddress(key.PublicKey).Hex(), nil
	}
	return getFirstAccount()
}

func getFirstAccount() (string, error) {
	resp, _ := rpcCall("eth_accounts", []interface{}{})
	var result struct{ Result []string }
	json.Unmarshal(resp, &result)
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no accounts (set TEE_PRIVATE_KEY)")
	}
	return result.Result[0], nil
}

func ethCall(data []byte) ([]byte, error) {
	params := []interface{}{map[string]string{"to": TEE_REGISTRY_ADDRESS, "data": "0x" + hex.EncodeToString(data)}, "latest"}
	resp, err := rpcCall("eth_call", params)
	if err != nil {
		return nil, err
	}
	var result struct{ Result string }
	json.Unmarshal(resp, &result)
	if len(result.Result) > 2 {
		return hex.DecodeString(result.Result[2:])
	}
	return nil, nil
}

func sendTx(from string, data []byte) (string, error) {
	if PRIVATE_KEY != "" {
		return sendTxSigned(data)
	}
	return sendTxUnlocked(from, data)
}

func sendTxUnlocked(from string, data []byte) (string, error) {
	params := []interface{}{map[string]string{"from": from, "to": TEE_REGISTRY_ADDRESS, "gas": "0x500000", "data": "0x" + hex.EncodeToString(data)}}
	resp, _ := rpcCall("eth_sendTransaction", params)
	var result struct{ Result string }
	json.Unmarshal(resp, &result)
	if result.Result == "" {
		return "", fmt.Errorf("tx failed")
	}
	return result.Result, nil
}

func sendTxSigned(data []byte) (string, error) {
	client, err := ethclient.Dial(RPC_URL)
	if err != nil {
		return "", err
	}
	defer client.Close()

	key, _ := crypto.HexToECDSA(strings.TrimPrefix(PRIVATE_KEY, "0x"))
	from := crypto.PubkeyToAddress(key.PublicKey)

	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	chainID, _ := client.NetworkID(context.Background())

	tx := types.NewTransaction(nonce, common.HexToAddress(TEE_REGISTRY_ADDRESS), big.NewInt(0), 5000000, gasPrice, data)
	signed, _ := types.SignTx(tx, types.NewEIP155Signer(chainID), key)

	if err := client.SendTransaction(context.Background(), signed); err != nil {
		return "", err
	}
	return signed.Hash().Hex(), nil
}

func waitForTx(txHash string) bool {
	log("Waiting for confirmation...")
	for i := 0; i < 30; i++ {
		resp, _ := rpcCall("eth_getTransactionReceipt", []string{txHash})
		var result struct{ Result *struct{ Status string } }
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			return result.Result.Status == "0x1"
		}
		time.Sleep(time.Second)
	}
	return false
}

func rpcCall(method string, params interface{}) ([]byte, error) {
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": 1})
	resp, err := http.Post(RPC_URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ============================================================================
// NETWORK HELPERS
// ============================================================================

func generateNonce() string {
	b := make([]byte, 20)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (i * 8))
	}
	return hex.EncodeToString(b)
}

func fetchAttestation(url string) (string, error) {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(bytes.TrimSpace(body)), nil
}

func fetchSigningPublicKey(host string) ([]byte, error) {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 30 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://%s/signing-key", host))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var data AttestationResponse
	json.Unmarshal(body, &data)

	if block, _ := pem.Decode([]byte(data.PublicKey)); block != nil {
		return block.Bytes, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(data.PublicKey); err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("invalid key format")
}

func fetchTLSCertificate(host, port string) ([]byte, error) {
	conn, err := tls.Dial("tcp", host+":"+port, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if certs := conn.ConnectionState().PeerCertificates; len(certs) > 0 {
		return certs[0].Raw, nil
	}
	return nil, fmt.Errorf("no certs")
}

func loadPCRs() ([]byte, []byte, []byte) {
	file := getEnvOrDefault("MEASUREMENTS_FILE", "measurements.txt")
	if data, err := os.ReadFile(file); err == nil {
		var m MeasurementsFile
		if json.Unmarshal(data, &m) == nil {
			pcr0, _ := hex.DecodeString(m.Measurements.PCR0)
			pcr1, _ := hex.DecodeString(m.Measurements.PCR1)
			pcr2, _ := hex.DecodeString(m.Measurements.PCR2)
			return pcr0, pcr1, pcr2
		}
	}

	pcr0Hex, pcr1Hex, pcr2Hex := os.Getenv("PCR0"), os.Getenv("PCR1"), os.Getenv("PCR2")
	if pcr0Hex == "" || pcr1Hex == "" || pcr2Hex == "" {
		fatal("Need MEASUREMENTS_FILE or PCR0/PCR1/PCR2")
	}
	pcr0, _ := hex.DecodeString(strings.TrimPrefix(pcr0Hex, "0x"))
	pcr1, _ := hex.DecodeString(strings.TrimPrefix(pcr1Hex, "0x"))
	pcr2, _ := hex.DecodeString(strings.TrimPrefix(pcr2Hex, "0x"))
	return pcr0, pcr1, pcr2
}

// ============================================================================
// UTILITY
// ============================================================================

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseBytes32(s string) [32]byte {
	s = strings.TrimPrefix(s, "0x")
	for len(s) < 64 {
		s = "0" + s
	}
	decoded, _ := hex.DecodeString(s)
	var result [32]byte
	copy(result[:], decoded)
	return result
}

func parseUint(s string) uint64 {
	var v uint64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func encodeBytes32(selector []byte, val [32]byte) []byte {
	return append(selector, val[:]...)
}

func decodeBytes32Array(result []byte) ([]string, error) {
	if len(result) < 64 {
		return []string{}, nil
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	items := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			items[i] = hex.EncodeToString(result[start : start+32])
		}
	}
	return items, nil
}

func decodeDynamicBytes(result []byte) ([]byte, error) {
	if len(result) < 64 {
		return nil, nil
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if uint64(len(result)) >= 64+length {
		return result[64 : 64+length], nil
	}
	return nil, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func getTEETypeName(id uint8) string {
	names := map[uint8]string{0: "LLMProxy", 1: "Validator"}
	if n, ok := names[id]; ok {
		return n
	}
	return "Unknown"
}

func log(format string, args ...interface{}) {
	fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func printTxResult(success bool, msg string) {
	if success {
		fmt.Printf("✅ %s\n", msg)
	} else {
		fmt.Println("❌ Transaction failed")
	}
}
