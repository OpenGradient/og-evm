package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"time"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	RPC_URL     = "http://localhost:8545"
	TEE_ADDRESS = "0x0000000000000000000000000000000000000900"

	// Enclave configuration
	ENCLAVE_HOST = "13.59.207.188"
	ENCLAVE_PORT = "443"

	// Path to measurements file
	MEASUREMENTS_PATH = "measurements.txt"
)

// Method selectors
var (
	// Admin
	SEL_ADD_ADMIN    = crypto.Keccak256([]byte("addAdmin(address)"))[:4]
	SEL_REMOVE_ADMIN = crypto.Keccak256([]byte("removeAdmin(address)"))[:4]
	SEL_IS_ADMIN     = crypto.Keccak256([]byte("isAdmin(address)"))[:4]
	SEL_GET_ADMINS   = crypto.Keccak256([]byte("getAdmins()"))[:4]

	// TEE Type
	SEL_ADD_TEE_TYPE  = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	SEL_IS_VALID_TYPE = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	SEL_GET_TEE_TYPES = crypto.Keccak256([]byte("getTEETypes()"))[:4]

	// PCR
	SEL_APPROVE_PCR     = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,bytes32,uint256)"))[:4]
	SEL_IS_PCR_APPROVED = crypto.Keccak256([]byte("isPCRApproved((bytes,bytes,bytes))"))[:4]
	SEL_GET_ACTIVE_PCRS = crypto.Keccak256([]byte("getActivePCRs()"))[:4]

	// Registration
	SEL_REGISTER_TEE = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]

	// Management
	SEL_DEACTIVATE_TEE = crypto.Keccak256([]byte("deactivateTEE(bytes32)"))[:4]
	SEL_ACTIVATE_TEE   = crypto.Keccak256([]byte("activateTEE(bytes32)"))[:4]

	// Queries
	SEL_GET_TEE           = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
	SEL_GET_ACTIVE_TEES   = crypto.Keccak256([]byte("getActiveTEEs()"))[:4]
	SEL_GET_TEES_BY_TYPE  = crypto.Keccak256([]byte("getTEEsByType(uint8)"))[:4]
	SEL_GET_TEES_BY_OWNER = crypto.Keccak256([]byte("getTEEsByOwner(address)"))[:4]
	SEL_GET_PUBLIC_KEY    = crypto.Keccak256([]byte("getPublicKey(bytes32)"))[:4]
	SEL_GET_TLS_CERT      = crypto.Keccak256([]byte("getTLSCertificate(bytes32)"))[:4]
	SEL_IS_ACTIVE         = crypto.Keccak256([]byte("isActive(bytes32)"))[:4]

	// Verification
	SEL_VERIFY_SIGNATURE  = crypto.Keccak256([]byte("verifySignature((bytes32,bytes32,bytes32,uint256,bytes))"))[:4]
	SEL_VERIFY_SETTLEMENT = crypto.Keccak256([]byte("verifySettlement(bytes32,bytes32,bytes32,uint256,bytes)"))[:4]

	// Utilities
	SEL_COMPUTE_TEE_ID   = crypto.Keccak256([]byte("computeTEEId(bytes)"))[:4]
	SEL_COMPUTE_MSG_HASH = crypto.Keccak256([]byte("computeMessageHash(bytes32,bytes32,uint256)"))[:4]
)

// Structs
type PCRMeasurements struct {
	PCR0 string `json:"PCR0"`
	PCR1 string `json:"PCR1"`
	PCR2 string `json:"PCR2"`
}

type MeasurementsFile struct {
	Measurements PCRMeasurements `json:"Measurements"`
}

type AttestationResponse struct {
	PublicKey string `json:"public_key"`
}

// Test results tracker
type TestResults struct {
	Passed int
	Failed int
	Tests  []TestResult
}

type TestResult struct {
	Name    string
	Passed  bool
	Message string
}

func (tr *TestResults) Add(name string, passed bool, msg string) {
	tr.Tests = append(tr.Tests, TestResult{name, passed, msg})
	if passed {
		tr.Passed++
		fmt.Printf("  ✅ %s\n", name)
	} else {
		tr.Failed++
		fmt.Printf("  ❌ %s: %s\n", name, msg)
	}
}

