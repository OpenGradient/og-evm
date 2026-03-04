package tee

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"math/big"
	"testing"
	"time"

	gcrypto "crypto"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// newMockEVM creates a minimal EVM context for testing with a specific timestamp
func newMockEVM(timestamp time.Time) *vm.EVM {
	return &vm.EVM{
		Context: vm.BlockContext{
			Time: uint64(timestamp.Unix()),
		},
	}
}

// newMockEVMWithTimestamp creates a minimal EVM context with a specific unix timestamp in seconds
func newMockEVMWithTimestamp(timestampSec uint64) *vm.EVM {
	return &vm.EVM{
		Context: vm.BlockContext{
			Time: timestampSec,
		},
	}
}

func TestComputePCRHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pcr0 []byte
		pcr1 []byte
		pcr2 []byte
		want common.Hash
	}{
		{
			name: "empty PCRs",
			pcr0: []byte{},
			pcr1: []byte{},
			pcr2: []byte{},
			want: crypto.Keccak256Hash([]byte{}),
		},
		{
			name: "all same PCRs",
			pcr0: make([]byte, 48),
			pcr1: make([]byte, 48),
			pcr2: make([]byte, 48),
			want: crypto.Keccak256Hash(make([]byte, 144)),
		},
		{
			name: "different PCRs",
			pcr0: []byte{0x01, 0x02, 0x03},
			pcr1: []byte{0x04, 0x05, 0x06},
			pcr2: []byte{0x07, 0x08, 0x09},
			want: crypto.Keccak256Hash([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09}),
		},
		{
			name: "realistic 48-byte PCRs",
			pcr0: common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001"),
			pcr1: common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002"),
			pcr2: common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003"),
			want: func() common.Hash {
				data := append(
					common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001"),
					common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002")...,
				)
				data = append(data, common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003")...)
				return crypto.Keccak256Hash(data)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := computePCRHash(tt.pcr0, tt.pcr1, tt.pcr2)
			require.Equal(t, tt.want, got, "PCR hash mismatch")
		})
	}
}

func TestComputePCRHashMatchesSolidity(t *testing.T) {
	t.Parallel()

	// Test that Go implementation matches Solidity:
	// keccak256(abi.encodePacked(pcr0, pcr1, pcr2))

	pcr0 := []byte("test_pcr0_value_here")
	pcr1 := []byte("test_pcr1_value_here")
	pcr2 := []byte("test_pcr2_value_here")

	// Go implementation
	goHash := computePCRHash(pcr0, pcr1, pcr2)

	// Expected: keccak256 of concatenated PCRs
	concatenated := append(append(pcr0, pcr1...), pcr2...)
	expectedHash := crypto.Keccak256Hash(concatenated)

	require.Equal(t, expectedHash, goHash, "Go PCR hash must match Solidity keccak256(abi.encodePacked(pcr0, pcr1, pcr2))")
}

func TestVerifyRSAPSS(t *testing.T) {
	t.Parallel()

	// Generate RSA key pair for testing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	// Create a message hash to sign
	messageHash := [32]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}

	// Hash the message hash with SHA256 (as the precompile does)
	h := sha256.Sum256(messageHash[:])

	// Sign with RSA-PSS
	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}
	signature, err := rsa.SignPSS(rand.Reader, privateKey, gcrypto.SHA256, h[:], opts)
	require.NoError(t, err)

	// Create precompile instance
	p, err := NewPrecompile()
	require.NoError(t, err)

	// Get verifyRSAPSS method
	method, ok := p.abi.Methods[MethodVerifyRSAPSS]
	require.True(t, ok)

	tests := []struct {
		name      string
		publicKey []byte
		msgHash   [32]byte
		signature []byte
		wantValid bool
	}{
		{
			name:      "valid signature",
			publicKey: publicKeyDER,
			msgHash:   messageHash,
			signature: signature,
			wantValid: true,
		},
		{
			name:      "invalid signature",
			publicKey: publicKeyDER,
			msgHash:   messageHash,
			signature: make([]byte, len(signature)), // zeros
			wantValid: false,
		},
		{
			name:      "wrong message hash",
			publicKey: publicKeyDER,
			msgHash:   [32]byte{0xff, 0xff, 0xff},
			signature: signature,
			wantValid: false,
		},
		{
			name:      "invalid public key",
			publicKey: []byte{0x01, 0x02, 0x03},
			msgHash:   messageHash,
			signature: signature,
			wantValid: false,
		},
		{
			name:      "empty signature",
			publicKey: publicKeyDER,
			msgHash:   messageHash,
			signature: []byte{},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := []interface{}{tt.publicKey, tt.msgHash, tt.signature}
			result, err := p.verifyRSAPSS(&method, args)
			require.NoError(t, err)

			// Unpack result
			outputs, err := method.Outputs.Unpack(result)
			require.NoError(t, err)
			require.Len(t, outputs, 1)

			valid := outputs[0].(bool)
			require.Equal(t, tt.wantValid, valid)
		})
	}
}

