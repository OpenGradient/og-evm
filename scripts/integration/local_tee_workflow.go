//go:build integration

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
	"strings"
	"time"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fxamacker/cbor/v2"
)

var (
	RPC_URL           = "http://127.0.0.1:8545"
	ENCLAVE_HOST      = getEnvOrDefault("TEE_ENCLAVE_HOST", "127.0.0.1")
	ENCLAVE_PORT      = getEnvOrDefault("TEE_ENCLAVE_PORT", "443")
	MEASUREMENTS_PATH = getEnvOrDefault("TEE_MEASUREMENTS_PATH", "measurements.txt")
	VERIFIER_ADDRESS  = "0x0000000000000000000000000000000000000900"
	TEE_REGISTRY_ADDRESS

)


// ============================================================================
// PASTE YOUR NEW COMPILED BYTECODE HERE
// ============================================================================
const TEE_REGISTRY_BYTECODE = ""

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
	SEL_GRANT_ROLE     = crypto.Keccak256([]byte("grantRole(bytes32,address)"))[:4]
	SEL_REVOKE_ROLE    = crypto.Keccak256([]byte("revokeRole(bytes32,address)"))[:4]
	SEL_HAS_ROLE       = crypto.Keccak256([]byte("hasRole(bytes32,address)"))[:4]
	SEL_GET_ROLE_ADMIN = crypto.Keccak256([]byte("getRoleAdmin(bytes32)"))[:4]

	SEL_ADD_TEE_TYPE  = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	SEL_IS_VALID_TYPE = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	SEL_GET_TEE_TYPES = crypto.Keccak256([]byte("getTEETypes()"))[:4]

	SEL_APPROVE_PCR      = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,uint8)"))[:4]
	SEL_REVOKE_PCR       = crypto.Keccak256([]byte("revokePCR(bytes32,uint256)"))[:4]
	SEL_IS_PCR_APPROVED  = crypto.Keccak256([]byte("isPCRApproved(bytes32)"))[:4]
	SEL_COMPUTE_PCR_HASH = crypto.Keccak256([]byte("computePCRHash((bytes,bytes,bytes))"))[:4]
	SEL_GET_ACTIVE_PCRS  = crypto.Keccak256([]byte("getActivePCRs()"))[:4]

	SEL_SET_AWS_ROOT_CERT = crypto.Keccak256([]byte("setAWSRootCertificate(bytes)"))[:4]

	SEL_REGISTER_TEE   = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]
	SEL_DEACTIVATE_TEE = crypto.Keccak256([]byte("deactivateTEE(bytes32)"))[:4]
	SEL_ACTIVATE_TEE   = crypto.Keccak256([]byte("activateTEE(bytes32)"))[:4]

	SEL_GET_TEE           = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
	SEL_GET_ACTIVE_TEES   = crypto.Keccak256([]byte("getActiveTEEs()"))[:4]
	SEL_GET_TEES_BY_TYPE  = crypto.Keccak256([]byte("getTEEsByType(uint8)"))[:4]
	SEL_GET_TEES_BY_OWNER = crypto.Keccak256([]byte("getTEEsByOwner(address)"))[:4]
	SEL_GET_PUBLIC_KEY    = crypto.Keccak256([]byte("getPublicKey(bytes32)"))[:4]
	SEL_GET_TLS_CERT      = crypto.Keccak256([]byte("getTLSCertificate(bytes32)"))[:4]
	SEL_IS_ACTIVE         = crypto.Keccak256([]byte("isActive(bytes32)"))[:4]
	SEL_GET_PAYMENT_ADDR  = crypto.Keccak256([]byte("getPaymentAddress(bytes32)"))[:4]

	SEL_COMPUTE_TEE_ID   = crypto.Keccak256([]byte("computeTEEId(bytes)"))[:4]
	SEL_COMPUTE_MSG_HASH = crypto.Keccak256([]byte("computeMessageHash(bytes32,bytes32,uint256)"))[:4]
)

// ============================================================================
// Structs
// ============================================================================

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

// ============================================================================
// MAIN
// ============================================================================

