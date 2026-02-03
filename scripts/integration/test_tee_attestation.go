package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	RPC_URL     = "http://localhost:8545"
	TEE_ADDRESS = "0x0000000000000000000000000000000000000900"
)

// Real attestation from Kyle's TEE node
const REAL_ATTESTATION_BASE64 = `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`

// Expected PCR values (truncated to 32 bytes)
var (
	PCR0_HEX = "34889f7be57af8d1fcab6564bcd933a5bac26e2f98074a1bc6b4fcd27e57aa65"
	PCR1_HEX = "4b4d5b3661b3efc12920900c80e126e4ce783c522de6c02a2a5bf7af3a2b9327"
	PCR2_HEX = "4c8cee64439b2769a2043ea01ec1d27d8e10dff249954599ab09a8512d698361"
)

// Selectors (computed)
var (
	SELECTOR_VERIFY_ATTESTATION   = crypto.Keccak256([]byte("verifyAttestation(bytes,(bytes32,bytes32,bytes32))"))[:4]
	SELECTOR_REGISTER_ATTESTATION = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,(bytes32,bytes32,bytes32))"))[:4]
)

func main() {
	fmt.Println("==========================================")
	fmt.Println("  TEE Attestation Registration Test")
	fmt.Println("==========================================")
	fmt.Println()

	fmt.Printf("Selectors:\n")
	fmt.Printf("  verifyAttestation:           0x%x\n", SELECTOR_VERIFY_ATTESTATION)
	fmt.Printf("  registerTEEWithAttestation:  0x%x\n\n", SELECTOR_REGISTER_ATTESTATION)

	account, err := getFirstAccount()
	if err != nil {
		fmt.Printf("❌ Failed to get account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📍 Account: %s\n\n", account)

	// Decode attestation
	fmt.Println("------------------------------------------")
	fmt.Println("Step 1: Decode Attestation Document")
	fmt.Println("------------------------------------------")

	attestationBytes, err := base64.StdEncoding.DecodeString(REAL_ATTESTATION_BASE64)
	if err != nil {
		fmt.Printf("❌ Failed to decode base64: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Attestation size: %d bytes\n\n", len(attestationBytes))

	// Decode PCRs
	pcr0, _ := hex.DecodeString(PCR0_HEX)
	pcr1, _ := hex.DecodeString(PCR1_HEX)
	pcr2, _ := hex.DecodeString(PCR2_HEX)

	fmt.Println("------------------------------------------")
	fmt.Println("Step 2: Expected PCR Values")
	fmt.Println("------------------------------------------")
	fmt.Printf("   PCR0: 0x%s\n", PCR0_HEX)
	fmt.Printf("   PCR1: 0x%s\n", PCR1_HEX)
	fmt.Printf("   PCR2: 0x%s\n\n", PCR2_HEX)

	// Test verifyAttestation (view function)
	fmt.Println("------------------------------------------")
	fmt.Println("Step 3: Verify Attestation (view)")
	fmt.Println("------------------------------------------")

	valid, publicKey, err := callVerifyAttestation(attestationBytes, pcr0, pcr1, pcr2)
	if err != nil {
		fmt.Printf("⚠️  verifyAttestation error: %v\n", err)
		fmt.Println("   Note: Attestation certificates expire quickly (~3 hours)")
		fmt.Println("   Get fresh attestation: curl -k https://13.59.207.188/enclave/attestation")
	} else if valid {
		fmt.Printf("✅ Attestation verified!\n")
		fmt.Printf("   Public key length: %d bytes\n\n", len(publicKey))

		fmt.Println("------------------------------------------")
		fmt.Println("Step 4: Register TEE with Attestation")
		fmt.Println("------------------------------------------")

		txHash, err := callRegisterTEEWithAttestation(account, attestationBytes, pcr0, pcr1, pcr2)
		if err != nil {
			fmt.Printf("❌ Registration failed: %v\n", err)
		} else {
			fmt.Printf("📤 Transaction sent: %s\n", txHash)
			fmt.Println("   Waiting for confirmation...")

			success, _ := waitForTx(txHash, 15)
			if success {
				fmt.Printf("✅ TEE registered with real attestation!\n")
			} else {
				fmt.Printf("❌ Transaction reverted\n")
			}
		}
	} else {
		fmt.Printf("❌ Attestation verification returned false\n")
	}

	fmt.Println("\n==========================================")
	fmt.Println("  Test Complete")
	fmt.Println("==========================================")
}

func callVerifyAttestation(attestation, pcr0, pcr1, pcr2 []byte) (bool, []byte, error) {
	calldata, err := encodeAttestationCall(SELECTOR_VERIFY_ATTESTATION, attestation, pcr0, pcr1, pcr2)
	if err != nil {
		return false, nil, err
	}

	result, err := ethCall(calldata)
	if err != nil {
		return false, nil, err
	}

	if len(result) < 64 {
		return false, nil, fmt.Errorf("result too short: %d bytes", len(result))
	}

	valid := result[31] == 1

	// Decode public key from bytes
	offset := new(big.Int).SetBytes(result[32:64]).Uint64()
	if offset+32 > uint64(len(result)) {
		return valid, nil, nil
	}
	length := new(big.Int).SetBytes(result[offset : offset+32]).Uint64()
	if offset+32+length > uint64(len(result)) {
		return valid, nil, nil
	}
	publicKey := result[offset+32 : offset+32+length]

	return valid, publicKey, nil
}

func callRegisterTEEWithAttestation(from string, attestation, pcr0, pcr1, pcr2 []byte) (string, error) {
	calldata, err := encodeAttestationCall(SELECTOR_REGISTER_ATTESTATION, attestation, pcr0, pcr1, pcr2)
	if err != nil {
		return "", err
	}
	return sendTx(from, calldata)
}

func encodeAttestationCall(selector []byte, attestation, pcr0, pcr1, pcr2 []byte) ([]byte, error) {
	bytesType, _ := abi.NewType("bytes", "", nil)
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes32"},
		{Name: "pcr1", Type: "bytes32"},
		{Name: "pcr2", Type: "bytes32"},
	})

	args := abi.Arguments{
		{Type: bytesType},
		{Type: tupleType},
	}

	var pcr0Arr, pcr1Arr, pcr2Arr [32]byte
	copy(pcr0Arr[:], pcr0)
	copy(pcr1Arr[:], pcr1)
	copy(pcr2Arr[:], pcr2)

	pcrs := struct {
		Pcr0 [32]byte
		Pcr1 [32]byte
		Pcr2 [32]byte
	}{pcr0Arr, pcr1Arr, pcr2Arr}

	encoded, err := args.Pack(attestation, pcrs)
	if err != nil {
		return nil, fmt.Errorf("ABI encode failed: %w", err)
	}

	return append(selector, encoded...), nil
}

// RPC helpers
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

func waitForTx(txHash string, timeoutSec int) (bool, error) {
	for i := 0; i < timeoutSec; i++ {
		resp, _ := rpcCall("eth_getTransactionReceipt", []string{txHash})
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
	return false, fmt.Errorf("timeout")
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
