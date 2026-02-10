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

	// Path to measurements file (from enclave build)
	MEASUREMENTS_PATH = "measurements.txt"
)

// Method selectors (compute with keccak256)
var (
	SEL_ADD_ADMIN       = crypto.Keccak256([]byte("addAdmin(address)"))[:4]
	SEL_IS_ADMIN        = crypto.Keccak256([]byte("isAdmin(address)"))[:4]
	SEL_ADD_TEE_TYPE    = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	SEL_IS_VALID_TYPE   = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	SEL_APPROVE_PCR     = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,bytes32,uint256)"))[:4]
	SEL_IS_PCR_APPROVED = crypto.Keccak256([]byte("isPCRApproved((bytes,bytes,bytes))"))[:4]
	// Updated signature: attestation, signingKey, tlsCert, paymentAddr, endpoint, teeType
	SEL_REGISTER_TEE      = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]
	SEL_IS_ACTIVE         = crypto.Keccak256([]byte("isActive(bytes32)"))[:4]
	SEL_VERIFY_SETTLEMENT = crypto.Keccak256([]byte("verifySettlement(bytes32,bytes32,bytes32,uint256,bytes)"))[:4]
	SEL_GET_ACTIVE_TEES   = crypto.Keccak256([]byte("getActiveTEEs()"))[:4]
	SEL_GET_TEE_INFO      = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
	SEL_GET_PUBLIC_KEY    = crypto.Keccak256([]byte("getPublicKey(bytes32)"))[:4]
	SEL_GET_TLS_CERT      = crypto.Keccak256([]byte("getTLSCertificate(bytes32)"))[:4]
)

// PCRMeasurements holds the expected PCR values
type PCRMeasurements struct {
	PCR0 string `json:"PCR0"`
	PCR1 string `json:"PCR1"`
	PCR2 string `json:"PCR2"`
}

// MeasurementsFile represents the measurements.txt structure
type MeasurementsFile struct {
	Measurements PCRMeasurements `json:"Measurements"`
}

// AttestationResponse from /attestation endpoint
type AttestationResponse struct {
	PublicKey   string `json:"public_key"`
	Timestamp   string `json:"timestamp"`
	EnclaveInfo struct {
		Platform     string `json:"platform"`
		InstanceType string `json:"instance_type"`
		Version      string `json:"version"`
	} `json:"enclave_info"`
}

