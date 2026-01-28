package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	RPC_URL     = "http://localhost:8545"
	TEE_ADDRESS = "0x0000000000000000000000000000000000000900"
)

// Selectors
var (
	SELECTOR_IS_ACTIVE         = gethcrypto.Keccak256([]byte("isActive(bytes32)"))[:4]
	SELECTOR_REGISTER_TEE      = gethcrypto.Keccak256([]byte("registerTEE(bytes,(bytes32,bytes32,bytes32))"))[:4]
	SELECTOR_VERIFY_SIGNATURE  = gethcrypto.Keccak256([]byte("verifySignature(bytes32,bytes32,bytes32,uint256,bytes)"))[:4]
	SELECTOR_VERIFY_SETTLEMENT = gethcrypto.Keccak256([]byte("verifySettlement(bytes32,bytes32,bytes32,uint256,bytes)"))[:4]
	SELECTOR_COMPUTE_TEE_ID    = gethcrypto.Keccak256([]byte("computeTEEId(bytes)"))[:4]
)

func main() {
	fmt.Println("==========================================")
	fmt.Println("  TEE Precompile - Full Workflow Test")
	fmt.Println("==========================================")
	fmt.Println()

	// Step 0: Get account
	account, err := getFirstAccount()
	if err != nil {
		fmt.Printf("❌ Failed to get account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📍 Using account: %s\n\n", account)

	// Step 1: Generate TEE identity
	fmt.Println("------------------------------------------")
	fmt.Println("Step 1: Generate TEE Identity")
	fmt.Println("------------------------------------------")

	privateKey, publicKeyDER, teeId := generateTEEIdentity()
	fmt.Printf("✅ Generated RSA-2048 key pair\n")
	fmt.Printf("   Public key: %d bytes (DER)\n", len(publicKeyDER))
	fmt.Printf("   TEE ID:     0x%s\n\n", hex.EncodeToString(teeId[:]))

	// Step 2: Check TEE is NOT active before registration
	fmt.Println("------------------------------------------")
	fmt.Println("Step 2: Verify TEE NOT Active (before)")
	fmt.Println("------------------------------------------")

	active, err := isActive(teeId)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		os.Exit(1)
	}
	if !active {
		fmt.Printf("✅ TEE is not active (expected)\n\n")
	} else {
		fmt.Printf("⚠️  TEE already active, skipping registration\n\n")
	}

	// Step 3: Register TEE
	if !active {
		fmt.Println("------------------------------------------")
		fmt.Println("Step 3: Register TEE")
		fmt.Println("------------------------------------------")

		pcrs := generatePCRs()
		fmt.Printf("   PCR0: 0x%s...\n", hex.EncodeToString(pcrs.Pcr0[:8]))
		fmt.Printf("   PCR1: 0x%s...\n", hex.EncodeToString(pcrs.Pcr1[:8]))
		fmt.Printf("   PCR2: 0x%s...\n", hex.EncodeToString(pcrs.Pcr2[:8]))

		txHash, err := registerTEE(account, publicKeyDER, pcrs)
		if err != nil {
			fmt.Printf("❌ Failed to register: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("📤 Transaction sent: %s\n", txHash)

		fmt.Printf("   Waiting for confirmation...\n")
		success, err := waitForTx(txHash, 15)
		if err != nil {
			fmt.Printf("❌ Failed to confirm: %v\n", err)
			os.Exit(1)
		}
		if success {
			fmt.Printf("✅ TEE registered successfully!\n\n")
		} else {
			fmt.Printf("❌ Transaction reverted\n")
			os.Exit(1)
		}
	}

	// Step 4: Verify TEE IS active after registration
	fmt.Println("------------------------------------------")
	fmt.Println("Step 4: Verify TEE IS Active (after)")
	fmt.Println("------------------------------------------")

	active, err = isActive(teeId)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		os.Exit(1)
	}
	if active {
		fmt.Printf("✅ TEE is now active!\n\n")
	} else {
		fmt.Printf("❌ TEE not active (unexpected)\n")
		os.Exit(1)
	}

	// Step 5: Simulate inference and sign
	fmt.Println("------------------------------------------")
	fmt.Println("Step 5: Simulate Inference + Sign")
	fmt.Println("------------------------------------------")

	// Simulate request/response
	request := []byte(`{"prompt": "What is the capital of France?"}`)
	response := []byte(`{"answer": "The capital of France is Paris."}`)
	timestamp := big.NewInt(time.Now().Unix())

	inputHash := gethcrypto.Keccak256Hash(request)
	outputHash := gethcrypto.Keccak256Hash(response)

	fmt.Printf("   Input hash:  0x%s...\n", hex.EncodeToString(inputHash[:8]))
	fmt.Printf("   Output hash: 0x%s...\n", hex.EncodeToString(outputHash[:8]))
	fmt.Printf("   Timestamp:   %d\n", timestamp.Int64())

	// Compute message hash (matches precompile)
	messageHash := computeMessageHash(inputHash, outputHash, timestamp)
	fmt.Printf("   Message hash: 0x%s...\n", hex.EncodeToString(messageHash[:8]))

	// Sign with TEE private key
	signature, err := signWithRSAPSS(privateKey, messageHash[:])
	if err != nil {
		fmt.Printf("❌ Failed to sign: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   Signature:   0x%s... (%d bytes)\n\n", hex.EncodeToString(signature[:8]), len(signature))

	// Step 6: Verify signature on-chain (view function)
	fmt.Println("------------------------------------------")
	fmt.Println("Step 6: Verify Signature (view)")
	fmt.Println("------------------------------------------")

	valid, err := verifySignature(teeId, inputHash, outputHash, timestamp, signature)
	if err != nil {
		fmt.Printf("❌ Verification error: %v\n", err)
		os.Exit(1)
	}
	if valid {
		fmt.Printf("✅ Signature verified on-chain!\n\n")
	} else {
		fmt.Printf("❌ Signature verification failed\n")
		os.Exit(1)
	}

	// Step 7: Test with tampered data (should fail)
	fmt.Println("------------------------------------------")
	fmt.Println("Step 7: Test Tampered Data (should fail)")
	fmt.Println("------------------------------------------")

	tamperedOutput := gethcrypto.Keccak256Hash([]byte("tampered response"))
	valid, _ = verifySignature(teeId, inputHash, tamperedOutput, timestamp, signature)
	if !valid {
		fmt.Printf("✅ Tampered data correctly rejected!\n\n")
	} else {
		fmt.Printf("❌ Tampered data was accepted (security issue!)\n")
		os.Exit(1)
	}

	// Step 8: Settlement verification (state-changing)
	fmt.Println("------------------------------------------")
	fmt.Println("Step 8: Settlement Verification")
	fmt.Println("------------------------------------------")

	txHash, err := verifySettlement(account, teeId, inputHash, outputHash, timestamp, signature)
	if err != nil {
		fmt.Printf("❌ Settlement error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📤 Settlement tx: %s\n", txHash)

	success, err := waitForTx(txHash, 15)
	if err != nil {
		fmt.Printf("❌ Failed to confirm: %v\n", err)
		os.Exit(1)
	}
	if success {
		fmt.Printf("✅ Settlement recorded on-chain!\n\n")
	} else {
		fmt.Printf("❌ Settlement transaction reverted\n")
		os.Exit(1)
	}

	// Done!
	fmt.Println("==========================================")
	fmt.Println("  ✅ Full Workflow Complete!")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  TEE ID:     0x%s\n", hex.EncodeToString(teeId[:]))
	fmt.Printf("  Account:    %s\n", account)
	fmt.Println("  Status:     Registered & Active")
	fmt.Println("  Signature:  Verified")
	fmt.Println("  Settlement: Recorded")
}

// ==================== TEE IDENTITY ====================

type PCRs struct {
	Pcr0 [32]byte
	Pcr1 [32]byte
	Pcr2 [32]byte
}

func generateTEEIdentity() (*rsa.PrivateKey, []byte, [32]byte) {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	publicKeyDER, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	teeId := gethcrypto.Keccak256Hash(publicKeyDER)
	return privateKey, publicKeyDER, teeId
}

func generatePCRs() PCRs {
	return PCRs{
		Pcr0: gethcrypto.Keccak256Hash([]byte("enclave-image-v1.0")),
		Pcr1: gethcrypto.Keccak256Hash([]byte("linux-kernel-5.15")),
		Pcr2: gethcrypto.Keccak256Hash([]byte("inference-app-v2.0")),
	}
}

// ==================== SIGNATURE ====================

func computeMessageHash(inputHash, outputHash common.Hash, timestamp *big.Int) common.Hash {
	// keccak256(abi.encodePacked(inputHash, outputHash, timestamp))
	data := make([]byte, 96)
	copy(data[0:32], inputHash[:])
	copy(data[32:64], outputHash[:])
	timestampBytes := timestamp.Bytes()
	copy(data[96-len(timestampBytes):96], timestampBytes)
	return gethcrypto.Keccak256Hash(data)
}

func signWithRSAPSS(privateKey *rsa.PrivateKey, messageHash []byte) ([]byte, error) {
	return rsa.SignPSS(
		rand.Reader,
		privateKey,
		crypto.SHA256,
		messageHash,
		&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash},
	)
}

// ==================== PRECOMPILE CALLS ====================

func isActive(teeId [32]byte) (bool, error) {
	data := append(SELECTOR_IS_ACTIVE, teeId[:]...)
	result, err := ethCall(data)
	if err != nil {
		return false, err
	}
	if len(result) >= 32 {
		return result[31] == 1, nil
	}
	return false, nil
}

func registerTEE(from string, publicKey []byte, pcrs PCRs) (string, error) {
	// ABI encode: bytes publicKey, tuple(bytes32,bytes32,bytes32) pcrs
	bytesType, _ := abi.NewType("bytes", "", nil)
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes32"},
		{Name: "pcr1", Type: "bytes32"},
		{Name: "pcr2", Type: "bytes32"},
	})

	args := abi.Arguments{{Type: bytesType}, {Type: tupleType}}
	encoded, err := args.Pack(publicKey, pcrs)
	if err != nil {
		return "", fmt.Errorf("abi encode: %w", err)
	}

	calldata := append(SELECTOR_REGISTER_TEE, encoded...)
	return sendTx(from, calldata)
}

