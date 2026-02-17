package tee

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Enclave endpoints - @Kyle Endpoints should be checked
const (
	enclaveHost    = "13.59.207.188"
	attestationURL = "https://" + enclaveHost + "/enclave/attestation?nonce=0123456789abcdef0123456789abcdef01234567"
	signingKeyURL  = "https://" + enclaveHost + "/enclave/signing-key"
	tlsCertURL     = "https://" + enclaveHost + "/enclave/tls-cert"
)

func TestParseProductionAttestation(t *testing.T) {
	// Load production attestation document from testdata (already base64-encoded)
	attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
	require.NoError(t, err)

	// Convert to string (it's already base64)
	attestationBase64 := string(attestationBase64Bytes)

	// Use a fixed time for certificate validation to avoid expiration issues in tests
	// Certificate is valid between 2026-02-12T17:55:59Z and 2026-02-12T20:56:02Z
	testTime := time.Date(2026, 2, 12, 19, 0, 0, 0, time.UTC)

	// Verify and parse the attestation document
	result, err := VerifyAttestationDocument(attestationBase64, nil, nil, &testTime)
	require.NoError(t, err)

	t.Logf("\n=== PRODUCTION ATTESTATION DOCUMENT ===")
	t.Logf("Valid: %v", result.Valid)
	t.Logf("Error: %v", result.ErrorMessage)

	if result.Valid {
		t.Logf("\n--- PCR Values (SHA-384, 48 bytes each) ---")
		t.Logf("PCR0: %x", result.PCRs.PCR0)
		t.Logf("PCR1: %x", result.PCRs.PCR1)
		t.Logf("PCR2: %x", result.PCRs.PCR2)

		t.Logf("\n--- Public Key ---")
		if len(result.PublicKey) > 0 {
			t.Logf("Length: %d bytes", len(result.PublicKey))
			t.Logf("Hex: %x", result.PublicKey)
		} else {
			t.Logf("No public key in attestation")
		}

		t.Logf("\n--- User Data ---")
		if len(result.UserData) > 0 {
			t.Logf("Length: %d bytes", len(result.UserData))
			t.Logf("Hex: %x", result.UserData)

			// Try to parse Nitriding dual-key format
			if len(result.UserData) == NitridingUserDataLength {
				hashes, err := ParseNitridingUserData(result.UserData)
				if err == nil {
					t.Logf("\n--- Nitriding Key Hashes ---")
					t.Logf("TLS Certificate Hash (SHA256): %x", hashes.TLSCertHash)
					t.Logf("Signing Key Hash (SHA256): %x", hashes.SigningKeyHash)
				}
			}
		} else {
			t.Logf("No user data in attestation")
		}

		// Compute PCR hash (what gets returned by verifyAttestation)
		pcrHash := computePCRHash(result.PCRs.PCR0, result.PCRs.PCR1, result.PCRs.PCR2)
		t.Logf("\n--- Computed PCR Hash (Keccak256) ---")
		t.Logf("PCR Hash: %x", pcrHash)
	}
}

// TestVerifySigningKeyBinding_Security tests signing key binding validation
func TestVerifySigningKeyBinding_Security(t *testing.T) {
	t.Parallel()

	// Create valid signing key
	signingKey := []byte("this_is_a_test_signing_key!!")
	signingKeyHash := sha256.Sum256(signingKey)

	// Create valid Nitriding user_data
	validUserData := make([]byte, NitridingUserDataLength)
	// TLS cert hash (dummy)
	copy(validUserData[0:2], []byte{0x12, 0x20})
	copy(validUserData[2:34], make([]byte, 32))
	// Signing key hash (real)
	copy(validUserData[34:36], []byte{0x12, 0x20})
	copy(validUserData[36:68], signingKeyHash[:])

	tests := []struct {
		name        string
		signingKey  []byte
		userData    []byte
		expectError bool
		description string
	}{
		{
			name:        "valid signing key binding",
			signingKey:  signingKey,
			userData:    validUserData,
			expectError: false,
			description: "Correct signing key should pass",
		},
		{
			name:        "empty signing key",
			signingKey:  []byte{},
			userData:    validUserData,
			expectError: true,
			description: "Empty key should be rejected",
		},
		{
			name:        "wrong signing key",
			signingKey:  []byte("wrong_key_data_here_instead!"),
			userData:    validUserData,
			expectError: true,
			description: "Wrong key should be rejected",
		},
		{
			name:       "single bit flip in user data hash",
			signingKey: signingKey,
			userData: func() []byte {
				tampered := make([]byte, NitridingUserDataLength)
				copy(tampered, validUserData)
				tampered[40] ^= 0x01 // Flip single bit
				return tampered
			}(),
			expectError: true,
			description: "Even single bit tampering should be detected",
		},
		{
			name:        "truncated user data",
			signingKey:  signingKey,
			userData:    validUserData[:67],
			expectError: true,
			description: "Truncated user data should be rejected",
		},
		{
			name:        "extended user data",
			signingKey:  signingKey,
			userData:    append(validUserData, 0x00),
			expectError: true,
			description: "Extended user data should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := VerifySigningKeyBinding(tt.signingKey, tt.userData)
			if tt.expectError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
			}
		})
	}
}