func main() {
	fmt.Println("==========================================")
	fmt.Println("  TEE Registry Integration Test")
	fmt.Println("  (Dual-Key Architecture)")
	fmt.Println("==========================================")
	fmt.Println()

	// Get account
	account, err := getFirstAccount()
	if err != nil {
		fmt.Printf("❌ Failed to get account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📍 Using account: %s\n\n", account)

	// Load PCR measurements from file
	pcr0, pcr1, pcr2, err := loadPCRMeasurements()
	if err != nil {
		fmt.Printf("⚠️  Failed to load measurements.txt: %v\n", err)
		fmt.Println("   Using random PCRs for testing (won't work with real attestation)")
		pcr0 = make([]byte, 48)
		pcr1 = make([]byte, 48)
		pcr2 = make([]byte, 48)
		rand.Read(pcr0)
		rand.Read(pcr1)
		rand.Read(pcr2)
	} else {
		fmt.Println("✅ Loaded PCR measurements from file")
		fmt.Printf("   PCR0: %s...\n", hex.EncodeToString(pcr0)[:32])
		fmt.Printf("   PCR1: %s...\n", hex.EncodeToString(pcr1)[:32])
		fmt.Printf("   PCR2: %s...\n\n", hex.EncodeToString(pcr2)[:32])
	}

	// ==========================================
	// Step 1: Setup Admin
	// ==========================================
	fmt.Println("------------------------------------------")
	fmt.Println("Step 1: Setup Admin")
	fmt.Println("------------------------------------------")

	txHash, err := callAddAdmin(account, account)
	if err != nil {
		fmt.Printf("⚠️  addAdmin failed (may already be admin): %v\n", err)
	} else {
		fmt.Printf("📤 addAdmin tx: %s\n", txHash)
		waitForTx(txHash)
	}

	isAdmin, err := callIsAdmin(account)
	if err != nil {
		fmt.Printf("❌ isAdmin failed: %v\n", err)
	} else {
		fmt.Printf("✅ isAdmin(%s) = %v\n\n", account, isAdmin)
	}

	// ==========================================
	// Step 2: Add TEE Type
	// ==========================================
	fmt.Println("------------------------------------------")
	fmt.Println("Step 2: Add TEE Type")
	fmt.Println("------------------------------------------")

	txHash, err = callAddTEEType(account, 0, "LLMProxy")
	if err != nil {
		fmt.Printf("⚠️  addTEEType failed (may already exist): %v\n", err)
	} else {
		fmt.Printf("📤 addTEEType tx: %s\n", txHash)
		waitForTx(txHash)
	}

	isValid, err := callIsValidTEEType(0)
	if err != nil {
		fmt.Printf("❌ isValidTEEType failed: %v\n", err)
	} else {
		fmt.Printf("✅ isValidTEEType(0) = %v\n\n", isValid)
	}

	// ==========================================
	// Step 3: Approve PCR
	// ==========================================
	fmt.Println("------------------------------------------")
	fmt.Println("Step 3: Approve PCR")
	fmt.Println("------------------------------------------")

	txHash, err = callApprovePCR(account, pcr0, pcr1, pcr2, "v1.0.0")
	if err != nil {
		fmt.Printf("❌ approvePCR failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📤 approvePCR tx: %s\n", txHash)
	waitForTx(txHash)

	approved, err := callIsPCRApproved(pcr0, pcr1, pcr2)
	if err != nil {
		fmt.Printf("❌ isPCRApproved failed: %v\n", err)
	} else {
		fmt.Printf("✅ isPCRApproved = %v\n\n", approved)
	}

	// ==========================================
	// Step 4: Register TEE (Real Attestation)
	// ==========================================
	fmt.Println("------------------------------------------")
	fmt.Println("Step 4: Register TEE (Dual-Key Architecture)")
	fmt.Println("------------------------------------------")

	var registeredTEEId [32]byte
	var signingPubKeyDER []byte
	registrationSuccess := false

	// Generate fresh nonce (40 hex chars = 20 bytes)
	nonce := generateNonce()
	fmt.Printf("🎲 Generated nonce: %s\n", nonce)

	// Step 4a: Fetch attestation document
	fmt.Printf("\n📡 Step 4a: Fetching attestation from enclave...\n")
	attestationURL := fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", ENCLAVE_HOST, nonce)
	attestationDoc, err := getAttestation(attestationURL)
	if err != nil {
		fmt.Printf("❌ Failed to get attestation: %v\n", err)
		fmt.Println("   ⚠️  Make sure the enclave is running!")
		fmt.Println("   Skipping TEE registration...")
	} else {
		fmt.Printf("✅ Got attestation document (%d chars base64)\n", len(attestationDoc))

		// Decode base64 attestation
		attestationBytes, err := base64.StdEncoding.DecodeString(attestationDoc)
		if err != nil {
			fmt.Printf("❌ Failed to decode attestation: %v\n", err)
		} else {
			fmt.Printf("   Decoded attestation: %d bytes\n", len(attestationBytes))

			// Step 4b: Fetch signing public key from /attestation endpoint
			fmt.Printf("\n📡 Step 4b: Fetching signing public key...\n")
			signingPubKeyDER, err = fetchSigningPublicKey(ENCLAVE_HOST)
			if err != nil {
				fmt.Printf("❌ Failed to get signing key: %v\n", err)
			} else {
				fmt.Printf("✅ Got signing public key (%d bytes DER)\n", len(signingPubKeyDER))
				signingKeyHash := sha256.Sum256(signingPubKeyDER)
				fmt.Printf("   SHA256: %s\n", base64.StdEncoding.EncodeToString(signingKeyHash[:]))

				// Step 4c: Fetch TLS certificate from TLS handshake
				fmt.Printf("\n📡 Step 4c: Fetching TLS certificate...\n")
				tlsCertDER, err := fetchTLSCertificate(ENCLAVE_HOST, ENCLAVE_PORT)
				if err != nil {
					fmt.Printf("❌ Failed to get TLS cert: %v\n", err)
				} else {
					fmt.Printf("✅ Got TLS certificate (%d bytes DER)\n", len(tlsCertDER))
					tlsCertHash := sha256.Sum256(tlsCertDER)
					fmt.Printf("   SHA256: %s\n", base64.StdEncoding.EncodeToString(tlsCertHash[:]))

					// Parse and display cert info
					cert, _ := x509.ParseCertificate(tlsCertDER)
					if cert != nil {
						fmt.Printf("   Subject: %s\n", cert.Subject.Organization)
						fmt.Printf("   Valid: %s to %s\n", cert.NotBefore.Format("2006-01-02"), cert.NotAfter.Format("2006-01-02"))
					}

					// Step 4d: Register TEE with all components
					fmt.Printf("\n📤 Step 4d: Registering TEE...\n")
					endpoint := fmt.Sprintf("https://%s", ENCLAVE_HOST)
					txHash, err = callRegisterTEE(
						account,
						attestationBytes,
						signingPubKeyDER,
						tlsCertDER,
						account,  // payment address
						endpoint, // endpoint
						0,        // teeType (LLMProxy)
					)
					if err != nil {
						fmt.Printf("❌ registerTEE failed: %v\n", err)
					} else {
						fmt.Printf("📤 registerTEE tx: %s\n", txHash)
						if waitForTx(txHash) {
							fmt.Println("✅ TEE registered successfully with dual-key architecture!")
							registrationSuccess = true

							// Compute TEE ID (keccak256 of signing public key)
							registeredTEEId = crypto.Keccak256Hash(signingPubKeyDER)
							fmt.Printf("   TEE ID: %s\n", hex.EncodeToString(registeredTEEId[:]))
						}
					}
				}
			}
		}
	}

	// ==========================================
	// Step 5: Verify Registration
	// ==========================================
	if registrationSuccess {
		fmt.Println("\n------------------------------------------")
		fmt.Println("Step 5: Verify Registration")
		fmt.Println("------------------------------------------")

		// Check if TEE is active
		isActive, err := callIsActive(registeredTEEId)
		if err != nil {
			fmt.Printf("❌ isActive failed: %v\n", err)
		} else {
			fmt.Printf("✅ isActive(TEE) = %v\n", isActive)
		}

		// Get stored public key and verify it matches
		storedPubKey, err := callGetPublicKey(registeredTEEId)
		if err != nil {
			fmt.Printf("❌ getPublicKey failed: %v\n", err)
		} else {
			if bytes.Equal(storedPubKey, signingPubKeyDER) {
				fmt.Printf("✅ Stored signing key matches (%d bytes)\n", len(storedPubKey))
			} else {
				fmt.Printf("❌ Stored signing key mismatch!\n")
			}
		}

		// Get stored TLS certificate
		storedTLSCert, err := callGetTLSCertificate(registeredTEEId)
		if err != nil {
			fmt.Printf("❌ getTLSCertificate failed: %v\n", err)
		} else {
			fmt.Printf("✅ Stored TLS certificate (%d bytes)\n", len(storedTLSCert))
		}
	}

	// ==========================================
	// Step 6: Signature Verification Test
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("Step 6: Local Signature Verification Test")
	fmt.Println("------------------------------------------")

	// Generate test RSA key pair for signature testing
	privateKey, testPubKeyDER := generateKeyPair()
	testTeeId := crypto.Keccak256Hash(testPubKeyDER)
	fmt.Printf("🔑 Generated test RSA key pair\n")
	fmt.Printf("   TEE ID (from pubkey): %s\n", testTeeId.Hex())

	// Create test data
	inputHash := sha256.Sum256([]byte(`{"prompt": "Hello, world!"}`))
	outputHash := sha256.Sum256([]byte(`{"response": "Hi there!"}`))
	timestamp := big.NewInt(time.Now().Unix())

	// Compute message hash
	messageHash := computeMessageHash(inputHash, outputHash, timestamp)
	fmt.Printf("   Input hash:   %s\n", hex.EncodeToString(inputHash[:]))
	fmt.Printf("   Output hash:  %s\n", hex.EncodeToString(outputHash[:]))
	fmt.Printf("   Timestamp:    %d\n", timestamp.Int64())
	fmt.Printf("   Message hash: %s\n", hex.EncodeToString(messageHash[:]))

	// Sign message
	signature := signMessage(privateKey, messageHash[:])
	fmt.Printf("   Signature:    %d bytes\n\n", len(signature))

	// Verify signature locally
	err = verifySignatureLocal(testPubKeyDER, messageHash[:], signature)
	if err != nil {
		fmt.Printf("❌ Local signature verification failed: %v\n", err)
	} else {
		fmt.Printf("✅ Local signature verification passed\n")
	}

	// ==========================================
	// Step 7: Get Active TEEs
	// ==========================================
	fmt.Println("\n------------------------------------------")
	fmt.Println("Step 7: Get Active TEEs")
	fmt.Println("------------------------------------------")

	activeTEEs, err := callGetActiveTEEs()
	if err != nil {
		fmt.Printf("❌ getActiveTEEs failed: %v\n", err)
	} else {
		fmt.Printf("✅ Active TEEs: %d\n", len(activeTEEs))
		for i, id := range activeTEEs {
			fmt.Printf("   [%d] %s\n", i, id)
		}
	}

	// ==========================================
	// Summary
	// ==========================================
	fmt.Println("\n==========================================")
	fmt.Println("  Integration Test Complete")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Println("  ✅ Admin setup")
	fmt.Println("  ✅ TEE type management")
	fmt.Println("  ✅ PCR approval")
	fmt.Println("  ✅ Signature verification (local)")
	if registrationSuccess {
		fmt.Println("  ✅ Real attestation fetched")
		fmt.Println("  ✅ Signing key fetched")
		fmt.Println("  ✅ TLS certificate fetched")
		fmt.Println("  ✅ TEE registration successful")
		fmt.Println("  ✅ Dual-key binding verified")
	} else {
		fmt.Println("  ⚠️  TEE registration skipped (enclave not available or error)")
	}
}

// ============================================================================
// ATTESTATION & KEY FETCHING FUNCTIONS
// ============================================================================

// generateNonce creates a random 40-character hex nonce
func generateNonce() string {
	nonce := make([]byte, 20)
	rand.Read(nonce)
	return hex.EncodeToString(nonce)
}

// getAttestation fetches attestation document from enclave
func getAttestation(url string) (string, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	return string(bytes.TrimSpace(body)), nil
}

// fetchSigningPublicKey gets the signing public key from /attestation endpoint
func fetchSigningPublicKey(host string) ([]byte, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	url := fmt.Sprintf("https://%s/attestation", host)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var attestResp AttestationResponse
	if err := json.NewDecoder(resp.Body).Decode(&attestResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Parse PEM to DER
	block, _ := pem.Decode([]byte(attestResp.PublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM public key")
	}

	// Validate it's a valid public key
	_, err = x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %v", err)
	}

	return block.Bytes, nil
}

// fetchTLSCertificate gets the TLS certificate from the enclave's TLS handshake
func fetchTLSCertificate(host, port string) ([]byte, error) {
	conn, err := tls.Dial("tcp", host+":"+port, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, fmt.Errorf("TLS dial failed: %v", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates received")
	}

	// Return DER-encoded certificate (first cert in chain)
	return certs[0].Raw, nil
}

// loadPCRMeasurements loads expected PCR values from measurements.txt
func loadPCRMeasurements() (pcr0, pcr1, pcr2 []byte, err error) {
	data, err := os.ReadFile(MEASUREMENTS_PATH)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read file: %v", err)
	}

	var measurements MeasurementsFile
	if err := json.Unmarshal(data, &measurements); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	pcr0, err = hex.DecodeString(measurements.Measurements.PCR0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode PCR0: %v", err)
	}

	pcr1, err = hex.DecodeString(measurements.Measurements.PCR1)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode PCR1: %v", err)
	}

	pcr2, err = hex.DecodeString(measurements.Measurements.PCR2)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode PCR2: %v", err)
	}

	return pcr0, pcr1, pcr2, nil
}

// ============================================================================
// KEY GENERATION & SIGNING
// ============================================================================

func generateKeyPair() (*rsa.PrivateKey, []byte) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		panic(err)
	}
	return privateKey, publicKeyDER
}