func main() {
	fmt.Println("==========================================")
	fmt.Println("  TEE Registry Integration Test")
	fmt.Println("  (AccessControl + Dual-Key Architecture)")
	fmt.Println("==========================================")
	fmt.Println()

	results := &TestResults{}

	account, err := getFirstAccount()
	if err != nil {
		fmt.Printf("❌ Failed to get account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📍 Primary account: %s\n\n", account)

	// SECTION 0: Deploy or Find TEERegistry
	fmt.Println("------------------------------------------")
	fmt.Println("SECTION 0: Contract Deployment")
	fmt.Println("------------------------------------------")

	registryAddress, err := deployOrFindRegistry(account)
	if err != nil {
		fmt.Printf("❌ Failed to deploy/find TEERegistry: %v\n", err)
		os.Exit(1)
	}
	TEE_REGISTRY_ADDRESS = registryAddress
	fmt.Printf("  📄 TEERegistry address: %s\n", TEE_REGISTRY_ADDRESS)
	results.Add("TEERegistry deployed/found", true, "")

	// Load PCR measurements
	pcr0, pcr1, pcr2, err := loadPCRMeasurements()
	if err != nil {
		fmt.Printf("⚠️  Failed to load measurements.txt: %v\n", err)
		fmt.Println("   Using random PCRs for testing")
		pcr0, pcr1, pcr2 = make([]byte, 48), make([]byte, 48), make([]byte, 48)
		rand.Read(pcr0)
		rand.Read(pcr1)
		rand.Read(pcr2)
	} else {
		// [LOG] Show PCRs loaded from file so we can cross-check against enclave
		fmt.Printf("  📂 Loaded PCRs from %s:\n", MEASUREMENTS_PATH)
		fmt.Printf("     PCR0: %s\n", hex.EncodeToString(pcr0))
		fmt.Printf("     PCR1: %s\n", hex.EncodeToString(pcr1))
		fmt.Printf("     PCR2: %s\n", hex.EncodeToString(pcr2))
	}

	// SECTION 1: Role Management (AccessControl)
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 1: Role Management (AccessControl)")
	fmt.Println("------------------------------------------")

	hasAdmin, _ := callHasRole(DEFAULT_ADMIN_ROLE, account)
	results.Add("Deployer has DEFAULT_ADMIN_ROLE", hasAdmin, "")

	hasOperator, _ := callHasRole(TEE_OPERATOR_ROLE, account)
	results.Add("Deployer has TEE_OPERATOR role", hasOperator, "")

	hasAdmin, _ = callHasRole(DEFAULT_ADMIN_ROLE, "0x0000000000000000000000000000000000000001")
	results.Add("Random address doesn't have admin role", !hasAdmin, "")

	// SECTION 2: TEE Type Management
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 2: TEE Type Management")
	fmt.Println("------------------------------------------")

	isValid, _ := callIsValidTEEType(0)
	if !isValid {
		txHash, err := callAddTEEType(account, 0, "LLMProxy")
		if err == nil {
			waitForTx(txHash)
		}
	}
	isValid, _ = callIsValidTEEType(0)
	results.Add("TEE type 0 (LLMProxy) valid", isValid, "")

	isValid, _ = callIsValidTEEType(1)
	if !isValid {
		txHash, err := callAddTEEType(account, 1, "Validator")
		if err == nil {
			waitForTx(txHash)
		}
	}
	isValid, _ = callIsValidTEEType(1)
	results.Add("TEE type 1 (Validator) valid", isValid, "")

	isValid, _ = callIsValidTEEType(99)
	results.Add("isValidTEEType returns false for unknown type", !isValid, "")

	// SECTION 3: PCR Management
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 3: PCR Management")
	fmt.Println("------------------------------------------")

	pcrHash, _ := callComputePCRHash(pcr0, pcr1, pcr2)
	fmt.Printf("  📊 PCR Hash (file measurements): 0x%s\n", hex.EncodeToString(pcrHash[:]))

	// [LOG] Show existing approved PCRs before making any changes
	fmt.Println("  🔍 Fetching currently approved PCRs from contract...")
	existingActivePCRs, err := callGetActivePCRs()
	if err != nil {
		fmt.Printf("  ⚠️  Could not fetch active PCRs: %v\n", err)
	} else {
		fmt.Printf("  📋 Currently approved PCRs (%d):\n", len(existingActivePCRs))
		for i, p := range existingActivePCRs {
			fmt.Printf("     [%d] %s\n", i, p)
		}
	}
	if len(existingActivePCRs) == 0 {
		txHash, err := callApprovePCR(account, pcr0, pcr1, pcr2, "v1.0.0", 0)
		if err == nil {
			waitForTx(txHash)
		}
	}
	approved, _ := callIsPCRApproved(pcrHash)
	// [LOG] Clearly show the approval status of the PCR we are about to use
	fmt.Printf("  🔍 isPCRApproved(filePCRHash=%s) = %v\n", hex.EncodeToString(pcrHash[:]), approved)

	if !approved {
		fmt.Print("PCRs do not match")
		os.Exit(1)
	}
	results.Add("PCR v1.0.0 approved", approved, "")

	fakePCR := make([]byte, 48)
	rand.Read(fakePCR)
	fakeHash, _ := callComputePCRHash(fakePCR, pcr1, pcr2)
	fmt.Printf("  🔍 isPCRApproved(fakePCRHash=%s) = expected false\n", hex.EncodeToString(fakeHash[:]))
	approved, _ = callIsPCRApproved(fakeHash)
	results.Add("isPCRApproved returns false for unknown PCR", !approved, "")

	activePCRs, err := callGetActivePCRs()
	results.Add("getActivePCRs returns list", err == nil && len(activePCRs) > 0, fmt.Sprintf("count=%d", len(activePCRs)))

	// SECTION 4: TEE Registration
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 4: TEE Registration")
	fmt.Println("------------------------------------------")

	// Set AWS root certificate before attempting registration
	fmt.Println("  📜 Setting AWS Nitro root certificate...")
	awsRootCert, err := getAWSNitroRootCert()
	if err != nil {
		fmt.Printf("  ❌ Failed to get AWS root cert: %v\n", err)
	} else {
		txHash, err := callSetAWSRootCert(account, awsRootCert)
		if err != nil {
			fmt.Printf("  ⚠️  Set AWS root cert failed (may already be set): %v\n", err)
		} else {
			waitForTx(txHash)
			fmt.Printf("  ✅ AWS root cert set (%d bytes)\n", len(awsRootCert))
		}
	}

	var registeredTEEId [32]byte
	var signingPubKeyDER []byte
	registrationSuccess := false

	nonce := generateNonce()
	fmt.Printf("  🎲 Nonce: %s\n", nonce)

	attestationURL := fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", ENCLAVE_HOST, nonce)
	attestationDoc, err := getAttestation(attestationURL)
	if err != nil {
		results.Add("Fetch attestation from enclave", false, err.Error())
	} else {
		results.Add("Fetch attestation from enclave", true, fmt.Sprintf("%d chars", len(attestationDoc)))

		attestationBytes, _ := base64.StdEncoding.DecodeString(attestationDoc)
		fmt.Println("  🔍 Extracting PCRs from attestation document...")
		realPCRs, err := extractPCRsFromAttestation(attestationBytes)
		if err != nil {
			fmt.Printf("  ❌ Failed to extract PCRs: %v\n", err)
		} else {
			// [LOG] Print all PCRs extracted from the attestation, not just 0-1-2
			fmt.Printf("  📊 PCRs extracted from attestation (%d total):\n", len(realPCRs))
			for idx, val := range realPCRs {
				fmt.Printf("     PCR%d: %s\n", idx, hex.EncodeToString(val))
			}

			realPCR0 := realPCRs[0]
			realPCR1 := realPCRs[1]
			realPCR2 := realPCRs[2]

			// [LOG] Explicit comparison between file PCRs and attestation PCRs
			fmt.Println("  🔍 Comparing file PCRs vs attestation PCRs:")
			fmt.Printf("     PCR0 match: %v  (file=%s | attest=%s)\n",
				hex.EncodeToString(pcr0) == hex.EncodeToString(realPCR0),
				hex.EncodeToString(pcr0), hex.EncodeToString(realPCR0))
			fmt.Printf("     PCR1 match: %v  (file=%s | attest=%s)\n",
				hex.EncodeToString(pcr1) == hex.EncodeToString(realPCR1),
				hex.EncodeToString(pcr1), hex.EncodeToString(realPCR1))
			fmt.Printf("     PCR2 match: %v  (file=%s | attest=%s)\n",
				hex.EncodeToString(pcr2) == hex.EncodeToString(realPCR2),
				hex.EncodeToString(pcr2), hex.EncodeToString(realPCR2))

			// Approve the REAL PCRs from the enclave
			realPCRHash, _ := callComputePCRHash(realPCR0, realPCR1, realPCR2)
			fmt.Printf("  📊 Real enclave PCR hash: 0x%s\n", hex.EncodeToString(realPCRHash[:]))

			realApproved, _ := callIsPCRApproved(realPCRHash)
			fmt.Printf("  🔍 isPCRApproved(realPCRHash) = %v\n", realApproved)

			if !realApproved {
				fmt.Println("  📝 Approving real enclave PCRs...")
				txHash, err := callApprovePCR(account, realPCR0, realPCR1, realPCR2, "enclave-v1", 0)
				if err == nil {
					waitForTx(txHash)
					// [LOG] Verify the approval actually stuck
					postApproval, _ := callIsPCRApproved(realPCRHash)
					fmt.Printf("  ✅ Real PCRs approved — post-approval isPCRApproved=%v\n", postApproval)
				} else {
					fmt.Printf("  ❌ Failed to approve real PCRs: %v\n", err)
				}
			} else {
				fmt.Println("  ✅ Real PCRs already approved")
			}

			// [LOG] Dump final state of active PCRs right before registration attempt
			fmt.Println("  🔍 Active PCRs in contract (pre-registration):")
			preRegPCRs, err := callGetActivePCRs()
			if err != nil {
				fmt.Printf("     ⚠️  Could not fetch: %v\n", err)
			} else {
				for i, p := range preRegPCRs {
					fmt.Printf("     [%d] %s\n", i, p)
				}
			}
		}

		fmt.Printf("  🔍 About to call fetchSigningPublicKey with host: %s\n", ENCLAVE_HOST)
		signingPubKeyDER, err = fetchSigningPublicKey(ENCLAVE_HOST)
		fmt.Printf("  🔍 fetchSigningPublicKey returned: err=%v, keyLen=%d\n", err, len(signingPubKeyDER))
		if err != nil {
			results.Add("Fetch signing public key", false, err.Error())
		} else {
			results.Add("Fetch signing public key", true, fmt.Sprintf("%d bytes", len(signingPubKeyDER)))

			// [LOG] Show the TEE ID that will be derived from the signing key
			expectedTeeId := crypto.Keccak256Hash(signingPubKeyDER)
			fmt.Printf("  🔑 Signing key SHA256: %x\n", sha256.Sum256(signingPubKeyDER))
			fmt.Printf("  🆔 Expected TEE ID (keccak256(signingKey)): 0x%s\n", hex.EncodeToString(expectedTeeId[:]))

			tlsCertDER, err := fetchTLSCertificate(ENCLAVE_HOST, ENCLAVE_PORT)
			if err != nil {
				results.Add("Fetch TLS certificate", false, err.Error())
			} else {
				results.Add("Fetch TLS certificate", true, fmt.Sprintf("%d bytes", len(tlsCertDER)))

				// [LOG] Show TLS cert fingerprint for cross-check
				fmt.Printf("  🔏 TLS cert SHA256: %x\n", sha256.Sum256(tlsCertDER))

				isActive, _ := callIsActive(expectedTeeId)
				fmt.Printf("  🔍 isActive(expectedTeeId) = %v\n", isActive)

				if isActive {
					fmt.Println("  ℹ️  TEE already registered")
					registrationSuccess = true
					registeredTEEId = expectedTeeId
					results.Add("TEE already registered", true, "")
				} else {
					endpoint := fmt.Sprintf("https://%s", ENCLAVE_HOST)

					// [LOG] Summarize all registration inputs before the call
					fmt.Println("  📦 Registration inputs:")
					fmt.Printf("     attestation:  %d bytes\n", len(attestationBytes))
					fmt.Printf("     signingKey:   %d bytes (SHA256=%x)\n", len(signingPubKeyDER), sha256.Sum256(signingPubKeyDER))
					fmt.Printf("     tlsCert:      %d bytes (SHA256=%x)\n", len(tlsCertDER), sha256.Sum256(tlsCertDER))
					fmt.Printf("     paymentAddr:  %s\n", account)
					fmt.Printf("     endpoint:     %s\n", endpoint)
					fmt.Printf("     teeType:      0\n")

					txHash, err := callRegisterTEE(account, attestationBytes, signingPubKeyDER, tlsCertDER, account, endpoint, 0)
					if err != nil {
						fmt.Printf("    ❌ Registration error: %v\n", err)
						results.Add("Register TEE with attestation", false, err.Error())
					} else {
						fmt.Printf("    📤 Tx hash: %s\n", txHash)
						success := waitForTx(txHash)
						results.Add("Register TEE with attestation", success, "")
						if success {
							registrationSuccess = true
							registeredTEEId = expectedTeeId

							// [LOG] Verify post-registration state
							fmt.Printf("  ✅ Registration succeeded — verifying on-chain state...\n")
							postActive, _ := callIsActive(registeredTEEId)
							fmt.Printf("     isActive(teeId)=%v\n", postActive)
						} else {
							// [LOG] On failure, dump current PCR state to diagnose mismatch
							fmt.Println("  ❌ Registration failed — dumping PCR state for diagnosis:")
							failPCRs, _ := callGetActivePCRs()
							fmt.Printf("     Active PCRs in contract (%d):\n", len(failPCRs))
							for i, p := range failPCRs {
								fmt.Printf("       [%d] %s\n", i, p)
							}
						}
					}
				}
			}
		}
	}

	// SECTION 5: TEE Queries
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 5: TEE Queries")
	fmt.Println("------------------------------------------")

	if registrationSuccess {
		isActive, err := callIsActive(registeredTEEId)
		results.Add("isActive returns true for registered TEE", isActive && err == nil, "")

		storedKey, err := callGetPublicKey(registeredTEEId)
		keyMatches := err == nil && bytes.Equal(storedKey, signingPubKeyDER)
		// [LOG] Show key comparison detail when it fails
		if !keyMatches {
			fmt.Printf("  ⚠️  Key mismatch — stored=%d bytes, expected=%d bytes\n", len(storedKey), len(signingPubKeyDER))
			if len(storedKey) > 0 {
				fmt.Printf("     stored SHA256:   %x\n", sha256.Sum256(storedKey))
			}
			fmt.Printf("     expected SHA256: %x\n", sha256.Sum256(signingPubKeyDER))
		}
		results.Add("getPublicKey returns correct key", keyMatches, "")

		storedCert, err := callGetTLSCertificate(registeredTEEId)
		results.Add("getTLSCertificate returns cert", err == nil && len(storedCert) > 0, fmt.Sprintf("%d bytes", len(storedCert)))

		activeTEEs, err := callGetActiveTEEs()
		results.Add("getActiveTEEs includes registered TEE", err == nil && len(activeTEEs) > 0, fmt.Sprintf("count=%d", len(activeTEEs)))

		teesByType, err := callGetTEEsByType(0)
		results.Add("getTEEsByType(0) includes registered TEE", err == nil && len(teesByType) > 0, fmt.Sprintf("count=%d", len(teesByType)))

		teesByOwner, err := callGetTEEsByOwner(account)
		results.Add("getTEEsByOwner includes registered TEE", err == nil && len(teesByOwner) > 0, fmt.Sprintf("count=%d", len(teesByOwner)))
	} else {
		fmt.Println("  ⚠️  Skipping query tests (registration failed)")
	}

	// SECTION 6: TEE Lifecycle
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 6: TEE Lifecycle")
	fmt.Println("------------------------------------------")

	if registrationSuccess {
		txHash, err := callDeactivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
		}
		isActive, _ := callIsActive(registeredTEEId)
		results.Add("Deactivate TEE", !isActive, "")

		txHash, err = callActivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
		}
		isActive, _ = callIsActive(registeredTEEId)
		results.Add("Reactivate TEE", isActive, "")
	} else {
		fmt.Println("  ⚠️  Skipping lifecycle tests (registration failed)")
	}

	// SECTION 7: Signature Verification (Local)
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 7: Signature Verification (Local)")
	fmt.Println("------------------------------------------")

	privateKey, testPubKeyDER := generateKeyPair()
	inputHash := sha256.Sum256([]byte(`{"prompt": "Hello"}`))
	outputHash := sha256.Sum256([]byte(`{"response": "Hi"}`))
	timestamp := big.NewInt(time.Now().Unix())
	messageHash := computeMessageHash(inputHash, outputHash, timestamp)
	signature := signMessage(privateKey, messageHash[:])

	err = verifySignatureLocal(testPubKeyDER, messageHash[:], signature)
	results.Add("Local RSA-PSS signature verification", err == nil, fmt.Sprintf("%v", err))

	badSig := make([]byte, len(signature))
	copy(badSig, signature)
	badSig[0] ^= 0xFF
	err = verifySignatureLocal(testPubKeyDER, messageHash[:], badSig)
	results.Add("Reject invalid signature", err != nil, "")
	// SECTION 8: PCR Revocation & TEE Deactivation Security
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 8: PCR Revocation & TEE Deactivation")
	fmt.Println("------------------------------------------")

	if registrationSuccess {
		fmt.Println("  🔒 Testing PCR revocation security...")

		// Step 1: Create test PCR for revocation testing
		fmt.Println("\n  Step 1: Create test PCR for revocation")
		testPCR0 := make([]byte, 48)
		testPCR1 := make([]byte, 48)
		testPCR2 := make([]byte, 48)
		rand.Read(testPCR0)
		rand.Read(testPCR1)
		rand.Read(testPCR2)

		testPCRHash, _ := callComputePCRHash(testPCR0, testPCR1, testPCR2)
		fmt.Printf("  📊 Test PCR Hash: 0x%s\n", hex.EncodeToString(testPCRHash[:]))

		// Approve the test PCR
		txHash, err := callApprovePCR(account, testPCR0, testPCR1, testPCR2, "test-revoke", 0)
		if err == nil {
			waitForTx(txHash)
			approved, _ := callIsPCRApproved(testPCRHash)
			results.Add("Test PCR approved", approved, "")
		} else {
			results.Add("Approve test PCR", false, err.Error())
		}

		// Step 2: Revoke the test PCR
		fmt.Println("\n  Step 2: Revoke test PCR")
		txHash, err = callRevokePCR(account, testPCRHash, 0)
		if err != nil {
			results.Add("Revoke test PCR", false, err.Error())
		} else {
			waitForTx(txHash)
			stillApproved, _ := callIsPCRApproved(testPCRHash)
			results.Add("Test PCR no longer approved after revocation", !stillApproved, "")
			fmt.Println("  ✅ Test PCR successfully revoked")
		}

		// Step 3: Test activate/deactivate cycle with valid PCR
		fmt.Println("\n  Step 3: Test activate/deactivate cycle")

		// Deactivate our real TEE
		txHash, err = callDeactivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
			isActive, _ := callIsActive(registeredTEEId)
			results.Add("TEE deactivated", !isActive, "")
		}

		// Reactivate - should succeed since TEE's PCR is still valid
		txHash, err = callActivateTEE(account, registeredTEEId)
		if err == nil {
			waitForTx(txHash)
			isActive, _ := callIsActive(registeredTEEId)
			results.Add("TEE reactivated with valid PCR", isActive, "")
			fmt.Println("  ✅ Reactivation successful (PCR valid)")
		} else {
			results.Add("Reactivate TEE", false, err.Error())
		}

		// Step 4: Test grace period functionality
		fmt.Println("\n  Step 4: Test PCR grace period")

		pcrV1_0 := make([]byte, 48)
		pcrV1_1 := make([]byte, 48)
		pcrV1_2 := make([]byte, 48)
		rand.Read(pcrV1_0)
		rand.Read(pcrV1_1)
		rand.Read(pcrV1_2)

		pcrV2_0 := make([]byte, 48)
		pcrV2_1 := make([]byte, 48)
		pcrV2_2 := make([]byte, 48)
		rand.Read(pcrV2_0)
		rand.Read(pcrV2_1)
		rand.Read(pcrV2_2)

		pcrV1Hash, _ := callComputePCRHash(pcrV1_0, pcrV1_1, pcrV1_2)
		pcrV2Hash, _ := callComputePCRHash(pcrV2_0, pcrV2_1, pcrV2_2)

		fmt.Printf("  📊 PCR v1 Hash: 0x%s\n", hex.EncodeToString(pcrV1Hash[:]))
		fmt.Printf("  📊 PCR v2 Hash: 0x%s\n", hex.EncodeToString(pcrV2Hash[:]))

		// Approve v1 and v2
		txHash, _ = callApprovePCR(account, pcrV1_0, pcrV1_1, pcrV1_2, "v1-grace", 0)
		waitForTx(txHash)
		txHash, _ = callApprovePCR(account, pcrV2_0, pcrV2_1, pcrV2_2, "v2-grace", 0)
		waitForTx(txHash)

		// Revoke v1 with 1 hour grace period
		txHash, err = callRevokePCR(account, pcrV1Hash, 3600)
		if err == nil {
			waitForTx(txHash)

			// Both should be valid during grace period
			v1Valid, _ := callIsPCRApproved(pcrV1Hash)
			v2Valid, _ := callIsPCRApproved(pcrV2Hash)

			bothValid := v1Valid && v2Valid
			results.Add("Both PCRs valid during grace period", bothValid,
				fmt.Sprintf("v1=%v, v2=%v", v1Valid, v2Valid))

			if bothValid {
				fmt.Println("  ✅ Grace period working: both v1 and v2 valid")
			} else {
				fmt.Printf("  ⚠️  Grace period issue: v1=%v, v2=%v\n", v1Valid, v2Valid)
			}
		} else {
			results.Add("Revoke PCR with grace period", false, err.Error())
		}

		// Step 5: Test duplicate PCR prevention
		fmt.Println("\n  Step 5: Test duplicate PCR prevention")

		dupPCR0 := make([]byte, 48)
		dupPCR1 := make([]byte, 48)
		dupPCR2 := make([]byte, 48)
		rand.Read(dupPCR0)
		rand.Read(dupPCR1)
		rand.Read(dupPCR2)

		// Approve once
		txHash, err = callApprovePCR(account, dupPCR0, dupPCR1, dupPCR2, "dup-test-1", 0)
		if err == nil {
			waitForTx(txHash)
		}

		// Try to approve same PCRs again - should fail
		txHash, err = callApprovePCR(account, dupPCR0, dupPCR1, dupPCR2, "dup-test-2", 0)
		if err != nil {
			results.Add("Duplicate PCR registration prevented", true, "")
			fmt.Println("  ✅ Duplicate PCR rejected as expected")
		} else {
			// If tx was sent, wait for it - it should revert
			success := waitForTx(txHash)
			results.Add("Duplicate PCR registration prevented", !success, "")
			if !success {
				fmt.Println("  ✅ Duplicate PCR transaction reverted")
			} else {
				fmt.Println("  ⚠️  Duplicate PCR was accepted (should have failed)")
			}
		}
	} else {
		fmt.Println("  ⚠️  Skipping PCR revocation tests (no registered TEE)")
	}
	// SECTION 9: Utility Functions
	fmt.Println("\n------------------------------------------")
	fmt.Println("SECTION 9: Utility Functions")
	fmt.Println("------------------------------------------")

	computedId, err := callComputeTEEId(testPubKeyDER)
	expectedId := crypto.Keccak256Hash(testPubKeyDER)
	results.Add("computeTEEId matches keccak256", err == nil && computedId == expectedId, "")

	computedHash, err := callComputeMessageHash(inputHash, outputHash, timestamp)
	results.Add("computeMessageHash returns hash", err == nil && computedHash != [32]byte{}, "")

	// Summary
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
// CONTRACT DEPLOYMENT
// ============================================================================