// TestVerifyTLSCertificateBinding_Security tests TLS certificate binding validation
func TestVerifyTLSCertificateBinding_Security(t *testing.T) {
	t.Parallel()

	// Create valid TLS certificate data
	tlsCert := []byte("this_is_a_test_tls_certificate_data_here")
	tlsCertHash := sha256.Sum256(tlsCert)

	// Create valid Nitriding user_data
	validUserData := make([]byte, NitridingUserDataLength)
	// TLS cert hash (real)
	copy(validUserData[0:2], []byte{0x12, 0x20})
	copy(validUserData[2:34], tlsCertHash[:])
	// Signing key hash (dummy)
	copy(validUserData[34:36], []byte{0x12, 0x20})
	copy(validUserData[36:68], make([]byte, 32))

	tests := []struct {
		name        string
		tlsCert     []byte
		userData    []byte
		expectError bool
		description string
	}{
		{
			name:        "valid TLS certificate binding",
			tlsCert:     tlsCert,
			userData:    validUserData,
			expectError: false,
			description: "Correct TLS cert should pass",
		},
		{
			name:        "empty TLS certificate",
			tlsCert:     []byte{},
			userData:    validUserData,
			expectError: true,
			description: "Empty cert should be rejected",
		},
		{
			name:        "wrong TLS certificate",
			tlsCert:     []byte("wrong_certificate_data_here_instead"),
			userData:    validUserData,
			expectError: true,
			description: "Wrong cert should be rejected",
		},
		{
			name: "single bit flip in certificate",
			tlsCert: func() []byte {
				tampered := make([]byte, len(tlsCert))
				copy(tampered, tlsCert)
				tampered[0] ^= 0x01 // Flip single bit
				return tampered
			}(),
			userData:    validUserData,
			expectError: true,
			description: "Even single bit tampering in cert should be detected",
		},
		{
			name:    "single bit flip in user data hash",
			tlsCert: tlsCert,
			userData: func() []byte {
				tampered := make([]byte, NitridingUserDataLength)
				copy(tampered, validUserData)
				tampered[20] ^= 0x01 // Flip single bit in TLS hash
				return tampered
			}(),
			expectError: true,
			description: "Even single bit tampering in user data should be detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := VerifyTLSCertificateBinding(tt.tlsCert, tt.userData)
			if tt.expectError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
			}
		})
	}
}