func main() {
	fmt.Println("==========================================")
	fmt.Println("  TEE Registry Full Integration Test")
	fmt.Println("==========================================")
	fmt.Println()

	results := &TestResults{}

	// Get accounts
	account, err := getFirstAccount()
	if err != nil {
		fmt.Printf("❌ Failed to get account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📍 Primary account: %s\n\n", account)

	// Load PCR measurements
	pcr0, pcr1, pcr2, err := loadPCRMeasurements()
	if err != nil {
		fmt.Printf("⚠️  Failed to load measurements.txt: %v\n", err)
		fmt.Println("   Using random PCRs for testing")
		pcr0, pcr1, pcr2 = make([]byte, 48), make([]byte, 48), make([]byte, 48)
		rand.Read(pcr0)
		rand.Read(pcr1)
		rand.Read(pcr2)
	}

	// ==========================================
	// SECTION 1: Admin Management
	// ==========================================
	fmt.Println("------------------------------------------")
	fmt.Println("SECTION 1: Admin Management")
	fmt.Println("------------------------------------------")

	// Test 1.1: Add first admin (bootstrap)
	txHash, err := callAddAdmin(account, account)
	if err == nil {
		waitForTx(txHash)
	}
	isAdmin, _ := callIsAdmin(account)
	results.Add("Add first admin (bootstrap)", isAdmin, "")

	// Test 1.2: Check isAdmin
	isAdmin, err = callIsAdmin(account)
	results.Add("isAdmin returns true for admin", isAdmin && err == nil, fmt.Sprintf("%v", err))

	// Test 1.3: isAdmin for non-admin
	isAdmin, _ = callIsAdmin("0x0000000000000000000000000000000000000001")
	results.Add("isAdmin returns false for non-admin", !isAdmin, "")

	// Test 1.4: Get admins list
	admins, err := callGetAdmins()
	results.Add("getAdmins returns list", err == nil && len(admins) > 0, fmt.Sprintf("count=%d", len(admins)))

	// ==========================================
	// SECTION 2: TEE Type Management
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 2: TEE Type Management")
	fmt.Println("------------------------------------------")

	// Test 2.1: Add TEE type 0 (LLMProxy)
	txHash, err = callAddTEEType(account, 0, "LLMProxy")
	if err == nil {
		waitForTx(txHash)
	}
	isValid, _ := callIsValidTEEType(0)
	results.Add("Add TEE type 0 (LLMProxy)", isValid, "")

	// Test 2.2: Add TEE type 1 (Validator)
	txHash, err = callAddTEEType(account, 1, "Validator")
	if err == nil {
		waitForTx(txHash)
	}
	isValid, _ = callIsValidTEEType(1)
	results.Add("Add TEE type 1 (Validator)", isValid, "")

	// Test 2.3: Check invalid type
	isValid, _ = callIsValidTEEType(99)
	results.Add("isValidTEEType returns false for unknown type", !isValid, "")

	// ==========================================
	// SECTION 3: PCR Management
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 3: PCR Management")
	fmt.Println("------------------------------------------")

	// Test 3.1: Approve PCR
	txHash, err = callApprovePCR(account, pcr0, pcr1, pcr2, "v1.0.0")
	if err == nil {
		waitForTx(txHash)
	}
	approved, _ := callIsPCRApproved(pcr0, pcr1, pcr2)
	results.Add("Approve PCR v1.0.0", approved, "")

	// Test 3.2: Check unapproved PCR
	fakePCR := make([]byte, 48)
	rand.Read(fakePCR)
	approved, _ = callIsPCRApproved(fakePCR, pcr1, pcr2)
	results.Add("isPCRApproved returns false for unknown PCR", !approved, "")

	// Test 3.3: Get active PCRs
	activePCRs, err := callGetActivePCRs()
	results.Add("getActivePCRs returns list", err == nil && len(activePCRs) > 0, fmt.Sprintf("count=%d", len(activePCRs)))

	// ==========================================
	// SECTION 4: TEE Registration (Real Attestation)
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 4: TEE Registration")
	fmt.Println("------------------------------------------")

	var registeredTEEId [32]byte
	var signingPubKeyDER []byte
	registrationSuccess := false

	// Generate nonce
	nonce := generateNonce()
	fmt.Printf("  🎲 Nonce: %s\n", nonce)

	// Fetch attestation
	attestationURL := fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", ENCLAVE_HOST, nonce)
	attestationDoc, err := getAttestation(attestationURL)
	if err != nil {
		results.Add("Fetch attestation from enclave", false, err.Error())
	} else {
		results.Add("Fetch attestation from enclave", true, fmt.Sprintf("%d chars", len(attestationDoc)))

		attestationBytes, _ := base64.StdEncoding.DecodeString(attestationDoc)

		// Fetch signing key
		signingPubKeyDER, err = fetchSigningPublicKey(ENCLAVE_HOST)
		if err != nil {
			results.Add("Fetch signing public key", false, err.Error())
		} else {
			results.Add("Fetch signing public key", true, fmt.Sprintf("%d bytes", len(signingPubKeyDER)))

			// Fetch TLS certificate
			tlsCertDER, err := fetchTLSCertificate(ENCLAVE_HOST, ENCLAVE_PORT)
			if err != nil {
				results.Add("Fetch TLS certificate", false, err.Error())
			} else {
				results.Add("Fetch TLS certificate", true, fmt.Sprintf("%d bytes", len(tlsCertDER)))

				// Register TEE
				endpoint := fmt.Sprintf("https://%s", ENCLAVE_HOST)
				txHash, err = callRegisterTEE(account, attestationBytes, signingPubKeyDER, tlsCertDER, account, endpoint, 0)
				if err != nil {
					results.Add("Register TEE with attestation", false, err.Error())
				} else {
					success := waitForTx(txHash)
					results.Add("Register TEE with attestation", success, "")
					if success {
						registrationSuccess = true
						registeredTEEId = crypto.Keccak256Hash(signingPubKeyDER)
					}
				}
			}
		}
	}

	// ==========================================
	// SECTION 5: TEE Queries
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 5: TEE Queries")
	fmt.Println("------------------------------------------")

	if registrationSuccess {
		// Test 5.1: isActive
		isActive, err := callIsActive(registeredTEEId)
		results.Add("isActive returns true for registered TEE", isActive && err == nil, "")

		// Test 5.2: getPublicKey
		storedKey, err := callGetPublicKey(registeredTEEId)
		keyMatches := err == nil && bytes.Equal(storedKey, signingPubKeyDER)
		results.Add("getPublicKey returns correct key", keyMatches, "")

		// Test 5.3: getTLSCertificate
		storedCert, err := callGetTLSCertificate(registeredTEEId)
		results.Add("getTLSCertificate returns cert", err == nil && len(storedCert) > 0, fmt.Sprintf("%d bytes", len(storedCert)))

		// Test 5.4: getActiveTEEs
		activeTEEs, err := callGetActiveTEEs()
		results.Add("getActiveTEEs includes registered TEE", err == nil && len(activeTEEs) > 0, fmt.Sprintf("count=%d", len(activeTEEs)))

		// Test 5.5: getTEEsByType
		teesByType, err := callGetTEEsByType(0)
		results.Add("getTEEsByType(0) includes registered TEE", err == nil && len(teesByType) > 0, fmt.Sprintf("count=%d", len(teesByType)))

		// Test 5.6: getTEEsByOwner
		teesByOwner, err := callGetTEEsByOwner(account)
		results.Add("getTEEsByOwner includes registered TEE", err == nil && len(teesByOwner) > 0, fmt.Sprintf("count=%d", len(teesByOwner)))
	} else {
		fmt.Println("  ⚠️  Skipping query tests (registration failed)")
	}

	// ==========================================
	// SECTION 6: TEE Lifecycle (Activate/Deactivate)
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 6: TEE Lifecycle")
	fmt.Println("------------------------------------------")

	if registrationSuccess {
		// Test 6.1: Deactivate TEE
		txHash, err = callDeactivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
		}
		isActive, _ := callIsActive(registeredTEEId)
		results.Add("Deactivate TEE", !isActive, "")

		// Test 6.2: Verify not in active list
		activeTEEs, _ := callGetActiveTEEs()
		found := false
		for _, id := range activeTEEs {
			if id == hex.EncodeToString(registeredTEEId[:]) {
				found = true
			}
		}
		results.Add("Deactivated TEE not in getActiveTEEs", !found, "")

		// Test 6.3: Reactivate TEE
		txHash, err = callActivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
		}
		isActive, _ = callIsActive(registeredTEEId)
		results.Add("Reactivate TEE", isActive, "")
	} else {
		fmt.Println("  ⚠️  Skipping lifecycle tests (registration failed)")
	}

	// ==========================================
	// SECTION 7: Signature Verification
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 7: Signature Verification")
	fmt.Println("------------------------------------------")

	// Generate test key pair
	privateKey, testPubKeyDER := generateKeyPair()

	// Create test data
	inputHash := sha256.Sum256([]byte(`{"prompt": "Hello"}`))
	outputHash := sha256.Sum256([]byte(`{"response": "Hi"}`))
	timestamp := big.NewInt(time.Now().Unix())

	// Compute message hash using Keccak256
	messageHash := computeMessageHash(inputHash, outputHash, timestamp)

	// Sign message
	signature := signMessage(privateKey, messageHash[:])

	// Verify locally
	err = verifySignatureLocal(testPubKeyDER, messageHash[:], signature)
	results.Add("Local RSA-PSS signature verification", err == nil, fmt.Sprintf("%v", err))

	// Test with wrong signature
	badSig := make([]byte, len(signature))
	copy(badSig, signature)
	badSig[0] ^= 0xFF
	err = verifySignatureLocal(testPubKeyDER, messageHash[:], badSig)
	results.Add("Reject invalid signature", err != nil, "")

	// ==========================================
	// SECTION 8: Utility Functions
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 8: Utility Functions")
	fmt.Println("------------------------------------------")

	// Test 8.1: computeTEEId
	computedId, err := callComputeTEEId(testPubKeyDER)
	expectedId := crypto.Keccak256Hash(testPubKeyDER)
	results.Add("computeTEEId matches keccak256", err == nil && computedId == expectedId, "")

	// Test 8.2: computeMessageHash
	computedHash, err := callComputeMessageHash(inputHash, outputHash, timestamp)
	results.Add("computeMessageHash returns hash", err == nil && computedHash != [32]byte{}, "")

	// ==========================================
	// Summary
	// ==========================================
	fmt.Println("\n==========================================")
	fmt.Println("  Test Summary")
	fmt.Println("==========================================")
	fmt.Printf("\n  Total:  %d\n", results.Passed+results.Failed)
	fmt.Printf("  Passed: %d ✅\n", results.Passed)
	fmt.Printf("  Failed: %d ❌\n", results.Failed)
	fmt.Println()

	if results.Failed > 0 {
		fmt.Println("Failed tests:")
		for _, t := range results.Tests {
			if !t.Passed {
				fmt.Printf("  - %s: %s\n", t.Name, t.Message)
			}
		}
		os.Exit(1)
	}
}

