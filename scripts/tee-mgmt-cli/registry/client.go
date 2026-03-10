package registry

import (
	"bytes"
	"context"
	"crypto/rand"
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

// Role constants
var (
	DefaultAdminRole = [32]byte{}
	TEEOperatorRole  = crypto.Keccak256Hash([]byte("TEE_OPERATOR"))
)

// Method selectors
var (
	selGrantRole        = crypto.Keccak256([]byte("grantRole(bytes32,address)"))[:4]
	selRevokeRole       = crypto.Keccak256([]byte("revokeRole(bytes32,address)"))[:4]
	selHasRole          = crypto.Keccak256([]byte("hasRole(bytes32,address)"))[:4]
	selAddTEEType       = crypto.Keccak256([]byte("addTEEType(uint8,string)"))[:4]
	selDeactivateTEETyp = crypto.Keccak256([]byte("deactivateTEEType(uint8)"))[:4]
	selIsValidType      = crypto.Keccak256([]byte("isValidTEEType(uint8)"))[:4]
	selApprovePCR       = crypto.Keccak256([]byte("approvePCR((bytes,bytes,bytes),string,uint8)"))[:4]
	selRevokePCR        = crypto.Keccak256([]byte("revokePCR(bytes32,uint8,uint256)"))[:4]
	selIsPCRApproved    = crypto.Keccak256([]byte("isPCRApproved(uint8,bytes32)"))[:4]
	selComputePCRHash   = crypto.Keccak256([]byte("computePCRHash((bytes,bytes,bytes))"))[:4]
	selGetActivePCRs    = crypto.Keccak256([]byte("getActivePCRs()"))[:4]
	selSetAWSRootCert   = crypto.Keccak256([]byte("setAWSRootCertificate(bytes)"))[:4]
	selRegisterTEE      = crypto.Keccak256([]byte("registerTEEWithAttestation(bytes,bytes,bytes,address,string,uint8)"))[:4]
	selDeactivateTEE    = crypto.Keccak256([]byte("deactivateTEE(bytes32)"))[:4]
	selActivateTEE      = crypto.Keccak256([]byte("activateTEE(bytes32)"))[:4]
	selRemoveTEE        = crypto.Keccak256([]byte("removeTEE(bytes32)"))[:4]
	selGetActivatedTEEs = crypto.Keccak256([]byte("getActivatedTEEs(uint8)"))[:4]
	selGetTEE           = crypto.Keccak256([]byte("getTEE(bytes32)"))[:4]
)

// Structs

type TEEInfo struct {
	Owner          common.Address
	PaymentAddress common.Address
	Endpoint       string
	PublicKey      []byte
	TLSCertificate []byte
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

// Client

type Client struct {
	RPCURL          string
	RegistryAddress string
	PrivateKey      string
}

func NewClient(rpcURL, registryAddress, privateKey string) *Client {
	return &Client{
		RPCURL:          rpcURL,
		RegistryAddress: registryAddress,
		PrivateKey:      privateKey,
	}
}

// Account

func (c *Client) GetAccountAddress() (string, error) {
	if c.PrivateKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(c.PrivateKey, "0x"))
		if err != nil {
			return "", err
		}
		return crypto.PubkeyToAddress(key.PublicKey).Hex(), nil
	}
	return c.getFirstAccount()
}

func (c *Client) getFirstAccount() (string, error) {
	resp, _ := c.rpcCall("eth_accounts", []interface{}{})
	var result struct{ Result []string }
	json.Unmarshal(resp, &result)
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no accounts (set TEE_PRIVATE_KEY)")
	}
	return result.Result[0], nil
}

// TEE Calls

func (c *Client) GetActivatedTEEs(teeType uint8) ([]string, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}}.Pack(teeType)
	result, err := c.ethCall(append(selGetActivatedTEEs, encoded...))
	if err != nil {
		return nil, err
	}
	return decodeBytes32Array(result)
}