func deployOrFindRegistry(account string) (string, error) {
	storedAddress := os.Getenv("TEE_REGISTRY_ADDRESS")
	if storedAddress != "" {
		code, err := getCode(storedAddress)
		if err == nil && len(code) > 0 {
			fmt.Printf("  ✅ Found existing TEERegistry at %s\n", storedAddress)
			return storedAddress, nil
		}
	}

	if TEE_REGISTRY_BYTECODE == "" || TEE_REGISTRY_BYTECODE == "PASTE_YOUR_NEW_BYTECODE_HERE" {
		return "", fmt.Errorf("TEE_REGISTRY_BYTECODE is empty. Please compile TEERegistry.sol and paste bytecode")
	}

	fmt.Println("  🚀 Deploying new TEERegistry...")

	bytecode, err := hex.DecodeString(strings.TrimPrefix(TEE_REGISTRY_BYTECODE, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid bytecode: %v", err)
	}

	txHash, err := deployContract(account, bytecode)
	if err != nil {
		return "", fmt.Errorf("deploy failed: %v", err)
	}

	fmt.Printf("  📤 Deploy tx: %s\n", txHash)

	address, err := waitForContractAddress(txHash)
	if err != nil {
		return "", fmt.Errorf("failed to get contract address: %v", err)
	}

	fmt.Printf("  ✅ TEERegistry deployed at %s\n", address)
	fmt.Println("  💡 Set TEE_REGISTRY_ADDRESS env var to skip deployment next time")

	return address, nil
}

func getCode(address string) ([]byte, error) {
	params := []interface{}{address, "latest"}
	resp, err := rpcCall("eth_getCode", params)
	if err != nil {
		return nil, err
	}
	var result struct{ Result string }
	json.Unmarshal(resp, &result)
	if len(result.Result) <= 2 {
		return nil, nil
	}
	return hex.DecodeString(result.Result[2:])
}

func deployContract(from string, bytecode []byte) (string, error) {
	params := []interface{}{
		map[string]string{
			"from": from,
			"gas":  "0x980000",
			"data": "0x" + hex.EncodeToString(bytecode),
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
	json.Unmarshal(resp, &result)
	if result.Error != nil {
		return "", fmt.Errorf("%s", result.Error.Message)
	}
	return result.Result, nil
}

func waitForContractAddress(txHash string) (string, error) {
	for i := 0; i < 30; i++ {
		resp, _ := rpcCall("eth_getTransactionReceipt", []string{txHash})
		var result struct {
			Result *struct {
				Status          string
				ContractAddress string
			}
		}
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			if result.Result.Status != "0x1" {
				return "", fmt.Errorf("deployment failed")
			}
			return result.Result.ContractAddress, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("timeout waiting for receipt")
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

	url := fmt.Sprintf("https://%s/signing-key", host)
	fmt.Printf("    🔍 Fetching signing key from: %s\n", url)

	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("    ❌ HTTP request failed: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Printf("    📋 HTTP Status: %d\n", resp.StatusCode)
	fmt.Printf("    📋 Content-Type: %s\n", resp.Header.Get("Content-Type"))

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("    ❌ Failed to read body: %v\n", err)
		return nil, err
	}
	fmt.Printf("    📋 Raw response (%d bytes): %s\n", len(rawBody), string(rawBody))

	var attestResp AttestationResponse
	if err := json.Unmarshal(rawBody, &attestResp); err != nil {
		fmt.Printf("    ❌ JSON parse failed: %v\n", err)
		return nil, fmt.Errorf("json unmarshal failed: %v", err)
	}
	fmt.Printf("    📋 public_key field (%d chars):\n%s\n", len(attestResp.PublicKey), attestResp.PublicKey)

	// Fix literal \n in JSON string vs real newlines
	pemStr := strings.ReplaceAll(attestResp.PublicKey, `\n`, "\n")

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		// Last resort: try raw body as PEM directly
		fmt.Printf("    ⚠️  JSON PEM decode failed, trying raw body as PEM\n")
		block, _ = pem.Decode(rawBody)
	}
	if block == nil {
		fmt.Printf("    ❌ PEM decode returned nil\n")
		return nil, fmt.Errorf("failed to decode PEM")
	}

	fmt.Printf("    ✅ PEM block type: %s, DER size: %d bytes\n", block.Type, len(block.Bytes))
	return block.Bytes, nil
}

func fetchTLSCertificate(host, port string) ([]byte, error) {
	addr := host + ":" + port
	fmt.Printf("    🔌 Connecting to %s via TLS...\n", addr)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: true, // enclave uses self-signed cert
	})
	if err != nil {
		fmt.Printf("    ❌ TLS Dial failed: %v\n", err)
		return nil, err
	}
	defer conn.Close()
	fmt.Printf("    ✅ TLS connection established\n")

	state := conn.ConnectionState()
	fmt.Printf("    📋 TLS version: 0x%x\n", state.Version)
	fmt.Printf("    📋 Certificates in chain: %d\n", len(state.PeerCertificates))

	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("server presented no certificates")
	}

	cert := state.PeerCertificates[0]
	fmt.Printf("    📄 Subject:   %s\n", cert.Subject)
	fmt.Printf("    📄 Issuer:    %s\n", cert.Issuer)
	fmt.Printf("    📄 NotBefore: %s\n", cert.NotBefore)
	fmt.Printf("    📄 NotAfter:  %s\n", cert.NotAfter)
	fmt.Printf("    📄 PublicKey: %T\n", cert.PublicKey)
	fmt.Printf("    📄 Raw size:  %d bytes\n", len(cert.Raw))
	fmt.Printf("    📄 SHA256:    %x\n", sha256.Sum256(cert.Raw))

	// Sanity check — parse back to confirm valid DER
	if _, err := x509.ParseCertificate(cert.Raw); err != nil {
		fmt.Printf("    ❌ Failed to parse cert back: %v\n", err)
		return nil, fmt.Errorf("invalid certificate: %v", err)
	}
	fmt.Printf("    ✅ Certificate valid and parsed successfully\n")

	return cert.Raw, nil
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

func callRevokePCR(from string, pcrHash [32]byte, gracePeriod uint64) (string, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: uint256Type}}
	encoded, _ := args.Pack(pcrHash, new(big.Int).SetUint64(gracePeriod))
	return sendTx(from, append(SEL_REVOKE_PCR, encoded...))
}

