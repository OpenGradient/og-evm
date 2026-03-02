// ============================================================================
// DEBUG COMMANDS
// ============================================================================

func cmdDebugEnclave() {
	enclaveHost := getEnvOrDefault("ENCLAVE_HOST", "")
	if enclaveHost == "" {
		fatal("ENCLAVE_HOSTpackage main

import (
	"bytes"
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
	"github.com/ethereum/go-ethereum/crypto"
)

// ============================================================================
// Configuration (from environment or defaults)
// ============================================================================

var (
	RPC_URL              = getEnvOrDefault("TEE_RPC_URL", "http://13.59.43.94:8545")
	TEE_REGISTRY_ADDRESS = getEnvOrDefault("TEE_REGISTRY_ADDRESS", "0x3d641a2791533b4a0000345ea8d509d01e1ec301")
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
	// AccessControl
	SEL_GRANT_ROLE  = crypto.Keccak256([]byte("grantRole(bytes32,address)"))[:4]
	SEL_REVOKE_ROLE = crypto.Keccak256([]byte("revokeRole(bytes32,address)"))[:4]
	SEL_HAS_ROLE    = crypto.Keccak256([]byte("hasRole(bytes32,address)"))[:4]

	// TEE Type Management
	SEL_ADD_TEE_TYPE        = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	SEL_DEACTIVATE_TEE_TYPE = crypto.Keccak256([]byte("deactivateTEEType(uint8)"))[:4]
	SEL_IS_VALID_TYPE       = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	SEL_GET_TEE_TYPES       = crypto.Keccak256([]byte("getTEETypes()"))[:4]

	// PCR Management
	SEL_APPROVE_PCR      = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,bytes32,uint256)"))[:4]
	SEL_REVOKE_PCR       = crypto.Keccak256([]byte("revokePCR(bytes32)"))[:4]
	SEL_IS_PCR_APPROVED  = crypto.Keccak256([]byte("isPCRApproved(bytes32)"))[:4]
	SEL_COMPUTE_PCR_HASH = crypto.Keccak256([]byte("computePCRHash((bytes,bytes,bytes))"))[:4]
	SEL_GET_ACTIVE_PCRS  = crypto.Keccak256([]byte("getActivePCRs()"))[:4]

	// Certificate Management
	SEL_SET_AWS_ROOT_CERT = crypto.Keccak256([]byte("setAWSRootCertificate(bytes)"))[:4]

	// TEE Registration & Management
	SEL_REGISTER_TEE   = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]
	SEL_DEACTIVATE_TEE = crypto.Keccak256([]byte("deactivateTEE(bytes32)"))[:4]
	SEL_ACTIVATE_TEE   = crypto.Keccak256([]byte("activateTEE(bytes32)"))[:4]

	// Queries
	SEL_GET_ACTIVE_TEES   = crypto.Keccak256([]byte("getActiveTEEs()"))[:4]
	SEL_GET_TEE           = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
	SEL_GET_PUBLIC_KEY    = crypto.Keccak256([]byte("getPublicKey(bytes32)"))[:4]
	SEL_GET_TLS_CERT      = crypto.Keccak256([]byte("getTLSCertificate(bytes32)"))[:4]
	SEL_IS_ACTIVE         = crypto.Keccak256([]byte("isActive(bytes32)"))[:4]
	SEL_GET_TEES_BY_TYPE  = crypto.Keccak256([]byte("getTEEsByType(uint8)"))[:4]
	SEL_GET_TEES_BY_OWNER = crypto.Keccak256([]byte("getTEEsByOwner(address)"))[:4]
)

// ============================================================================
// Structs
// ============================================================================

type TEEInfo struct {
	Owner          common.Address
	PaymentAddress common.Address
	Endpoint       string
	PublicKey      string
	TLSCertificate string
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

	command := os.Args[1]

	switch command {
	// TEE Commands
	case "list":
		cmdList()
	case "show":
		if len(os.Args) < 3 {
			fatal("Usage: %s show <tee_id>", os.Args[0])
		}
		cmdShow(os.Args[2])
	case "register":
		cmdRegister()
	case "deactivate":
		if len(os.Args) < 3 {
			fatal("Usage: %s deactivate <tee_id>", os.Args[0])
		}
		cmdDeactivate(os.Args[2])
	case "activate":
		if len(os.Args) < 3 {
			fatal("Usage: %s activate <tee_id>", os.Args[0])
		}
		cmdActivate(os.Args[2])

	// PCR Commands
	case "pcr-list":
		cmdPCRList()
	case "pcr-approve":
		cmdPCRApprove()
	case "pcr-revoke":
		if len(os.Args) < 3 {
			fatal("Usage: %s pcr-revoke <pcr_hash>", os.Args[0])
		}
		cmdPCRRevoke(os.Args[2])
	case "pcr-check":
		if len(os.Args) < 3 {
			fatal("Usage: %s pcr-check <pcr_hash>", os.Args[0])
		}
		cmdPCRCheck(os.Args[2])
	case "pcr-compute":
		cmdPCRCompute()

	// TEE Type Commands
	case "type-list":
		cmdTypeList()
	case "type-add":
		if len(os.Args) < 4 {
			fatal("Usage: %s type-add <type_id> <name>", os.Args[0])
		}
		cmdTypeAdd(os.Args[2], os.Args[3])
	case "type-deactivate":
		if len(os.Args) < 3 {
			fatal("Usage: %s type-deactivate <type_id>", os.Args[0])
		}
		cmdTypeDeactivate(os.Args[2])

	// Certificate Commands
	case "set-aws-cert":
		if len(os.Args) < 3 {
			fatal("Usage: %s set-aws-cert <cert_file>", os.Args[0])
		}
		cmdSetAWSCert(os.Args[2])

	// Role Commands
	case "add-admin":
		if len(os.Args) < 3 {
			fatal("Usage: %s add-admin <address>", os.Args[0])
		}
		cmdAddAdmin(os.Args[2])
	case "add-operator":
		if len(os.Args) < 3 {
			fatal("Usage: %s add-operator <address>", os.Args[0])
		}
		cmdAddOperator(os.Args[2])
	case "revoke-admin":
		if len(os.Args) < 3 {
			fatal("Usage: %s revoke-admin <address>", os.Args[0])
		}
		cmdRevokeAdmin(os.Args[2])
	case "revoke-operator":
		if len(os.Args) < 3 {
			fatal("Usage: %s revoke-operator <address>", os.Args[0])
		}
		cmdRevokeOperator(os.Args[2])
	case "check-role":
		if len(os.Args) < 4 {
			fatal("Usage: %s check-role <admin|operator> <address>", os.Args[0])
		}
		cmdCheckRole(os.Args[2], os.Args[3])

	// Debug Commands
	case "debug-enclave":
		cmdDebugEnclave()

	case "help", "--help", "-h":
		printHelp()
	default:
		fatal("Unknown command: %s. Use '%s help' for usage.", command, os.Args[0])
	}
}

// ============================================================================
// TEE COMMANDS
// ============================================================================

func cmdList() {
	fmt.Println("=== Active TEEs in Registry ===")
	fmt.Printf("Registry: %s\n", TEE_REGISTRY_ADDRESS)
	fmt.Printf("RPC: %s\n", RPC_URL)
	fmt.Println()

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

	teeTypeName := getTEETypeName(info.TEEType)

	fmt.Printf("  Owner:          %s\n", info.Owner.Hex())
	fmt.Printf("  Payment Addr:   %s\n", info.PaymentAddress.Hex())
	fmt.Printf("  Endpoint:       %s\n", info.Endpoint)
	fmt.Printf("  PCR Hash:       0x%s\n", hex.EncodeToString(info.PCRHash[:]))
	fmt.Printf("  TEE Type:       %d (%s)\n", info.TEEType, teeTypeName)
	fmt.Printf("  Active:         %v\n", info.IsActive)
	fmt.Printf("  Registered:     %s UTC\n", info.RegisteredAt.UTC().Format("2006-01-02 15:04:05"))
	fmt.Printf("  Last Updated:   %s UTC\n", info.LastUpdatedAt.UTC().Format("2006-01-02 15:04:05"))

	// Public Key
	fmt.Println("\n  --- Public Key ---")
	pubKey, err := callGetPublicKey(teeId)
	if err != nil || len(pubKey) == 0 {
		fmt.Println("  Not available")
	} else {
		fmt.Printf("  Size:   %d bytes\n", len(pubKey))
		fmt.Printf("  Hex:    %s...\n", truncateHex(hex.EncodeToString(pubKey), 64))
	}

	// TLS Certificate
	fmt.Println("\n  --- TLS Certificate ---")
	tlsCert, err := callGetTLSCertificate(teeId)
	if err != nil || len(tlsCert) == 0 {
		fmt.Println("  Not available")
	} else {
		fmt.Printf("  Size:   %d bytes\n", len(tlsCert))
		certHash := crypto.Keccak256Hash(tlsCert)
		fmt.Printf("  Hash:   0x%s\n", hex.EncodeToString(certHash[:]))
	}
}

func cmdRegister() {
	// Get parameters from environment or flags
	enclaveHost := getEnvOrDefault("ENCLAVE_HOST", "")
	enclavePort := getEnvOrDefault("ENCLAVE_PORT", "443")
	paymentAddr := getEnvOrDefault("PAYMENT_ADDRESS", "")
	teeTypeStr := getEnvOrDefault("TEE_TYPE", "0")
	endpoint := getEnvOrDefault("TEE_ENDPOINT", "")

	if enclaveHost == "" {
		fatal("ENCLAVE_HOST environment variable required")
	}

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	if paymentAddr == "" {
		paymentAddr = account
	}
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://%s", enclaveHost)
	}

	teeType := uint8(parseUint(teeTypeStr))

	fmt.Println("=== Registering TEE ===")
	fmt.Printf("  Enclave Host: %s:%s\n", enclaveHost, enclavePort)
	fmt.Printf("  Payment Addr: %s\n", paymentAddr)
	fmt.Printf("  TEE Type:     %d\n", teeType)
	fmt.Printf("  Endpoint:     %s\n", endpoint)
	fmt.Printf("  Account:      %s\n", account)
	fmt.Println()

	// 1. Fetch attestation document
	log("Fetching attestation document...")
	nonce := generateNonce()
	attestationURL := fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", enclaveHost, nonce)
	attestationDoc, err := fetchAttestation(attestationURL)
	if err != nil {
		fatal("Failed to fetch attestation: %v", err)
	}
	attestationBytes, _ := base64.StdEncoding.DecodeString(attestationDoc)
	fmt.Printf("  ✅ Attestation: %d bytes\n", len(attestationBytes))

	// 2. Fetch signing public key
	log("Fetching signing public key...")
	signingPubKeyDER, err := fetchSigningPublicKey(enclaveHost)
	if err != nil {
		fatal("Failed to fetch signing key: %v", err)
	}
	fmt.Printf("  ✅ Signing Key: %d bytes\n", len(signingPubKeyDER))

	// 3. Fetch TLS certificate
	log("Fetching TLS certificate...")
	tlsCertDER, err := fetchTLSCertificate(enclaveHost, enclavePort)
	if err != nil {
		fatal("Failed to fetch TLS cert: %v", err)
	}
	fmt.Printf("  ✅ TLS Cert: %d bytes\n", len(tlsCertDER))

	// 4. Check if already registered
	expectedTeeId := crypto.Keccak256Hash(signingPubKeyDER)
	isActive, _ := callIsActive(expectedTeeId)
	if isActive {
		fmt.Printf("\n⚠️  TEE already registered with ID: 0x%s\n", hex.EncodeToString(expectedTeeId[:]))
		return
	}

	// 5. Register
	log("Sending registration transaction...")
	txHash, err := callRegisterTEE(account, attestationBytes, signingPubKeyDER, tlsCertDER, paymentAddr, endpoint, teeType)
	if err != nil {
		fatal("Failed to register TEE: %v", err)
	}

	fmt.Printf("  TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Printf("\n✅ TEE registered successfully!\n")
		fmt.Printf("   TEE ID: 0x%s\n", hex.EncodeToString(expectedTeeId[:]))
	} else {
		fmt.Println("\n❌ Registration failed")
	}
}