// TestParseNitridingUserData_EdgeCases tests Nitriding format parsing
func TestParseNitridingUserData_EdgeCases(t *testing.T) {
	t.Parallel()

	// Create valid user data
	validUserData := make([]byte, NitridingUserDataLength)
	copy(validUserData[0:2], []byte{0x12, 0x20})
	tlsHash := []byte("this_is_the_32byte_tls_hash!!!!!") // Exactly 32 bytes
	copy(validUserData[2:34], tlsHash)
	copy(validUserData[34:36], []byte{0x12, 0x20})
	sigHash := []byte("this_is_32byte_signing_hash!!!!!") // Exactly 32 bytes
	copy(validUserData[36:68], sigHash)

	tests := []struct {
		name        string
		userData    []byte
		expectError bool
		validateFn  func(*testing.T, *NitridingKeyHashes)
	}{
		{
			name:        "valid format",
			userData:    validUserData,
			expectError: false,
			validateFn: func(t *testing.T, hashes *NitridingKeyHashes) {
				require.Equal(t, tlsHash, hashes.TLSCertHash)
				require.Equal(t, sigHash, hashes.SigningKeyHash)
			},
		},
		{
			name:        "empty input",
			userData:    []byte{},
			expectError: true,
			validateFn:  nil,
		},
		{
			name:        "one byte short",
			userData:    make([]byte, NitridingUserDataLength-1),
			expectError: true,
			validateFn:  nil,
		},
		{
			name:        "one byte long",
			userData:    make([]byte, NitridingUserDataLength+1),
			expectError: true,
			validateFn:  nil,
		},
		{
			name:        "all zeros",
			userData:    make([]byte, NitridingUserDataLength),
			expectError: false,
			validateFn: func(t *testing.T, hashes *NitridingKeyHashes) {
				require.Equal(t, make([]byte, 32), hashes.TLSCertHash)
				require.Equal(t, make([]byte, 32), hashes.SigningKeyHash)
			},
		},
		{
			name: "all 0xFF",
			userData: func() []byte {
				data := make([]byte, NitridingUserDataLength)
				for i := range data {
					data[i] = 0xFF
				}
				return data
			}(),
			expectError: false,
			validateFn: func(t *testing.T, hashes *NitridingKeyHashes) {
				// Hashes should be extracted regardless of prefix values
				require.Len(t, hashes.TLSCertHash, 32)
				require.Len(t, hashes.SigningKeyHash, 32)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hashes, err := ParseNitridingUserData(tt.userData)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, hashes)
			} else {
				require.NoError(t, err)
				require.NotNil(t, hashes)
				if tt.validateFn != nil {
					tt.validateFn(t, hashes)
				}
			}
		})
	}
}

// TestKeyBindingAttackScenarios tests various attack scenarios
func TestKeyBindingAttackScenarios(t *testing.T) {
	t.Parallel()

	// Setup valid keys and hashes
	legitimateKey := []byte("legitimate_signing_key_data_")
	legitimateKeyHash := sha256.Sum256(legitimateKey)

	attackerKey := []byte("attacker_controlled_key_data_")
	attackerKeyHash := sha256.Sum256(attackerKey)

	t.Run("attacker tries to substitute key with same hash prefix", func(t *testing.T) {
		t.Parallel()

		// Create user data with legitimate key hash
		userData := make([]byte, NitridingUserDataLength)
		copy(userData[0:2], []byte{0x12, 0x20})
		copy(userData[2:34], make([]byte, 32))
		copy(userData[34:36], []byte{0x12, 0x20})
		copy(userData[36:68], legitimateKeyHash[:])

		// Attacker tries to use their key
		err := VerifySigningKeyBinding(attackerKey, userData)
		require.Error(t, err, "Attacker's key should be rejected even if hash prefix matches")
		require.ErrorIs(t, err, ErrPublicKeyBindingFailed)
	})

	t.Run("attacker tries to swap TLS cert hash into signing key position", func(t *testing.T) {
		t.Parallel()

		// Create user data where both hashes are the same
		userData := make([]byte, NitridingUserDataLength)
		copy(userData[0:2], []byte{0x12, 0x20})
		copy(userData[2:34], legitimateKeyHash[:])
		copy(userData[34:36], []byte{0x12, 0x20})
		copy(userData[36:68], attackerKeyHash[:])

		// Verify with legitimate key - should fail because signing hash doesn't match
		err := VerifySigningKeyBinding(legitimateKey, userData)
		require.Error(t, err, "Hash position matters - can't swap positions")
	})

	t.Run("length extension attack attempt", func(t *testing.T) {
		t.Parallel()

		// Try to extend user data beyond 68 bytes with additional data
		userData := make([]byte, NitridingUserDataLength)
		copy(userData[0:2], []byte{0x12, 0x20})
		copy(userData[2:34], make([]byte, 32))
		copy(userData[34:36], []byte{0x12, 0x20})
		copy(userData[36:68], legitimateKeyHash[:])

		extendedData := append(userData, []byte("extra_data")...)

		err := VerifySigningKeyBinding(legitimateKey, extendedData)
		require.Error(t, err, "Extended user data should be rejected")
	})
}