// ============================================================================
// NETWORK HELPERS
// ============================================================================

func generateNonce() string {
	nonce := make([]byte, 20)
	rand.Read(nonce)
	return hex.EncodeToString(nonce)
}

func getAttestation(url string) (string, error) {
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

	resp, err := client.Get(fmt.Sprintf("https://%s/attestation", host))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var attestResp AttestationResponse
	if err := json.NewDecoder(resp.Body).Decode(&attestResp); err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(attestResp.PublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM")
	}
	return block.Bytes, nil
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

func loadPCRMeasurements() ([]byte, []byte, []byte, error) {
	data, err := os.ReadFile(MEASUREMENTS_PATH)
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
// CRYPTO HELPERS
// ============================================================================

func generateKeyPair() (*rsa.PrivateKey, []byte) {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	publicKeyDER, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	return privateKey, publicKeyDER
}

func signMessage(privateKey *rsa.PrivateKey, messageHash []byte) []byte {
	// Hash the message hash with SHA256 for RSA-PSS
	hash := sha256.Sum256(messageHash)
	signature, _ := rsa.SignPSS(rand.Reader, privateKey, gcrypto.SHA256, hash[:], &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	})
	return signature
}

func verifySignatureLocal(publicKeyDER, messageHash, signature []byte) error {
	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return err
	}
	rsaPub := pub.(*rsa.PublicKey)
	hash := sha256.Sum256(messageHash)
	return rsa.VerifyPSS(rsaPub, gcrypto.SHA256, hash[:], signature, &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	})
}