func (c *Client) GetTEE(teeId [32]byte) (*TEEInfo, error) {
	data := encodeBytes32(selGetTEE, teeId)
	result, err := c.ethCall(data)
	if err != nil {
		return nil, err
	}

	type TEEData struct {
		Owner          common.Address
		PaymentAddress common.Address
		Endpoint       string
		PCRHash        [32]byte
		TEEType        uint8
		IsActive       bool
		RegisteredAt   *big.Int
		LastUpdatedAt  *big.Int
	}

	tupleType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "owner", Type: "address"},
		{Name: "paymentAddress", Type: "address"},
		{Name: "endpoint", Type: "string"},
		{Name: "publicKey", Type: "bytes"},
		{Name: "tlsCertificate", Type: "bytes"},
		{Name: "pcrHash", Type: "bytes32"},
		{Name: "teeType", Type: "uint8"},
		{Name: "isActive", Type: "bool"},
		{Name: "registeredAt", Type: "uint256"},
		{Name: "lastUpdatedAt", Type: "uint256"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ABI type: %v", err)
	}

	args := abi.Arguments{{Type: tupleType}}

	values, err := args.Unpack(result)
	if err != nil {
		return nil, fmt.Errorf("failed to decode TEE data: %v", err)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("empty result from getTEE")
	}

	// The result is a struct (anonymous) containing the tuple fields
	s := values[0].(struct {
		Owner          common.Address `json:"owner"`
		PaymentAddress common.Address `json:"paymentAddress"`
		Endpoint       string         `json:"endpoint"`
		PublicKey      []byte         `json:"publicKey"`
		TlsCertificate []byte         `json:"tlsCertificate"`
		PcrHash        [32]byte       `json:"pcrHash"`
		TeeType        uint8          `json:"teeType"`
		IsActive       bool           `json:"isActive"`
		RegisteredAt   *big.Int       `json:"registeredAt"`
		LastUpdatedAt  *big.Int       `json:"lastUpdatedAt"`
	})

	return &TEEInfo{
		Owner:          s.Owner,
		PaymentAddress: s.PaymentAddress,
		Endpoint:       s.Endpoint,
		PublicKey:      s.PublicKey,
		TLSCertificate: s.TlsCertificate,
		PCRHash:        s.PcrHash,
		TEEType:        s.TeeType,
		IsActive:       s.IsActive,
		RegisteredAt:   time.Unix(s.RegisteredAt.Int64(), 0),
		LastUpdatedAt:  time.Unix(s.LastUpdatedAt.Int64(), 0),
	}, nil
}

func (c *Client) RegisterTEE(from string, attestation, signingKey, tlsCert []byte, paymentAddr, endpoint string, teeType uint8) (string, error) {
	bytesT, _ := abi.NewType("bytes", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	strT, _ := abi.NewType("string", "", nil)
	u8T, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{{Type: bytesT}, {Type: bytesT}, {Type: bytesT}, {Type: addrT}, {Type: strT}, {Type: u8T}}
	encoded, _ := args.Pack(attestation, signingKey, tlsCert, common.HexToAddress(paymentAddr), endpoint, teeType)
	return c.sendTx(from, append(selRegisterTEE, encoded...))
}

func (c *Client) DeactivateTEE(from string, teeId [32]byte) (string, error) {
	return c.sendTx(from, encodeBytes32(selDeactivateTEE, teeId))
}

func (c *Client) ActivateTEE(from string, teeId [32]byte) (string, error) {
	return c.sendTx(from, encodeBytes32(selActivateTEE, teeId))
}

func (c *Client) RemoveTEE(from string, teeId [32]byte) (string, error) {
	return c.sendTx(from, encodeBytes32(selRemoveTEE, teeId))
}

// PCR Calls

func (c *Client) GetActivePCRs() ([]string, error) {
	result, err := c.ethCall(selGetActivePCRs)
	if err != nil {
		return nil, err
	}
	return decodeBytes32Array(result)
}

func (c *Client) ComputePCRHash(pcr0, pcr1, pcr2 []byte) ([32]byte, error) {
	tupleT, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"}, {Name: "pcr1", Type: "bytes"}, {Name: "pcr2", Type: "bytes"},
	})
	args := abi.Arguments{{Type: tupleT}}
	encoded, _ := args.Pack(struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2})
	result, err := c.ethCall(append(selComputePCRHash, encoded...))
	var hash [32]byte
	if len(result) >= 32 {
		copy(hash[:], result[:32])
	}
	return hash, err
}