// ============================================================================
// AWS NITRO ROOT CERTIFICATE
// ============================================================================

func getAWSNitroRootCert() ([]byte, error) {
	const awsNitroRootPEM = `-----BEGIN CERTIFICATE-----
MIICETCCAZagAwIBAgIRAPkxdWgbkK/hHUbMtOTn+FYwCgYIKoZIzj0EAwMwSTEL
MAkGA1UEBhMCVVMxDzANBgNVBAoMBkFtYXpvbjEMMAoGA1UECwwDQVdTMRswGQYD
VQQDDBJhd3Mubml0cm8tZW5jbGF2ZXMwHhcNMTkxMDI4MTMyODA1WhcNNDkxMDI4
MTQyODA1WjBJMQswCQYDVQQGEwJVUzEPMA0GA1UECgwGQW1hem9uMQwwCgYDVQQL
DANBV1MxGzAZBgNVBAMMEmF3cy5uaXRyby1lbmNsYXZlczB2MBAGByqGSM49AgEG
BSuBBAAiA2IABPwCVOumCMHzaHDimtqQvkY4MpJzbolL//Zy2YlES1BR5TSksfbb
48C8WBoyt7F2Bw7eEtaaP+ohG2bnUs990d0JX28TcPQXCEPZ3BABIeTPYwEoCWZE
h8l5YoQwTcU/9KNCMEAwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUkCW1DdkF
R+eWw5b6cp3PmanfS5YwDgYDVR0PAQH/BAQDAgGGMAoGCCqGSM49BAMDA2kAMGYC
MQCjfy+Rocm9Xue4YnwWmNJVA44fA0P5W2OpYow9OYCVRaEevL8uO1XYru5xtMPW
rfMCMQCi85sWBbJwKKXdS6BptQFuZbT73o/gBh1qUxl/nNr12UO8Yfwr6wPLb+6N
IwLz3/Y=
-----END CERTIFICATE-----`

	block, _ := pem.Decode([]byte(awsNitroRootPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode AWS Nitro root cert PEM")
	}

	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("invalid AWS Nitro root cert: %v", err)
	}

	fmt.Printf("    ✅ AWS Nitro root cert: %d bytes DER\n", len(block.Bytes))
	return []byte(awsNitroRootPEM), nil
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

func computeMessageHash(inputHash, outputHash [32]byte, timestamp *big.Int) [32]byte {
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])
	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)
	return crypto.Keccak256Hash(data)
}