// computeMessageHash uses Keccak256 to match the precompile

func computeMessageHash(inputHash, outputHash [32]byte, timestamp *big.Int) [32]byte {
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])
	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)

	hash := crypto.Keccak256Hash(data)
	return hash
}

// ============================================================================
// CONTRACT CALLS - ADMIN
// ============================================================================

func callAddAdmin(from, newAdmin string) (string, error) {
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: addrType}}
	encoded, _ := args.Pack(common.HexToAddress(newAdmin))
	return sendTx(from, append(SEL_ADD_ADMIN, encoded...))
}

func callIsAdmin(account string) (bool, error) {
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: addrType}}
	encoded, _ := args.Pack(common.HexToAddress(account))
	result, err := ethCall(append(SEL_IS_ADMIN, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
}

func callGetAdmins() ([]string, error) {
	result, err := ethCall(SEL_GET_ADMINS)
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	admins := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			admins[i] = common.BytesToAddress(result[start+12 : start+32]).Hex()
		}
	}
	return admins, nil
}

// ============================================================================
// CONTRACT CALLS - TEE TYPE
// ============================================================================

func callAddTEEType(from string, typeId uint8, name string) (string, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	args := abi.Arguments{{Type: uint8Type}, {Type: stringType}}
	encoded, _ := args.Pack(typeId, name)
	return sendTx(from, append(SEL_ADD_TEE_TYPE, encoded...))
}