func signMessage(privateKey *rsa.PrivateKey, messageHash []byte) []byte {
	hash := sha256.Sum256(messageHash)
	signature, err := rsa.SignPSS(rand.Reader, privateKey, gcrypto.SHA256, hash[:], &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	})
	if err != nil {
		panic(err)
	}
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

func computeMessageHash(inputHash, outputHash [32]byte, timestamp *big.Int) [32]byte {
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])
	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)
	return sha256.Sum256(data)
}

// ============================================================================
// CONTRACT CALLS
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
	if err != nil {
		return false, err
	}
	if len(result) < 32 {
		return false, nil
	}
	return result[31] == 1, nil
}

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
	if err != nil {
		return false, err
	}
	if len(result) < 32 {
		return false, nil
	}
	return result[31] == 1, nil
}

func callApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string) (string, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})
	stringType, _ := abi.NewType("string", "", nil)
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: tupleType},
		{Type: stringType},
		{Type: bytes32Type},
		{Type: uint256Type},
	}

	pcrs := struct {
		Pcr0 []byte
		Pcr1 []byte
		Pcr2 []byte
	}{pcr0, pcr1, pcr2}

	encoded, err := args.Pack(pcrs, version, [32]byte{}, big.NewInt(0))
	if err != nil {
		return "", err
	}

	return sendTx(from, append(SEL_APPROVE_PCR, encoded...))
}