func TestVerifyRSAPSS_KeySizeValidation(t *testing.T) {
	t.Parallel()

	// Generate a weak 1024-bit key (should be rejected)
	weakKey, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)

	weakPublicKeyDER, err := x509.MarshalPKIXPublicKey(&weakKey.PublicKey)
	require.NoError(t, err)

	p, err := NewPrecompile()
	require.NoError(t, err)

	method, ok := p.abi.Methods[MethodVerifyRSAPSS]
	require.True(t, ok)

	messageHash := [32]byte{}
	signature := make([]byte, 128)

	args := []interface{}{weakPublicKeyDER, messageHash, signature}
	result, err := p.verifyRSAPSS(&method, args)
	require.NoError(t, err)

	outputs, err := method.Outputs.Unpack(result)
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	valid := outputs[0].(bool)
	require.False(t, valid, "1024-bit RSA key should be rejected (minimum is 2048)")
}

func TestRequiredGas(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    []byte
		expected uint64
	}{
		{
			name:     "too short input",
			input:    []byte{0x01, 0x02},
			expected: 0,
		},
		{
			name:     "invalid method selector",
			input:    []byte{0xff, 0xff, 0xff, 0xff},
			expected: 0,
		},
		{
			name: "verifyAttestation",
			input: func() []byte {
				method := p.abi.Methods[MethodVerifyAttestation]
				return method.ID
			}(),
			expected: GasVerifyAttestation,
		},
		{
			name: "verifyRSAPSS",
			input: func() []byte {
				method := p.abi.Methods[MethodVerifyRSAPSS]
				return method.ID
			}(),
			expected: GasVerifyRSAPSS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gas := p.RequiredGas(tt.input)
			require.Equal(t, tt.expected, gas)
		})
	}
}

func TestRun_InvalidInput(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty input",
			input: []byte{},
		},
		{
			name:  "too short",
			input: []byte{0x01, 0x02, 0x03},
		},
		{
			name:  "invalid method selector",
			input: []byte{0xff, 0xff, 0xff, 0xff},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			contract := &vm.Contract{
				Input: tt.input,
			}

			result, err := p.Run(nil, contract, true)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestAddress(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	expected := common.HexToAddress(AddressHex)
	require.Equal(t, expected, p.Address())
}

func TestNewPrecompile(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)
	require.NotNil(t, p)
	require.NotNil(t, p.abi)
	require.Equal(t, common.HexToAddress(AddressHex), p.address)

	// Verify ABI has expected methods
	_, ok := p.abi.Methods[MethodVerifyAttestation]
	require.True(t, ok)

	_, ok = p.abi.Methods[MethodVerifyRSAPSS]
	require.True(t, ok)
}

func TestVerifyRSAPSS_Integration(t *testing.T) {
	t.Parallel()

	// Generate key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	// Create message hash (simulating keccak256 from Solidity)
	messageHash := crypto.Keccak256Hash([]byte("test message"))

	// Sign the message hash (after SHA256, as precompile does)
	h := sha256.Sum256(messageHash[:])
	opts := &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
		Hash:       gcrypto.SHA256,
	}
	signature, err := rsa.SignPSS(rand.Reader, privateKey, gcrypto.SHA256, h[:], opts)
	require.NoError(t, err)

	// Create precompile and pack input
	p, err := NewPrecompile()
	require.NoError(t, err)

	method := p.abi.Methods[MethodVerifyRSAPSS]
	input, err := method.Inputs.Pack(publicKeyDER, messageHash, signature)
	require.NoError(t, err)

	// Prepend method selector
	fullInput := append(method.ID, input...)

	// Call precompile via Run
	contract := &vm.Contract{
		Input: fullInput,
	}

	result, err := p.Run(nil, contract, true)
	require.NoError(t, err)

	// Unpack result
	outputs, err := method.Outputs.Unpack(result)
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	valid := outputs[0].(bool)
	require.True(t, valid, "signature verification should succeed")
}