func cmdDeactivate(teeIdStr string) {
	teeId := parseBytes32(teeIdStr)
	log("Deactivating TEE: 0x%s", hex.EncodeToString(teeId[:]))

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callDeactivateTEE(account, teeId)
	if err != nil {
		fatal("Failed to deactivate TEE: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ TEE deactivated successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdActivate(teeIdStr string) {
	teeId := parseBytes32(teeIdStr)
	log("Activating TEE: 0x%s", hex.EncodeToString(teeId[:]))

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callActivateTEE(account, teeId)
	if err != nil {
		fatal("Failed to activate TEE: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ TEE activated successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

// ============================================================================
// PCR COMMANDS
// ============================================================================

func cmdPCRList() {
	fmt.Println("=== Active PCRs ===")

	pcrs, err := callGetActivePCRs()
	if err != nil {
		fatal("Failed to get active PCRs: %v", err)
	}

	fmt.Printf("Found %d active PCR(s)\n\n", len(pcrs))
	for i, pcrHash := range pcrs {
		fmt.Printf("  [%d] 0x%s\n", i+1, pcrHash)
	}
}

func cmdPCRApprove() {
	// Get PCR values from environment or measurements file
	measurementsFile := getEnvOrDefault("MEASUREMENTS_FILE", "measurements.txt")
	version := getEnvOrDefault("PCR_VERSION", "v1.0.0")
	previousPCR := getEnvOrDefault("PREVIOUS_PCR", "")
	gracePeriodStr := getEnvOrDefault("GRACE_PERIOD", "0")

	var pcr0, pcr1, pcr2 []byte
	var err error

	// Try to load from file first
	if _, err := os.Stat(measurementsFile); err == nil {
		pcr0, pcr1, pcr2, err = loadPCRMeasurements(measurementsFile)
		if err != nil {
			fatal("Failed to load measurements file: %v", err)
		}
		log("Loaded PCRs from %s", measurementsFile)
	} else {
		// Try environment variables
		pcr0Hex := os.Getenv("PCR0")
		pcr1Hex := os.Getenv("PCR1")
		pcr2Hex := os.Getenv("PCR2")

		if pcr0Hex == "" || pcr1Hex == "" || pcr2Hex == "" {
			fatal("Either MEASUREMENTS_FILE or PCR0/PCR1/PCR2 environment variables required")
		}

		pcr0, _ = hex.DecodeString(strings.TrimPrefix(pcr0Hex, "0x"))
		pcr1, _ = hex.DecodeString(strings.TrimPrefix(pcr1Hex, "0x"))
		pcr2, _ = hex.DecodeString(strings.TrimPrefix(pcr2Hex, "0x"))
	}

	gracePeriod := new(big.Int)
	gracePeriod.SetString(gracePeriodStr, 10)

	var previousPCRHash [32]byte
	if previousPCR != "" {
		previousPCRHash = parseBytes32(previousPCR)
	}

	// Compute hash locally for display
	pcrHash, _ := callComputePCRHash(pcr0, pcr1, pcr2)

	fmt.Println("=== Approving PCR ===")
	fmt.Printf("  PCR0:         %s...\n", truncateHex(hex.EncodeToString(pcr0), 32))
	fmt.Printf("  PCR1:         %s...\n", truncateHex(hex.EncodeToString(pcr1), 32))
	fmt.Printf("  PCR2:         %s...\n", truncateHex(hex.EncodeToString(pcr2), 32))
	fmt.Printf("  PCR Hash:     0x%s\n", hex.EncodeToString(pcrHash[:]))
	fmt.Printf("  Version:      %s\n", version)
	if previousPCR != "" {
		fmt.Printf("  Previous PCR: 0x%s\n", hex.EncodeToString(previousPCRHash[:]))
		fmt.Printf("  Grace Period: %s seconds\n", gracePeriod.String())
	}
	fmt.Println()

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callApprovePCR(account, pcr0, pcr1, pcr2, version, previousPCRHash, gracePeriod)
	if err != nil {
		fatal("Failed to approve PCR: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ PCR approved successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdPCRRevoke(pcrHashStr string) {
	pcrHash := parseBytes32(pcrHashStr)
	log("Revoking PCR: 0x%s", hex.EncodeToString(pcrHash[:]))

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callRevokePCR(account, pcrHash)
	if err != nil {
		fatal("Failed to revoke PCR: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ PCR revoked successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdPCRCheck(pcrHashStr string) {
	pcrHash := parseBytes32(pcrHashStr)

	approved, err := callIsPCRApproved(pcrHash)
	if err != nil {
		fatal("Failed to check PCR: %v", err)
	}

	if approved {
		fmt.Printf("✅ PCR 0x%s is APPROVED\n", hex.EncodeToString(pcrHash[:]))
	} else {
		fmt.Printf("❌ PCR 0x%s is NOT approved\n", hex.EncodeToString(pcrHash[:]))
	}
}

func cmdPCRCompute() {
	measurementsFile := getEnvOrDefault("MEASUREMENTS_FILE", "measurements.txt")

	var pcr0, pcr1, pcr2 []byte

	if _, err := os.Stat(measurementsFile); err == nil {
		var err error
		pcr0, pcr1, pcr2, err = loadPCRMeasurements(measurementsFile)
		if err != nil {
			fatal("Failed to load measurements file: %v", err)
		}
	} else {
		pcr0Hex := os.Getenv("PCR0")
		pcr1Hex := os.Getenv("PCR1")
		pcr2Hex := os.Getenv("PCR2")

		if pcr0Hex == "" || pcr1Hex == "" || pcr2Hex == "" {
			fatal("Either MEASUREMENTS_FILE or PCR0/PCR1/PCR2 environment variables required")
		}

		pcr0, _ = hex.DecodeString(strings.TrimPrefix(pcr0Hex, "0x"))
		pcr1, _ = hex.DecodeString(strings.TrimPrefix(pcr1Hex, "0x"))
		pcr2, _ = hex.DecodeString(strings.TrimPrefix(pcr2Hex, "0x"))
	}

	pcrHash, err := callComputePCRHash(pcr0, pcr1, pcr2)
	if err != nil {
		fatal("Failed to compute PCR hash: %v", err)
	}

	fmt.Printf("PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
}

// ============================================================================
// TEE TYPE COMMANDS
// ============================================================================

func cmdTypeList() {
	fmt.Println("=== TEE Types ===")

	// For now, check types 0-10
	for i := uint8(0); i <= 10; i++ {
		valid, _ := callIsValidTEEType(i)
		if valid {
			fmt.Printf("  [%d] %s (active)\n", i, getTEETypeName(i))
		}
	}
}

func cmdTypeAdd(typeIdStr, name string) {
	typeId := uint8(parseUint(typeIdStr))
	log("Adding TEE type %d: %s", typeId, name)

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callAddTEEType(account, typeId, name)
	if err != nil {
		fatal("Failed to add TEE type: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ TEE type added successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdTypeDeactivate(typeIdStr string) {
	typeId := uint8(parseUint(typeIdStr))
	log("Deactivating TEE type %d", typeId)

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callDeactivateTEEType(account, typeId)
	if err != nil {
		fatal("Failed to deactivate TEE type: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ TEE type deactivated successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

// ============================================================================
// CERTIFICATE COMMANDS
// ============================================================================

func cmdSetAWSCert(certFile string) {
	log("Setting AWS root certificate from %s", certFile)

	certData, err := os.ReadFile(certFile)
	if err != nil {
		fatal("Failed to read certificate file: %v", err)
	}

	// If PEM, extract DER
	if bytes.Contains(certData, []byte("-----BEGIN")) {
		block, _ := pem.Decode(certData)
		if block != nil {
			certData = block.Bytes
		}
	}

	fmt.Printf("  Certificate size: %d bytes\n", len(certData))

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callSetAWSRootCertificate(account, certData)
	if err != nil {
		fatal("Failed to set AWS certificate: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ AWS root certificate set successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

// ============================================================================
// ROLE COMMANDS
// ============================================================================

func cmdAddAdmin(addressStr string) {
	address := common.HexToAddress(addressStr)
	log("Adding admin: %s", address.Hex())

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callGrantRole(account, DEFAULT_ADMIN_ROLE, address.Hex())
	if err != nil {
		fatal("Failed to grant admin role: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ Admin role granted successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdAddOperator(addressStr string) {
	address := common.HexToAddress(addressStr)
	log("Adding TEE operator: %s", address.Hex())

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callGrantRole(account, TEE_OPERATOR_ROLE, address.Hex())
	if err != nil {
		fatal("Failed to grant operator role: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ Operator role granted successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdRevokeAdmin(addressStr string) {
	address := common.HexToAddress(addressStr)
	log("Revoking admin: %s", address.Hex())

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callRevokeRole(account, DEFAULT_ADMIN_ROLE, address.Hex())
	if err != nil {
		fatal("Failed to revoke admin role: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ Admin role revoked successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdRevokeOperator(addressStr string) {
	address := common.HexToAddress(addressStr)
	log("Revoking TEE operator: %s", address.Hex())

	account, err := getFirstAccount()
	if err != nil {
		fatal("Failed to get account: %v", err)
	}

	txHash, err := callRevokeRole(account, TEE_OPERATOR_ROLE, address.Hex())
	if err != nil {
		fatal("Failed to revoke operator role: %v", err)
	}

	fmt.Printf("TX Hash: %s\n", txHash)
	if waitForTx(txHash) {
		fmt.Println("✅ Operator role revoked successfully")
	} else {
		fmt.Println("❌ Transaction failed")
	}
}

func cmdCheckRole(roleName, addressStr string) {
	address := common.HexToAddress(addressStr)

	var role [32]byte
	var roleDisplayName string

	switch strings.ToLower(roleName) {
	case "admin":
		role = DEFAULT_ADMIN_ROLE
		roleDisplayName = "DEFAULT_ADMIN_ROLE"
	case "operator":
		role = TEE_OPERATOR_ROLE
		roleDisplayName = "TEE_OPERATOR"
	default:
		fatal("Unknown role: %s (use 'admin' or 'operator')", roleName)
	}

	hasRole, err := callHasRole(role, address.Hex())
	if err != nil {
		fatal("Failed to check role: %v", err)
	}

	if hasRole {
		fmt.Printf("✅ Address %s HAS %s role\n", address.Hex(), roleDisplayName)
	} else {
		fmt.Printf("❌ Address %s does NOT have %s role\n", address.Hex(), roleDisplayName)
	}
}

// ============================================================================
// HELP
// ============================================================================

func printHelp() {
	fmt.Printf(`TEE Registry Management CLI (Go)

Usage: %s <command> [arguments]

Environment Variables:
  TEE_REGISTRY_ADDRESS  Contract address (default: 0x3d64...)
  TEE_RPC_URL           RPC endpoint (default: http://13.59.43.94:8545)

TEE Commands:
  list                          List all active TEEs
  show <tee_id>                 Show detailed info for a TEE
  register                      Register a new TEE (see env vars below)
  deactivate <tee_id>           Deactivate a TEE
  activate <tee_id>             Reactivate a TEE

PCR Commands:
  pcr-list                      List all approved PCRs
  pcr-approve                   Approve PCR measurements (see env vars below)
  pcr-revoke <pcr_hash>         Revoke a PCR
  pcr-check <pcr_hash>          Check if PCR is approved
  pcr-compute                   Compute PCR hash from measurements

TEE Type Commands:
  type-list                     List all TEE types
  type-add <type_id> <name>     Add a new TEE type
  type-deactivate <type_id>     Deactivate a TEE type

Certificate Commands:
  set-aws-cert <cert_file>      Set AWS root certificate

Role Commands:
  add-admin <address>           Grant DEFAULT_ADMIN_ROLE
  add-operator <address>        Grant TEE_OPERATOR role
  revoke-admin <address>        Revoke DEFAULT_ADMIN_ROLE
  revoke-operator <address>     Revoke TEE_OPERATOR role
  check-role <admin|operator> <address>  Check if address has role

Register TEE Environment Variables:
  ENCLAVE_HOST          Enclave hostname (required)
  ENCLAVE_PORT          Enclave port (default: 443)
  PAYMENT_ADDRESS       Payment address (default: sender)
  TEE_TYPE              TEE type ID (default: 0)
  TEE_ENDPOINT          TEE endpoint URL (default: https://ENCLAVE_HOST)

PCR Approve Environment Variables:
  MEASUREMENTS_FILE     Path to measurements.txt (default: measurements.txt)
  PCR0, PCR1, PCR2      PCR values in hex (alternative to file)
  PCR_VERSION           Version string (default: v1.0.0)
  PREVIOUS_PCR          Previous PCR hash for rotation (optional)
  GRACE_PERIOD          Grace period in seconds (default: 0)

Examples:
  # List all TEEs
  %s list

  # Register a TEE
  ENCLAVE_HOST=13.59.207.188 %s register

  # Approve PCR from measurements file
  MEASUREMENTS_FILE=measurements.txt PCR_VERSION=v1.0.0 %s pcr-approve

  # Approve PCR from hex values
  PCR0=abc... PCR1=def... PCR2=123... %s pcr-approve

  # Add TEE type
  %s type-add 0 LLMProxy
  %s type-add 1 Validator
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
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
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)

	result, err := ethCall(append(SEL_GET_TEE, encoded...))
	if err != nil {
		return nil, err
	}
	if len(result) < 320 {
		return nil, fmt.Errorf("TEE not found or invalid response")
	}

	info := &TEEInfo{}
	info.Owner = common.BytesToAddress(result[12:32])
	info.PaymentAddress = common.BytesToAddress(result[44:64])

	endpointOffset := new(big.Int).SetBytes(result[64:96]).Uint64()
	if endpointOffset < uint64(len(result)) {
		endpointLen := new(big.Int).SetBytes(result[endpointOffset : endpointOffset+32]).Uint64()
		if endpointOffset+32+endpointLen <= uint64(len(result)) {
			info.Endpoint = string(result[endpointOffset+32 : endpointOffset+32+endpointLen])
		}
	}

	copy(info.PCRHash[:], result[160:192])
	info.TEEType = uint8(result[223])
	info.IsActive = result[255] == 1

	registeredAt := new(big.Int).SetBytes(result[256:288]).Int64()
	info.RegisteredAt = time.Unix(registeredAt, 0)

	lastUpdated := new(big.Int).SetBytes(result[288:320]).Int64()
	info.LastUpdatedAt = time.Unix(lastUpdated, 0)

	return info, nil
}

func callGetPublicKey(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_GET_PUBLIC_KEY, encoded...))
	if err != nil {
		return nil, err
	}
	return decodeDynamicBytes(result)
}

func callGetTLSCertificate(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_GET_TLS_CERT, encoded...))
	if err != nil {
		return nil, err
	}
	return decodeDynamicBytes(result)
}

func callIsActive(teeId [32]byte) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_IS_ACTIVE, encoded...))
	if err != nil {
		return false, err
	}
	return len(result) >= 32 && result[31] == 1, nil
}

func callRegisterTEE(from string, attestation, signingKey, tlsCert []byte, paymentAddr, endpoint string, teeType uint8) (string, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{
		{Type: bytesType}, {Type: bytesType}, {Type: bytesType},
		{Type: addrType}, {Type: stringType}, {Type: uint8Type},
	}
	encoded, err := args.Pack(attestation, signingKey, tlsCert, common.HexToAddress(paymentAddr), endpoint, teeType)
	if err != nil {
		return "", fmt.Errorf("abi pack failed: %v", err)
	}
	return sendTx(from, append(SEL_REGISTER_TEE, encoded...))
}

func callDeactivateTEE(from string, teeId [32]byte) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	return sendTx(from, append(SEL_DEACTIVATE_TEE, encoded...))
}

func callActivateTEE(from string, teeId [32]byte) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	return sendTx(from, append(SEL_ACTIVATE_TEE, encoded...))
}

// PCR Calls

func callGetActivePCRs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_PCRS)
	if err != nil {
		return nil, err
	}
	return decodeBytes32Array(result)
}

func callComputePCRHash(pcr0, pcr1, pcr2 []byte) ([32]byte, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})
	args := abi.Arguments{{Type: tupleType}}
	pcrs := struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}
	encoded, _ := args.Pack(pcrs)
	result, err := ethCall(append(SEL_COMPUTE_PCR_HASH, encoded...))
	if err != nil || len(result) < 32 {
		return [32]byte{}, err
	}
	var hash [32]byte
	copy(hash[:], result[:32])
	return hash, nil
}

func callApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string, previousPCR [32]byte, gracePeriod *big.Int) (string, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})
	stringType, _ := abi.NewType("string", "", nil)
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{{Type: tupleType}, {Type: stringType}, {Type: bytes32Type}, {Type: uint256Type}}
	pcrs := struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}
	encoded, _ := args.Pack(pcrs, version, previousPCR, gracePeriod)
	return sendTx(from, append(SEL_APPROVE_PCR, encoded...))
}

func callRevokePCR(from string, pcrHash [32]byte) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(pcrHash)
	return sendTx(from, append(SEL_REVOKE_PCR, encoded...))
}

func callIsPCRApproved(pcrHash [32]byte) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(pcrHash)
	result, err := ethCall(append(SEL_IS_PCR_APPROVED, encoded...))
	if err != nil {
		return false, err
	}
	return len(result) >= 32 && result[31] == 1, nil
}

// TEE Type Calls

func callIsValidTEEType(typeId uint8) (bool, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	args := abi.Arguments{{Type: uint8Type}}
	encoded, _ := args.Pack(typeId)
	result, err := ethCall(append(SEL_IS_VALID_TYPE, encoded...))
	if err != nil {
		return false, err
	}
	return len(result) >= 32 && result[31] == 1, nil
}

func callAddTEEType(from string, typeId uint8, name string) (string, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	args := abi.Arguments{{Type: uint8Type}, {Type: stringType}}
	encoded, _ := args.Pack(typeId, name)
	return sendTx(from, append(SEL_ADD_TEE_TYPE, encoded...))
}

func callDeactivateTEEType(from string, typeId uint8) (string, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	args := abi.Arguments{{Type: uint8Type}}
	encoded, _ := args.Pack(typeId)
	return sendTx(from, append(SEL_DEACTIVATE_TEE_TYPE, encoded...))
}

// Certificate Calls

func callSetAWSRootCertificate(from string, certificate []byte) (string, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	args := abi.Arguments{{Type: bytesType}}
	encoded, _ := args.Pack(certificate)
	return sendTx(from, append(SEL_SET_AWS_ROOT_CERT, encoded...))
}

// Role Calls

func callHasRole(role [32]byte, account string) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: addrType}}
	encoded, _ := args.Pack(role, common.HexToAddress(account))
	result, err := ethCall(append(SEL_HAS_ROLE, encoded...))
	if err != nil {
		return false, err
	}
	return len(result) >= 32 && result[31] == 1, nil
}

func callGrantRole(from string, role [32]byte, account string) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: addrType}}
	encoded, _ := args.Pack(role, common.HexToAddress(account))
	return sendTx(from, append(SEL_GRANT_ROLE, encoded...))
}

func callRevokeRole(from string, role [32]byte, account string) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: addrType}}
	encoded, _ := args.Pack(role, common.HexToAddress(account))
	return sendTx(from, append(SEL_REVOKE_ROLE, encoded...))
}

// ============================================================================
// RPC HELPERS
// ============================================================================

func getFirstAccount() (string, error) {
	resp, err := rpcCall("eth_accounts", []interface{}{})
	if err != nil {
		return "", err
	}
	var result struct {
		Result []string
		Error  *struct{ Message string }
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("%s", result.Error.Message)
	}
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no unlocked accounts available")
	}
	return result.Result[0], nil
}

func ethCall(data []byte) ([]byte, error) {
	params := []interface{}{
		map[string]string{
			"to":   TEE_REGISTRY_ADDRESS,
			"data": "0x" + hex.EncodeToString(data),
		},
		"latest",
	}
	resp, err := rpcCall("eth_call", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result string
		Error  *struct{ Message string }
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, fmt.Errorf("%s", result.Error.Message)
	}
	if len(result.Result) > 2 {
		return hex.DecodeString(result.Result[2:])
	}
	return nil, nil
}

func sendTx(from string, data []byte) (string, error) {
	params := []interface{}{
		map[string]string{
			"from": from,
			"to":   TEE_REGISTRY_ADDRESS,
			"gas":  "0x500000",
			"data": "0x" + hex.EncodeToString(data),
		},
	}
	resp, err := rpcCall("eth_sendTransaction", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Result string
		Error  *struct{ Message string }
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("%s", result.Error.Message)
	}
	if result.Result == "" {
		return "", fmt.Errorf("empty tx hash in response")
	}
	return result.Result, nil
}

func waitForTx(txHash string) bool {
	log("Waiting for transaction confirmation...")
	for i := 0; i < 30; i++ {
		resp, err := rpcCall("eth_getTransactionReceipt", []string{txHash})
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		var result struct {
			Result *struct {
				Status  string
				GasUsed string
			}
		}
		if err := json.Unmarshal(resp, &result); err != nil {
			time.Sleep(time.Second)
			continue
		}

		if result.Result != nil {
			if result.Result.Status != "0x1" {
				log("Transaction reverted. GasUsed: %s", result.Result.GasUsed)
				return false
			}
			log("Transaction confirmed. GasUsed: %s", result.Result.GasUsed)
			return true
		}
		time.Sleep(time.Second)
	}
	log("Timeout waiting for transaction receipt")
	return false
}

func rpcCall(method string, params interface{}) ([]byte, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(RPC_URL, "application/json", bytes.NewReader(jsonBody))
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
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(body)), nil
}

func fetchSigningPublicKey(host string) ([]byte, error) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	endpoints := []string{"/signing-key"}
	
	var lastErr error
	for _, endpoint := range endpoints {
		url := fmt.Sprintf("https://%s%s", host, endpoint)
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 404 {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %v", err)
			continue
		}

		// Try to parse as JSON
		var attestResp AttestationResponse
		if err := json.Unmarshal(body, &attestResp); err != nil {
			lastErr = fmt.Errorf("failed to parse JSON from %s: %v", endpoint, err)
			continue
		}

		pubKeyStr := attestResp.PublicKey
		if pubKeyStr == "" {
			lastErr = fmt.Errorf("empty public_key in response from %s", endpoint)
			continue
		}

		// Try PEM decode
		block, _ := pem.Decode([]byte(pubKeyStr))
		if block != nil {
			log("Fetched signing key from %s", endpoint)
			return block.Bytes, nil
		}

		// Try base64 decode (raw DER in base64)
		if decoded, err := base64.StdEncoding.DecodeString(pubKeyStr); err == nil && len(decoded) > 0 {
			log("Fetched signing key from %s (base64)", endpoint)
			return decoded, nil
		}

		// Try hex decode
		cleaned := strings.TrimPrefix(pubKeyStr, "0x")
		if decoded, err := hex.DecodeString(cleaned); err == nil && len(decoded) > 0 {
			log("Fetched signing key from %s (hex)", endpoint)
			return decoded, nil
		}

		lastErr = fmt.Errorf("unknown public key format from %s", endpoint)
	}

	return nil, fmt.Errorf("failed to fetch signing key from any endpoint: %v", lastErr)
}

func fetchTLSCertificate(host, port string) ([]byte, error) {
	conn, err := tls.Dial("tcp", host+":"+port, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates")
	}
	return certs[0].Raw, nil
}

func loadPCRMeasurements(filepath string) ([]byte, []byte, []byte, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, nil, nil, err
	}

	var m MeasurementsFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, nil, err
	}

	pcr0, _ := hex.DecodeString(m.Measurements.PCR0)
	pcr1, _ := hex.DecodeString(m.Measurements.PCR1)
	pcr2, _ := hex.DecodeString(m.Measurements.PCR2)
	return pcr0, pcr1, pcr2, nil
}

// ============================================================================
// UTILITY HELPERS
// ============================================================================

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func parseBytes32(s string) [32]byte {
	s = strings.TrimPrefix(s, "0x")
	for len(s) < 64 {
		s = "0" + s
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		fatal("Invalid bytes32: %s", s)
	}
	var result [32]byte
	copy(result[:], decoded)
	return result
}

func parseUint(s string) uint64 {
	var val uint64
	fmt.Sscanf(s, "%d", &val)
	return val
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
	if uint64(len(result)) < 64+length {
		return nil, fmt.Errorf("truncated response")
	}
	return result[64 : 64+length], nil
}

func truncateHex(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func getTEETypeName(typeId uint8) string {
	switch typeId {
	case 0:
		return "LLMProxy"
	case 1:
		return "Validator"
	default:
		return "Unknown"
	}
}

func log(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}