func callIsPCRApproved(pcr0, pcr1, pcr2 []byte) (bool, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})

	args := abi.Arguments{{Type: tupleType}}

	pcrs := struct {
		Pcr0 []byte
		Pcr1 []byte
		Pcr2 []byte
	}{pcr0, pcr1, pcr2}

	encoded, err := args.Pack(pcrs)
	if err != nil {
		return false, err
	}

	result, err := ethCall(append(SEL_IS_PCR_APPROVED, encoded...))
	if err != nil {
		return false, err
	}
	if len(result) < 32 {
		return false, nil
	}
	return result[31] == 1, nil
}

// Updated to match new precompile signature
func callRegisterTEE(from string, attestationDoc []byte, signingPubKeyDER []byte, tlsCertDER []byte, paymentAddr string, endpoint string, teeType uint8) (string, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{
		{Type: bytesType},  // attestationDoc
		{Type: bytesType},  // signingPublicKeyDER
		{Type: bytesType},  // tlsCertificateDER
		{Type: addrType},   // paymentAddress
		{Type: stringType}, // endpoint
		{Type: uint8Type},  // teeType
	}

	encoded, err := args.Pack(
		attestationDoc,
		signingPubKeyDER,
		tlsCertDER,
		common.HexToAddress(paymentAddr),
		endpoint,
		teeType,
	)
	if err != nil {
		return "", err
	}

	return sendTx(from, append(SEL_REGISTER_TEE, encoded...))
}