// ============================================================================
// CONTRACT CALLS - AccessControl
// ============================================================================

func callHasRole(role [32]byte, account string) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	addrType, _ := abi.NewType("address", "", nil)
	args := abi.Arguments{{Type: bytes32Type}, {Type: addrType}}
	encoded, _ := args.Pack(role, common.HexToAddress(account))
	result, err := ethCall(append(SEL_HAS_ROLE, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
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
// CONTRACT CALLS - TEE Types
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
// CONTRACT CALLS - PCR Management
// ============================================================================

func callApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string, teeType uint8) (string, error) {
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"},
		{Name: "pcr1", Type: "bytes"},
		{Name: "pcr2", Type: "bytes"},
	})
	stringType, _ := abi.NewType("string", "", nil)
	uint8Type, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{{Type: tupleType}, {Type: stringType}, {Type: uint8Type}}
	pcrs := struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}
	encoded, _ := args.Pack(pcrs, version, teeType)
	return sendTx(from, append(SEL_APPROVE_PCR, encoded...))
}

func callIsPCRApproved(pcrHash [32]byte) (bool, error) {
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	args := abi.Arguments{{Type: bytes32Type}}
	encoded, _ := args.Pack(pcrHash)
	result, err := ethCall(append(SEL_IS_PCR_APPROVED, encoded...))
	if err != nil || len(result) < 32 {
		return false, err
	}
	return result[31] == 1, nil
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
// CONTRACT CALLS - AWS Root Certificate
// ============================================================================

func callSetAWSRootCert(from string, certDER []byte) (string, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	args := abi.Arguments{{Type: bytesType}}
	encoded, _ := args.Pack(certDER)
	return sendTx(from, append(SEL_SET_AWS_ROOT_CERT, encoded...))
}

// ============================================================================
// CONTRACT CALLS - TEE Registration
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

// ============================================================================
// CONTRACT CALLS - Queries
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
// CONTRACT CALLS - Utility
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
		map[string]string{"to": TEE_REGISTRY_ADDRESS, "data": "0x" + hex.EncodeToString(data)},
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
		return nil, fmt.Errorf("%s", result.Error.Message)
	}
	if len(result.Result) > 2 {
		return hex.DecodeString(result.Result[2:])
	}
	return nil, nil
}