func callIsValidTEEType(typeId uint8) (bool, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	args := abi.Arguments{{Type: uint8Type}}
	encoded, _ := args.Pack(typeId)
	result, err := ethCall(append(SEL_IS_VALID_TYPE, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
}

// ============================================================================
// CONTRACT CALLS - PCR
// ============================================================================

func callApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string) (string, error) {
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
	encoded, _ := args.Pack(pcrs, version, [32]byte{}, big.NewInt(0))
	return sendTx(from, append(SEL_APPROVE_PCR, encoded...))
}

func callIsPCRApproved(pcr0, pcr1, pcr2 []byte) (bool, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})
	args := abi.Arguments{{Type: tupleType}}
	pcrs := struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}
	encoded, _ := args.Pack(pcrs)
	result, err := ethCall(append(SEL_IS_PCR_APPROVED, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
}

func callGetActivePCRs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_PCRS)
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	pcrs := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			pcrs[i] = "0x" + hex.EncodeToString(result[start:start+32])
		}
	}
	return pcrs, nil
}

// ============================================================================
// CONTRACT CALLS - REGISTRATION
// ============================================================================

func callRegisterTEE(from string, attestation, signingKey, tlsCert []byte, paymentAddr, endpoint string, teeType uint8) (string, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{
		{Type: bytesType}, {Type: bytesType}, {Type: bytesType},
		{Type: addrType}, {Type: stringType}, {Type: uint8Type},
	}
	encoded, _ := args.Pack(attestation, signingKey, tlsCert, common.HexToAddress(paymentAddr), endpoint, teeType)
	return sendTx(from, append(SEL_REGISTER_TEE, encoded...))
}

// ============================================================================
// CONTRACT CALLS - MANAGEMENT
// ============================================================================

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

// ============================================================================
// CONTRACT CALLS - QUERIES
// ============================================================================

func callIsActive(teeId [32]byte) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_IS_ACTIVE, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
}

func callGetPublicKey(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_GET_PUBLIC_KEY, encoded...))
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if uint64(len(result)) < 64+length {
		return nil, fmt.Errorf("truncated")
	}
	return result[64 : 64+length], nil
}