func (c *Client) ApprovePCR(from string, pcr0, pcr1, pcr2 []byte, version string, teeType uint8) (string, error) {
	tupleT, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "pcr0", Type: "bytes"}, {Name: "pcr1", Type: "bytes"}, {Name: "pcr2", Type: "bytes"},
	})
	strT, _ := abi.NewType("string", "", nil)
	u8T, _ := abi.NewType("uint8", "", nil)

	args := abi.Arguments{{Type: tupleT}, {Type: strT}, {Type: u8T}}
	encoded, _ := args.Pack(struct{ Pcr0, Pcr1, Pcr2 []byte }{pcr0, pcr1, pcr2}, version, teeType)
	return c.sendTx(from, append(selApprovePCR, encoded...))
}

func (c *Client) RevokePCR(from string, pcrHash [32]byte, teeType uint8, gracePeriod *big.Int) (string, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	u8T, _ := abi.NewType("uint8", "", nil)
	u256T, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{{Type: b32T}, {Type: u8T}, {Type: u256T}}
	encoded, _ := args.Pack(pcrHash, teeType, gracePeriod)
	return c.sendTx(from, append(selRevokePCR, encoded...))
}

func (c *Client) IsPCRApproved(teeType uint8, pcrHash [32]byte) (bool, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	b32T, _ := abi.NewType("bytes32", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}, {Type: b32T}}.Pack(teeType, pcrHash)
	result, err := c.ethCall(append(selIsPCRApproved, encoded...))
	return len(result) >= 32 && result[31] == 1, err
}

// Type Calls

func (c *Client) IsValidTEEType(typeId uint8) (bool, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}}.Pack(typeId)
	result, err := c.ethCall(append(selIsValidType, encoded...))
	return len(result) >= 32 && result[31] == 1, err
}

func (c *Client) AddTEEType(from string, typeId uint8, name string) (string, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	strT, _ := abi.NewType("string", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}, {Type: strT}}.Pack(typeId, name)
	return c.sendTx(from, append(selAddTEEType, encoded...))
}

func (c *Client) DeactivateTEEType(from string, typeId uint8) (string, error) {
	u8T, _ := abi.NewType("uint8", "", nil)
	encoded, _ := abi.Arguments{{Type: u8T}}.Pack(typeId)
	return c.sendTx(from, append(selDeactivateTEETyp, encoded...))
}

// Certificate Calls

func (c *Client) SetAWSRootCertificate(from string, cert []byte) (string, error) {
	bytesT, _ := abi.NewType("bytes", "", nil)
	encoded, _ := abi.Arguments{{Type: bytesT}}.Pack(cert)
	return c.sendTx(from, append(selSetAWSRootCert, encoded...))
}

// Role Calls

func (c *Client) HasRole(role [32]byte, account string) (bool, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	result, err := c.ethCall(append(selHasRole, encoded...))
	return len(result) >= 32 && result[31] == 1, err
}

func (c *Client) GrantRole(from string, role [32]byte, account string) (string, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	return c.sendTx(from, append(selGrantRole, encoded...))
}

func (c *Client) RevokeRole(from string, role [32]byte, account string) (string, error) {
	b32T, _ := abi.NewType("bytes32", "", nil)
	addrT, _ := abi.NewType("address", "", nil)
	encoded, _ := abi.Arguments{{Type: b32T}, {Type: addrT}}.Pack(role, common.HexToAddress(account))
	return c.sendTx(from, append(selRevokeRole, encoded...))
}

// RPC / TX Helpers

func (c *Client) ethCall(data []byte) ([]byte, error) {
	params := []interface{}{map[string]string{"to": c.RegistryAddress, "data": "0x" + hex.EncodeToString(data)}, "latest"}
	resp, err := c.rpcCall("eth_call", params)
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

func (c *Client) sendTx(from string, data []byte) (string, error) {
	if c.PrivateKey != "" {
		return c.sendTxSigned(data)
	}
	return c.sendTxUnlocked(from, data)
}

func (c *Client) sendTxUnlocked(from string, data []byte) (string, error) {
	params := []interface{}{map[string]string{"from": from, "to": c.RegistryAddress, "gas": "0x500000", "data": "0x" + hex.EncodeToString(data)}}
	resp, _ := c.rpcCall("eth_sendTransaction", params)
	var result struct{ Result string }
	json.Unmarshal(resp, &result)
	if result.Result == "" {
		return "", fmt.Errorf("tx failed")
	}
	return result.Result, nil
}

func (c *Client) sendTxSigned(data []byte) (string, error) {
	client, err := ethclient.Dial(c.RPCURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %v", err)
	}
	defer client.Close()

	key, err := crypto.HexToECDSA(strings.TrimPrefix(c.PrivateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid private key: %v", err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)

	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to suggest gas price: %v", err)
	}

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get chain ID: %v", err)
	}

	tx := types.NewTransaction(nonce, common.HexToAddress(c.RegistryAddress), big.NewInt(0), 5000000, gasPrice, data)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %v", err)
	}

	err = client.SendTransaction(context.Background(), signed)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %v", err)
	}

	return signed.Hash().Hex(), nil
}