func sendTx(from string, data []byte) (string, error) {
	fmt.Printf("    🔍 sendTx: from=%s, to=%s, data=%d bytes\n", from, TEE_REGISTRY_ADDRESS, len(data))

	// First simulate with eth_call
	callParams := []interface{}{
		map[string]string{
			"from": from,
			"to":   TEE_REGISTRY_ADDRESS,
			"gas":  "0x500000",
			"data": "0x" + hex.EncodeToString(data),
		},
		"latest",
	}
	callResp, err := rpcCall("eth_call", callParams)
	if err != nil {
		return "", fmt.Errorf("eth_call RPC error: %v", err)
	}

	var callResult struct {
		Result string
		Error  *struct {
			Message string
			Data    string
		}
	}
	json.Unmarshal(callResp, &callResult)
	if callResult.Error != nil {
		return "", fmt.Errorf("simulation failed: %s (data: %s)", callResult.Error.Message, callResult.Error.Data)
	}

	// Check for revert
	if len(callResult.Result) > 10 {
		selector := callResult.Result[2:10]
		if selector == "08c379a0" {
			revertData, _ := hex.DecodeString(callResult.Result[10:])
			if len(revertData) >= 64 {
				length := new(big.Int).SetBytes(revertData[32:64]).Uint64()
				if uint64(len(revertData)) >= 64+length {
					errorMsg := string(revertData[64 : 64+length])
					return "", fmt.Errorf("revert: %s", errorMsg)
				}
			}
		}
	}

	// Send actual transaction
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
		return "", fmt.Errorf("eth_sendTransaction RPC error: %v", err)
	}

	var result struct {
		Result string
		Error  *struct{ Message string }
	}
	json.Unmarshal(resp, &result)
	if result.Error != nil {
		return "", fmt.Errorf("tx error: %s", result.Error.Message)
	}
	if result.Result == "" {
		return "", fmt.Errorf("empty tx hash in response: %s", string(resp))
	}
	return result.Result, nil
}