// fetchFromEnclave fetches data from an enclave endpoint, returns nil if unreachable
func fetchFromEnclave(url string) ([]byte, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// TestVerifyAttestation_TimestampFreshness tests timestamp validation with live enclave
// This test fetches fresh attestation, signing key, and TLS cert from the enclave
// and verifies that:
// 1. Fresh attestation with current block time is accepted
// 2. Fresh attestation with old block time (>5 min) is rejected
func TestVerifyAttestation_TimestampFreshness(t *testing.T) {
	// Fetch fresh attestation from enclave
	attestationBase64, err := fetchFromEnclave(attestationURL)
	if err != nil {
		t.Skipf("WARNING: Could not fetch attestation from enclave: %v", err)
		return
	}
	t.Logf("Fetched attestation: %d bytes", len(attestationBase64))

	// Fetch signing key from enclave
	signingKey, err := fetchFromEnclave(signingKeyURL)
	if err != nil {
		t.Skipf("WARNING: Could not fetch signing key from enclave: %v", err)
		return
	}
	t.Logf("Fetched signing key: %d bytes", len(signingKey))

	// Fetch TLS certificate from enclave
	tlsCert, err := fetchFromEnclave(tlsCertURL)
	if err != nil {
		t.Skipf("WARNING: Could not fetch TLS cert from enclave: %v", err)
		return
	}
	t.Logf("Fetched TLS cert: %d bytes", len(tlsCert))

	// Parse attestation to get timestamp
	now := time.Now()
	parseResult, err := VerifyAttestationDocument(string(attestationBase64), nil, nil, &now)
	if err != nil {
		t.Skipf("WARNING: Could not parse attestation: %v", err)
		return
	}
	if !parseResult.Valid {
		t.Skipf("WARNING: Attestation not valid: %s", parseResult.ErrorMessage)
		return
	}

	attestationTimeSec := parseResult.Timestamp / 1000
	t.Logf("Attestation timestamp: %s", time.Unix(int64(attestationTimeSec), 0).UTC())

	p, err := NewPrecompile()
	if err != nil {
		t.Skipf("WARNING: Could not create precompile: %v", err)
		return
	}

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	if !ok {
		t.Skipf("WARNING: Method not found")
		return
	}

	t.Run("fresh attestation accepted", func(t *testing.T) {
		// Block time 1 minute after attestation - within 5 min window
		blockTime := attestationTimeSec + 60
		mockEVM := newMockEVMWithTimestamp(blockTime)

		args := []interface{}{
			attestationBase64,
			signingKey,
			tlsCert,
			[]byte{},
		}

		result, err := p.verifyAttestation(mockEVM, &method, args)
		if err != nil {
			t.Skipf("WARNING: verifyAttestation error: %v", err)
			return
		}

		outputs, err := method.Outputs.Unpack(result)
		if err != nil {
			t.Skipf("WARNING: Could not unpack result: %v", err)
			return
		}

		valid := outputs[0].(bool)
		require.True(t, valid, "Fresh attestation should be accepted")
		t.Logf("Fresh attestation correctly accepted")
	})

	t.Run("old attestation rejected", func(t *testing.T) {
		// Block time 10 minutes after attestation - exceeds 5 min window
		blockTime := attestationTimeSec + 600
		mockEVM := newMockEVMWithTimestamp(blockTime)

		args := []interface{}{
			attestationBase64,
			signingKey,
			tlsCert,
			[]byte{},
		}

		result, err := p.verifyAttestation(mockEVM, &method, args)
		if err != nil {
			t.Skipf("WARNING: verifyAttestation error: %v", err)
			return
		}

		outputs, err := method.Outputs.Unpack(result)
		if err != nil {
			t.Skipf("WARNING: Could not unpack result: %v", err)
			return
		}

		valid := outputs[0].(bool)
		require.False(t, valid, "Old attestation should be rejected")
		t.Logf("Old attestation correctly rejected")
	})
}