func TestVerifyAttestation_InvalidInputs(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	require.True(t, ok)

	tests := []struct {
		name                string
		attestationDoc      []byte
		signingPublicKey    []byte
		tlsCertificate      []byte
		rootCertificate     []byte
		expectValidResponse bool
	}{
		{
			name:                "empty attestation document",
			attestationDoc:      []byte{},
			signingPublicKey:    []byte("test_key"),
			tlsCertificate:      []byte("test_cert"),
			rootCertificate:     []byte{},
			expectValidResponse: false,
		},
		{
			name:                "empty signing public key",
			attestationDoc:      []byte("test_doc"),
			signingPublicKey:    []byte{},
			tlsCertificate:      []byte("test_cert"),
			rootCertificate:     []byte{},
			expectValidResponse: false,
		},
		{
			name:                "empty TLS certificate",
			attestationDoc:      []byte("test_doc"),
			signingPublicKey:    []byte("test_key"),
			tlsCertificate:      []byte{},
			rootCertificate:     []byte{},
			expectValidResponse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := []interface{}{
				tt.attestationDoc,
				tt.signingPublicKey,
				tt.tlsCertificate,
				tt.rootCertificate,
			}

			mockEVM := newMockEVM(time.Now())
			result, err := p.verifyAttestation(mockEVM, &method, args)
			require.NoError(t, err)

			// Unpack result
			outputs, err := method.Outputs.Unpack(result)
			require.NoError(t, err)
			require.Len(t, outputs, 2) // verifyAttestation returns (bool, bytes32)

			valid := outputs[0].(bool)
			require.Equal(t, tt.expectValidResponse, valid)
		})
	}
}

// TestVerifyAttestation_KeyBindingValidation tests the critical dual-key binding logic
func TestVerifyAttestation_KeyBindingValidation(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	require.True(t, ok)

	// Generate test keys
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	correctPublicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	wrongPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	wrongPublicKeyDER, err := x509.MarshalPKIXPublicKey(&wrongPrivateKey.PublicKey)
	require.NoError(t, err)

	// Create test certificates
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
	}
	correctCertDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	wrongCertDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &wrongPrivateKey.PublicKey, wrongPrivateKey)
	require.NoError(t, err)

	// Note: These tests use invalid attestation documents, so they will fail
	// attestation verification before reaching key binding. This tests that
	// the precompile handles all invalid inputs gracefully without panicking.

	tests := []struct {
		name             string
		signingPublicKey []byte
		tlsCertificate   []byte
		expectValid      bool
	}{
		{
			name:             "mismatched signing key",
			signingPublicKey: wrongPublicKeyDER,
			tlsCertificate:   correctCertDER,
			expectValid:      false,
		},
		{
			name:             "mismatched TLS certificate",
			signingPublicKey: correctPublicKeyDER,
			tlsCertificate:   wrongCertDER,
			expectValid:      false,
		},
		{
			name:             "both keys wrong",
			signingPublicKey: wrongPublicKeyDER,
			tlsCertificate:   wrongCertDER,
			expectValid:      false,
		},
		{
			name:             "oversized signing key",
			signingPublicKey: make([]byte, MaxPublicKeySize+1),
			tlsCertificate:   correctCertDER,
			expectValid:      false,
		},
		{
			name:             "oversized certificate",
			signingPublicKey: correctPublicKeyDER,
			tlsCertificate:   make([]byte, MaxCertificateSize+1),
			expectValid:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := []interface{}{
				[]byte("invalid_attestation_doc"),
				tt.signingPublicKey,
				tt.tlsCertificate,
				[]byte{}, // Use default root cert
			}

			mockEVM := newMockEVM(time.Now())
			result, err := p.verifyAttestation(mockEVM, &method, args)
			require.NoError(t, err, "precompile should not panic")

			outputs, err := method.Outputs.Unpack(result)
			require.NoError(t, err)
			require.Len(t, outputs, 2)

			valid := outputs[0].(bool)
			require.Equal(t, tt.expectValid, valid)
		})
	}
}

// TestVerifyAttestation_SizeLimits tests DoS protection via size limits
func TestVerifyAttestation_SizeLimits(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	require.True(t, ok)

	tests := []struct {
		name           string
		attestationDoc []byte
		signingKey     []byte
		tlsCert        []byte
		rootCert       []byte
		expectValid    bool
	}{
		{
			name:           "attestation too large",
			attestationDoc: make([]byte, MaxAttestationSize+1),
			signingKey:     []byte("key"),
			tlsCert:        []byte("cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "signing key too large",
			attestationDoc: []byte("doc"),
			signingKey:     make([]byte, MaxPublicKeySize+1),
			tlsCert:        []byte("cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "TLS cert too large",
			attestationDoc: []byte("doc"),
			signingKey:     []byte("key"),
			tlsCert:        make([]byte, MaxCertificateSize+1),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "root cert too large",
			attestationDoc: []byte("doc"),
			signingKey:     []byte("key"),
			tlsCert:        []byte("cert"),
			rootCert:       make([]byte, MaxCertificateSize+1),
			expectValid:    false,
		},
		{
			name:           "all at max size (should pass size check)",
			attestationDoc: make([]byte, MaxAttestationSize),
			signingKey:     make([]byte, MaxPublicKeySize),
			tlsCert:        make([]byte, MaxCertificateSize),
			rootCert:       make([]byte, MaxCertificateSize),
			expectValid:    false, // Will fail attestation parsing, but passes size check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := []interface{}{
				tt.attestationDoc,
				tt.signingKey,
				tt.tlsCert,
				tt.rootCert,
			}

			mockEVM := newMockEVM(time.Now())
			result, err := p.verifyAttestation(mockEVM, &method, args)
			require.NoError(t, err, "should handle oversized inputs gracefully")

			outputs, err := method.Outputs.Unpack(result)
			require.NoError(t, err)
			require.Len(t, outputs, 2)

			valid := outputs[0].(bool)
			require.Equal(t, tt.expectValid, valid)
		})
	}
}