func waitForTx(txHash string) bool {
	for i := 0; i < 15; i++ {
		resp, _ := rpcCall("eth_getTransactionReceipt", []string{txHash})
		var result struct {
			Result *struct {
				Status  string
				GasUsed string
			}
		}
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			if result.Result.Status != "0x1" {
				fmt.Printf("    ⚠️  Tx reverted. GasUsed: %s\n", result.Result.GasUsed)
				return false
			}
			return true
		}
		time.Sleep(time.Second)
	}
	fmt.Println("    ⚠️  Timeout waiting for tx receipt")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractPCRsFromAttestation(attestationBytes []byte) (map[int][]byte, error) {
	// COSE Sign1 structure: [protected, unprotected, payload, signature]
	var cose []cbor.RawMessage
	if err := cbor.Unmarshal(attestationBytes, &cose); err != nil {
		return nil, fmt.Errorf("COSE unmarshal: %v", err)
	}
	if len(cose) != 4 {
		return nil, fmt.Errorf("expected 4 COSE elements, got %d", len(cose))
	}

	var payload []byte
	if err := cbor.Unmarshal(cose[2], &payload); err != nil {
		return nil, fmt.Errorf("payload unmarshal: %v", err)
	}

	var doc struct {
		PCRs map[int][]byte `cbor:"pcrs"`
	}
	if err := cbor.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("doc unmarshal: %v", err)
	}

	return doc.PCRs, nil
}

func getEnvOrDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}