func verifySignature(teeId, inputHash, outputHash [32]byte, timestamp *big.Int, signature []byte) (bool, error) {
	// ABI encode arguments
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
		return false, fmt.Errorf("abi encode: %w", err)
	}

	calldata := append(SELECTOR_VERIFY_SIGNATURE, encoded...)
	result, err := ethCall(calldata)
	if err != nil {
		return false, err
	}

	if len(result) >= 32 {
		return result[31] == 1, nil
	}
	return false, nil
}

func verifySettlement(from string, teeId, inputHash, outputHash [32]byte, timestamp *big.Int, signature []byte) (string, error) {
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
		return "", fmt.Errorf("abi encode: %w", err)
	}

	calldata := append(SELECTOR_VERIFY_SETTLEMENT, encoded...)
	return sendTx(from, calldata)
}

// ==================== RPC HELPERS ====================

func getFirstAccount() (string, error) {
	resp, err := rpcCall("eth_accounts", []interface{}{})
	if err != nil {
		return "", err
	}

	var result struct {
		Result []string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(resp, &result)

	if result.Error != nil {
		return "", fmt.Errorf(result.Error.Message)
	}
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no accounts available")
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
			"gas":  "0x200000",
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

func waitForTx(txHash string, timeoutSec int) (bool, error) {
	for i := 0; i < timeoutSec; i++ {
		resp, err := rpcCall("eth_getTransactionReceipt", []string{txHash})
		if err != nil {
			return false, err
		}

		var result struct {
			Result *struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		json.Unmarshal(resp, &result)

		if result.Result != nil {
			return result.Result.Status == "0x1", nil
		}

		time.Sleep(time.Second)
	}
	return false, fmt.Errorf("timeout waiting for tx")
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