func callGetTLSCertificate(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)
	result, err := ethCall(append(SEL_GET_TLS_CERT, encoded...))
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if uint64(len(result)) < 64+length {
		return nil, fmt.Errorf("truncated")
	}
	return result[64 : 64+length], nil
}

func callGetActiveTEEs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_TEES)
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	tees := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			tees[i] = hex.EncodeToString(result[start : start+32])
		}
	}
	return tees, nil
}

func callGetTEEsByType(teeType uint8) ([]string, error) {
	uint8Type, _ := abi.NewType("uint8", "", nil)
	args := abi.Arguments{{Type: uint8Type}}
	encoded, _ := args.Pack(teeType)
	result, err := ethCall(append(SEL_GET_TEES_BY_TYPE, encoded...))
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	tees := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			tees[i] = hex.EncodeToString(result[start : start+32])
		}
	}
	return tees, nil
}

func callGetTEEsByOwner(owner string) ([]string, error) {
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: addrType}}
	encoded, _ := args.Pack(common.HexToAddress(owner))
	result, err := ethCall(append(SEL_GET_TEES_BY_OWNER, encoded...))
	if err != nil || len(result) < 64 {
		return nil, err
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	tees := make([]string, length)
	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		if start+32 <= uint64(len(result)) {
			tees[i] = hex.EncodeToString(result[start : start+32])
		}
	}
	return tees, nil
}

// ============================================================================
// CONTRACT CALLS - UTILITIES
// ============================================================================

func callComputeTEEId(publicKey []byte) ([32]byte, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	args := abi.Arguments{{Type: bytesType}}
	encoded, _ := args.Pack(publicKey)
	result, err := ethCall(append(SEL_COMPUTE_TEE_ID, encoded...))
	if err != nil || len(result) < 32 {
		return [32]byte{}, err
	}
	var id [32]byte
	copy(id[:], result[:32])
	return id, nil
}

func callComputeMessageHash(inputHash, outputHash [32]byte, timestamp *big.Int) ([32]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: bytes32Type}, {Type: uint256Type}}
	encoded, _ := args.Pack(inputHash, outputHash, timestamp)
	result, err := ethCall(append(SEL_COMPUTE_MSG_HASH, encoded...))
	if err != nil || len(result) < 32 {
		return [32]byte{}, err
	}
	var hash [32]byte
	copy(hash[:], result[:32])
	return hash, nil
}

// ============================================================================
// RPC HELPERS
// ============================================================================

func getFirstAccount() (string, error) {
	resp, err := rpcCall("eth_accounts", []interface{}{})
	if err != nil {
		return "", err
	}
	var result struct{ Result []string }
	json.Unmarshal(resp, &result)
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no accounts")
	}
	return result.Result[0], nil
}

func ethCall(data []byte) ([]byte, error) {
	params := []interface{}{
		map[string]string{"to": TEE_ADDRESS, "data": "0x" + hex.EncodeToString(data)},
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
	json.Unmarshal(resp, &result)
	if result.Error != nil {
		return nil, fmt.Errorf(result.Error.Message)
	}
	if len(result.Result) > 2 {
		return hex.DecodeString(result.Result[2:])
	}
	return nil, nil
}

func sendTx(from string, data []byte) (string, error) {
	params := []interface{}{
		map[string]string{"from": from, "to": TEE_ADDRESS, "gas": "0x500000", "data": "0x" + hex.EncodeToString(data)},
	}
	resp, err := rpcCall("eth_sendTransaction", params)
	if err != nil {
		return "", err
	}
	var result struct {
		Result string
		Error  *struct{ Message string }
	}
	json.Unmarshal(resp, &result)
	if result.Error != nil {
		return "", fmt.Errorf(result.Error.Message)
	}
	return result.Result, nil
}

func waitForTx(txHash string) bool {
	for i := 0; i < 15; i++ {
		resp, _ := rpcCall("eth_getTransactionReceipt", []string{txHash})
		var result struct {
			Result *struct{ Status string }
		}
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			return result.Result.Status == "0x1"
		}
		time.Sleep(time.Second)
	}
	return false
}

func rpcCall(method string, params interface{}) ([]byte, error) {
	body := map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": 1}
	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(RPC_URL, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