func (c *Client) WaitForTx(txHash string) bool {
	Log("Waiting for confirmation...")
	for i := 0; i < 30; i++ {
		resp, _ := c.rpcCall("eth_getTransactionReceipt", []string{txHash})
		var result struct{ Result *struct{ Status string } }
		json.Unmarshal(resp, &result)
		if result.Result != nil {
			return result.Result.Status == "0x1"
		}
		time.Sleep(time.Second)
	}
	return false
}

func (c *Client) rpcCall(method string, params interface{}) ([]byte, error) {
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": 1})
	resp, err := http.Post(c.RPCURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Encoding/Decoding helpers

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

// Network helpers

func GenerateNonce() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to generate nonce: %v\n", err)
		os.Exit(1)
	}
	return hex.EncodeToString(b)
}

func FetchAttestation(url string) (string, error) {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(bytes.TrimSpace(body)), nil
}

func FetchSigningPublicKey(host string) ([]byte, error) {
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

func FetchTLSCertificate(host, port string) ([]byte, error) {
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

func DecodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func LoadPCRsFromArgs(measurementsFile, pcr0Hex, pcr1Hex, pcr2Hex string) ([]byte, []byte, []byte) {
	// Try measurements file first
	if measurementsFile != "" {
		data, err := os.ReadFile(measurementsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to read measurements file: %v\n", err)
			os.Exit(1)
		}
		var m MeasurementsFile
		if json.Unmarshal(data, &m) == nil {
			pcr0, _ := hex.DecodeString(m.Measurements.PCR0)
			pcr1, _ := hex.DecodeString(m.Measurements.PCR1)
			pcr2, _ := hex.DecodeString(m.Measurements.PCR2)
			return pcr0, pcr1, pcr2
		}
	}

	// Fall back to direct PCR values
	if pcr0Hex == "" || pcr1Hex == "" || pcr2Hex == "" {
		fmt.Fprintln(os.Stderr, "ERROR: Need --measurements-file or --pcr0/--pcr1/--pcr2")
		os.Exit(1)
	}
	pcr0, _ := hex.DecodeString(strings.TrimPrefix(pcr0Hex, "0x"))
	pcr1, _ := hex.DecodeString(strings.TrimPrefix(pcr1Hex, "0x"))
	pcr2, _ := hex.DecodeString(strings.TrimPrefix(pcr2Hex, "0x"))
	return pcr0, pcr1, pcr2
}

// Utility

func ParseBytes32(s string) ([32]byte, error) {
	var result [32]byte
	s = strings.TrimPrefix(s, "0x")

	if len(s) > 64 {
		return result, fmt.Errorf("input too long: %d chars (max 64)", len(s))
	}

	for len(s) < 64 {
		s = "0" + s
	}

	decoded, err := hex.DecodeString(s)
	if err != nil {
		return result, fmt.Errorf("invalid hex: %v", err)
	}

	if len(decoded) != 32 {
		return result, fmt.Errorf("invalid length: got %d bytes, expected 32", len(decoded))
	}

	copy(result[:], decoded)
	return result, nil
}

func ParseUint(s string) uint64 {
	var v uint64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func GetTEETypeName(id uint8) string {
	names := map[uint8]string{0: "LLMProxy", 1: "Validator"}
	if n, ok := names[id]; ok {
		return n
	}
	return "Unknown"
}

func Log(format string, args ...interface{}) {
	fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func PrintTxResult(success bool, msg string) {
	if success {
		fmt.Printf("%s\n", msg)
	} else {
		fmt.Println("Transaction failed")
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
