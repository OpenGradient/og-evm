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