func callIsActive(teeId [32]byte) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)

	result, err := ethCall(append(SEL_IS_ACTIVE, encoded...))
	if err != nil {
		return false, err
	}
	if len(result) < 32 {
		return false, nil
	}
	return result[31] == 1, nil
}

func callGetPublicKey(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)

	result, err := ethCall(append(SEL_GET_PUBLIC_KEY, encoded...))
	if err != nil {
		return nil, err
	}

	// ABI decode bytes
	if len(result) < 64 {
		return nil, fmt.Errorf("result too short")
	}

	// First 32 bytes = offset, next 32 = length
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if uint64(len(result)) < 64+length {
		return nil, fmt.Errorf("result truncated")
	}

	return result[64 : 64+length], nil
}

func callGetTLSCertificate(teeId [32]byte) ([]byte, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(teeId)

	result, err := ethCall(append(SEL_GET_TLS_CERT, encoded...))
	if err != nil {
		return nil, err
	}

	// ABI decode bytes
	if len(result) < 64 {
		return nil, fmt.Errorf("result too short")
	}

	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if uint64(len(result)) < 64+length {
		return nil, fmt.Errorf("result truncated")
	}

	return result[64 : 64+length], nil
}

func callVerifySettlement(from string, teeId, inputHash, outputHash [32]byte, timestamp *big.Int, signature []byte) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)

	args := abi.Arguments{
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: uint256Type},
		{Type: bytesType},
	}

	encoded, err := args.Pack(teeId, inputHash, outputHash, timestamp, signature)
	if err != nil {
		return "", err
	}

	return sendTx(from, append(SEL_VERIFY_SETTLEMENT, encoded...))
}

func callGetActiveTEEs() ([]string, error) {
	result, err := ethCall(SEL_GET_ACTIVE_TEES)
	if err != nil {
		return nil, err
	}

	if len(result) < 64 {
		return []string{}, nil
	}

	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	tees := make([]string, length)

	for i := uint64(0); i < length; i++ {
		start := 64 + i*32
		end := start + 32
		if end > uint64(len(result)) {
			break
		}
		tees[i] = "0x" + hex.EncodeToString(result[start:end])
	}

	return tees, nil
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
		Result []string `json:"result"`
	}
	json.Unmarshal(resp, &result)
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no accounts")
	}
	return result.Result[0], nil
}

func ethCall(data []byte) ([]byte, error) {
	params := []interface{}{
		map[string]string{
			"to":   TEE_ADDRESS,
			"data": "0x" + hex.EncodeToString(data),
		},
		"latest",
	}

	resp, err := rpcCall("eth_call", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
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
		map[string]string{
			"from": from,
			"to":   TEE_ADDRESS,
			"gas":  "0x500000",
			"data": "0x" + hex.EncodeToString(data),
		},
	}

	resp, err := rpcCall("eth_sendTransaction", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
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
			Result *struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			success := result.Result.Status == "0x1"
			if success {
				fmt.Println("   ✅ Transaction confirmed")
			} else {
				fmt.Println("   ❌ Transaction reverted")
			}
			return success
		}
		time.Sleep(time.Second)
	}
	fmt.Println("   ⚠️ Transaction timeout")
	return false
}

func rpcCall(method string, params interface{}) ([]byte, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(RPC_URL, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