// TestRun_VerifyAttestation_Integration tests the Run() method with verifyAttestation selector
func TestRun_VerifyAttestation_Integration(t *testing.T) {
	t.Parallel()

	p, err := NewPrecompile()
	require.NoError(t, err)

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	require.True(t, ok)

	mockEVM := newMockEVM(time.Now())

	tests := []struct {
		name           string
		attestationDoc []byte
		signingKey     []byte
		tlsCert        []byte
		rootCert       []byte
		expectValid    bool
	}{
		{
			name:           "invalid attestation via Run",
			attestationDoc: []byte("invalid_cose_data"),
			signingKey:     []byte("test_key"),
			tlsCert:        []byte("test_cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "empty attestation via Run",
			attestationDoc: []byte{},
			signingKey:     []byte("test_key"),
			tlsCert:        []byte("test_cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "empty signing key via Run",
			attestationDoc: []byte("some_data"),
			signingKey:     []byte{},
			tlsCert:        []byte("test_cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "empty TLS cert via Run",
			attestationDoc: []byte("some_data"),
			signingKey:     []byte("test_key"),
			tlsCert:        []byte{},
			rootCert:       []byte{},
			expectValid:    false,
		},
		{
			name:           "oversized attestation via Run",
			attestationDoc: make([]byte, MaxAttestationSize+1),
			signingKey:     []byte("test_key"),
			tlsCert:        []byte("test_cert"),
			rootCert:       []byte{},
			expectValid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Pack inputs using the ABI method
			input, err := method.Inputs.Pack(
				tt.attestationDoc,
				tt.signingKey,
				tt.tlsCert,
				tt.rootCert,
			)
			require.NoError(t, err)

			// Prepend method selector
			fullInput := append(method.ID, input...)

			contract := &vm.Contract{
				Input: fullInput,
			}

			result, err := p.Run(mockEVM, contract, true)
			require.NoError(t, err, "Run should not error for valid ABI input")

			// Unpack result
			outputs, err := method.Outputs.Unpack(result)
			require.NoError(t, err)
			require.Len(t, outputs, 2)

			valid := outputs[0].(bool)
			require.Equal(t, tt.expectValid, valid)
		})
	}
}

// TestComputePCRHash_SizeLimits tests PCR size validation
func TestComputePCRHash_SizeLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pcr0        []byte
		pcr1        []byte
		pcr2        []byte
		expectEmpty bool
	}{
		{
			name:        "valid PCR sizes",
			pcr0:        make([]byte, 48),
			pcr1:        make([]byte, 48),
			pcr2:        make([]byte, 48),
			expectEmpty: false,
		},
		{
			name:        "PCR0 too large",
			pcr0:        make([]byte, MaxPCRSize+1),
			pcr1:        make([]byte, 48),
			pcr2:        make([]byte, 48),
			expectEmpty: true,
		},
		{
			name:        "PCR1 too large",
			pcr0:        make([]byte, 48),
			pcr1:        make([]byte, MaxPCRSize+1),
			pcr2:        make([]byte, 48),
			expectEmpty: true,
		},
		{
			name:        "PCR2 too large",
			pcr0:        make([]byte, 48),
			pcr1:        make([]byte, 48),
			pcr2:        make([]byte, MaxPCRSize+1),
			expectEmpty: true,
		},
		{
			name:        "all PCRs at max size",
			pcr0:        make([]byte, MaxPCRSize),
			pcr1:        make([]byte, MaxPCRSize),
			pcr2:        make([]byte, MaxPCRSize),
			expectEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := computePCRHash(tt.pcr0, tt.pcr1, tt.pcr2)
			if tt.expectEmpty {
				require.Equal(t, common.Hash{}, result, "oversized PCRs should return empty hash")
			} else {
				require.NotEqual(t, common.Hash{}, result, "valid PCRs should return non-empty hash")
			}
		})
	}
}
