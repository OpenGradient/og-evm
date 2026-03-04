package tee

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseProductionAttestation(t *testing.T) {
	// Load production attestation document from testdata (already base64-encoded)
	attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
	require.NoError(t, err)

	// Convert to string (it's already base64)
	attestationBase64 := string(attestationBase64Bytes)

	// Use a fixed time for certificate validation to avoid expiration issues in tests
	// Certificate is valid between 2026-02-12T17:55:59Z and 2026-02-12T20:56:02Z
	// Must be within MaxAttestationAgeSec (300s) of the attestation timestamp (~17:56:02 UTC)
	testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

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

// TestValidateAttestationTimestamp tests the timestamp freshness validation
func TestValidateAttestationTimestamp(t *testing.T) {
	t.Parallel()

	// Reference block time: Feb 12, 2026 18:00:00 UTC (in seconds)
	blockTimeSec := uint64(time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC).Unix())
	// Matching attestation timestamp in milliseconds
	attestationTimeMs := blockTimeSec * 1000

	tests := []struct {
		name              string
		attestationTimeMs uint64
		blockTimeSec      uint64
		expectError       bool
		description       string
	}{
		{
			name:              "fresh attestation accepted",
			attestationTimeMs: attestationTimeMs,
			blockTimeSec:      blockTimeSec + 60,
			expectError:       false,
			description:       "Attestation 60s old should pass",
		},
		{
			name:              "attestation at exact block time",
			attestationTimeMs: attestationTimeMs,
			blockTimeSec:      blockTimeSec,
			expectError:       false,
			description:       "Attestation at exact block time should pass",
		},
		{
			name:              "attestation at max age boundary",
			attestationTimeMs: attestationTimeMs,
			blockTimeSec:      blockTimeSec + MaxAttestationAgeSec,
			expectError:       false,
			description:       "Attestation at exactly max age should pass",
		},
		{
			name:              "attestation exceeds max age by 1s",
			attestationTimeMs: attestationTimeMs,
			blockTimeSec:      blockTimeSec + MaxAttestationAgeSec + 1,
			expectError:       true,
			description:       "Attestation 1s over max age should be rejected",
		},
		{
			name:              "attestation 10 minutes old",
			attestationTimeMs: attestationTimeMs,
			blockTimeSec:      blockTimeSec + 600,
			expectError:       true,
			description:       "10-minute-old attestation should be rejected",
		},
		{
			name:              "attestation slightly in future accepted",
			attestationTimeMs: (blockTimeSec + 30) * 1000,
			blockTimeSec:      blockTimeSec,
			expectError:       false,
			description:       "Attestation 30s in future should pass (within clock skew)",
		},
		{
			name:              "attestation at max clock skew boundary",
			attestationTimeMs: (blockTimeSec + MaxClockSkewSec) * 1000,
			blockTimeSec:      blockTimeSec,
			expectError:       false,
			description:       "Attestation at exactly max clock skew should pass",
		},
		{
			name:              "attestation exceeds max clock skew by 1s",
			attestationTimeMs: (blockTimeSec + MaxClockSkewSec + 1) * 1000,
			blockTimeSec:      blockTimeSec,
			expectError:       true,
			description:       "Attestation 1s over max clock skew should be rejected",
		},
		{
			name:              "attestation far in future",
			attestationTimeMs: (blockTimeSec + 3600) * 1000,
			blockTimeSec:      blockTimeSec,
			expectError:       true,
			description:       "Attestation 1 hour in future should be rejected",
		},
		{
			name:              "timestamp before minimum valid",
			attestationTimeMs: minValidTimestampMs - 1,
			blockTimeSec:      blockTimeSec,
			expectError:       true,
			description:       "Timestamp before Jan 31, 2026 should be rejected",
		},
		{
			name:              "zero timestamp",
			attestationTimeMs: 0,
			blockTimeSec:      blockTimeSec,
			expectError:       true,
			description:       "Zero timestamp should be rejected",
		},
		{
			name:              "timestamp at minimum valid",
			attestationTimeMs: minValidTimestampMs,
			blockTimeSec:      minValidTimestampMs/1000 + 60,
			expectError:       false,
			description:       "Timestamp at exactly minimum valid should pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateAttestationTimestamp(tt.attestationTimeMs, tt.blockTimeSec)
			if tt.expectError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
			}
		})
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

	legitimateKey := []byte("legitimate_signing_key_data_")
	legitimateKeyHash := sha256.Sum256(legitimateKey)

	attackerKey := []byte("attacker_controlled_key_data_")
	attackerKeyHash := sha256.Sum256(attackerKey)

	t.Run("attacker tries to substitute key with same hash prefix", func(t *testing.T) {
		t.Parallel()

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

// TestConstantTimeEqual tests the constant-time byte comparison function
func TestConstantTimeEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        []byte
		b        []byte
		expected bool
	}{
		{
			name:     "equal slices",
			a:        []byte{0x01, 0x02, 0x03, 0x04},
			b:        []byte{0x01, 0x02, 0x03, 0x04},
			expected: true,
		},
		{
			name:     "different content same length",
			a:        []byte{0x01, 0x02, 0x03, 0x04},
			b:        []byte{0x01, 0x02, 0x03, 0x05},
			expected: false,
		},
		{
			name:     "different lengths",
			a:        []byte{0x01, 0x02, 0x03},
			b:        []byte{0x01, 0x02, 0x03, 0x04},
			expected: false,
		},
		{
			name:     "both empty",
			a:        []byte{},
			b:        []byte{},
			expected: true,
		},
		{
			name:     "one empty one not",
			a:        []byte{},
			b:        []byte{0x01},
			expected: false,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "nil vs empty",
			a:        nil,
			b:        []byte{},
			expected: true, // len(nil) == len([]byte{}) == 0
		},
		{
			name:     "single byte equal",
			a:        []byte{0xFF},
			b:        []byte{0xFF},
			expected: true,
		},
		{
			name:     "single byte differ",
			a:        []byte{0xFE},
			b:        []byte{0xFF},
			expected: false,
		},
		{
			name:     "48-byte PCR values equal",
			a:        make([]byte, 48),
			b:        make([]byte, 48),
			expected: true,
		},
		{
			name: "48-byte PCR values differ in last byte",
			a:    make([]byte, 48),
			b: func() []byte {
				b := make([]byte, 48)
				b[47] = 0x01
				return b
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := constantTimeEqual(tt.a, tt.b)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestComparePCRs tests PCR comparison logic
func TestComparePCRs(t *testing.T) {
	t.Parallel()

	pcr0 := []byte("pcr0_value_48_bytes_padded_to_fill_sha384_hash!!")[:48]
	pcr1 := []byte("pcr1_value_48_bytes_padded_to_fill_sha384_hash!!")[:48]
	pcr2 := []byte("pcr2_value_48_bytes_padded_to_fill_sha384_hash!!")[:48]

	tests := []struct {
		name        string
		extracted   PCRValues384
		expected    PCRValues384
		expectError bool
		errContains string
	}{
		{
			name:        "all PCRs match",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expectError: false,
		},
		{
			name:        "PCR0 mismatch",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: pcr1, PCR1: pcr1, PCR2: pcr2},
			expectError: true,
			errContains: "PCR0 mismatch",
		},
		{
			name:        "PCR1 mismatch",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: pcr0, PCR1: pcr0, PCR2: pcr2},
			expectError: true,
			errContains: "PCR1 mismatch",
		},
		{
			name:        "PCR2 mismatch",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr0},
			expectError: true,
			errContains: "PCR2 mismatch",
		},
		{
			name:        "nil expected PCRs skips comparison",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: nil, PCR1: nil, PCR2: nil},
			expectError: false,
		},
		{
			name:        "empty expected PCRs skips comparison",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: []byte{}, PCR1: []byte{}, PCR2: []byte{}},
			expectError: false,
		},
		{
			name:        "partial check PCR0 only",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: pcr0, PCR1: nil, PCR2: nil},
			expectError: false,
		},
		{
			name:        "partial check PCR2 only mismatch",
			extracted:   PCRValues384{PCR0: pcr0, PCR1: pcr1, PCR2: pcr2},
			expected:    PCRValues384{PCR0: nil, PCR1: nil, PCR2: pcr0},
			expectError: true,
			errContains: "PCR2 mismatch",
		},
		{
			name:        "error message includes hex of extracted PCR",
			extracted:   PCRValues384{PCR0: []byte{0xDE, 0xAD}, PCR1: nil, PCR2: nil},
			expected:    PCRValues384{PCR0: []byte{0xBE, 0xEF}, PCR1: nil, PCR2: nil},
			expectError: true,
			errContains: hex.EncodeToString([]byte{0xDE, 0xAD}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := comparePCRs(tt.extracted, tt.expected)
			if tt.expectError {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateNonce tests nonce validation for replay protection
func TestValidateNonce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		docNonce      []byte
		expectedNonce []byte
		expectError   bool
		errContains   string
	}{
		{
			name:          "nil expected nonce skips validation",
			docNonce:      []byte("some_nonce"),
			expectedNonce: nil,
			expectError:   false,
		},
		{
			name:          "empty expected nonce skips validation",
			docNonce:      []byte("some_nonce"),
			expectedNonce: []byte{},
			expectError:   false,
		},
		{
			name:          "matching nonces",
			docNonce:      []byte("matching_nonce_value"),
			expectedNonce: []byte("matching_nonce_value"),
			expectError:   false,
		},
		{
			name:          "mismatched nonces",
			docNonce:      []byte("nonce_a"),
			expectedNonce: []byte("nonce_b"),
			expectError:   true,
			errContains:   "nonce mismatch",
		},
		{
			name:          "missing doc nonce when expected",
			docNonce:      nil,
			expectedNonce: []byte("expected_nonce"),
			expectError:   true,
			errContains:   "missing nonce",
		},
		{
			name:          "empty doc nonce when expected",
			docNonce:      []byte{},
			expectedNonce: []byte("expected_nonce"),
			expectError:   true,
			errContains:   "missing nonce",
		},
		{
			name:          "both nil",
			docNonce:      nil,
			expectedNonce: nil,
			expectError:   false,
		},
		{
			name:          "different lengths",
			docNonce:      []byte("short"),
			expectedNonce: []byte("much_longer_nonce"),
			expectError:   true,
			errContains:   "nonce mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateNonce(tt.docNonce, tt.expectedNonce)
			if tt.expectError {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateRootCertificate tests root certificate validation
func TestValidateRootCertificate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		certPEM     []byte
		expectError bool
	}{
		{
			name:        "valid AWS Nitro root cert",
			certPEM:     DefaultAWSNitroRootCertPEM,
			expectError: false,
		},
		{
			name:        "empty certificate",
			certPEM:     []byte{},
			expectError: true,
		},
		{
			name:        "nil certificate",
			certPEM:     nil,
			expectError: true,
		},
		{
			name:        "invalid PEM data",
			certPEM:     []byte("not a valid PEM certificate"),
			expectError: true,
		},
		{
			name:        "truncated PEM",
			certPEM:     DefaultAWSNitroRootCertPEM[:100],
			expectError: true,
		},
		{
			name: "valid PEM header but invalid content",
			certPEM: []byte(`-----BEGIN CERTIFICATE-----
aW52YWxpZCBjZXJ0aWZpY2F0ZSBkYXRh
-----END CERTIFICATE-----`),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRootCertificate(tt.certPEM)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestVerifyAttestationWithDefaultCert tests the default-cert convenience wrapper
func TestVerifyAttestationWithDefaultCert(t *testing.T) {
	t.Parallel()

	t.Run("invalid base64 returns error", func(t *testing.T) {
		t.Parallel()
		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)
		result, err := VerifyAttestationWithDefaultCert("not_valid_base64!!!", nil, &testTime)
		require.Error(t, err)
		require.False(t, result.Valid)
	})

	t.Run("valid base64 but invalid COSE returns error", func(t *testing.T) {
		t.Parallel()
		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)
		// Valid base64 encoding of "invalid data"
		result, err := VerifyAttestationWithDefaultCert("aW52YWxpZCBkYXRh", nil, &testTime)
		require.Error(t, err)
		require.False(t, result.Valid)
	})

	t.Run("matches VerifyAttestationDocument with default cert", func(t *testing.T) {
		t.Parallel()
		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

		// Both should return the same error for invalid input
		resultDefault, errDefault := VerifyAttestationWithDefaultCert("aW52YWxpZCBkYXRh", nil, &testTime)
		resultExplicit, errExplicit := VerifyAttestationDocument("aW52YWxpZCBkYXRh", DefaultAWSNitroRootCertPEM, nil, &testTime)

		require.Equal(t, resultDefault.Valid, resultExplicit.Valid)
		require.Equal(t, resultDefault.ErrorMessage, resultExplicit.ErrorMessage)
		// Both should error the same way
		if errDefault != nil {
			require.Error(t, errExplicit)
		}
	})
}

// TestVerifyAttestationWithPCRCheck tests attestation verification with PCR matching
func TestVerifyAttestationWithPCRCheck(t *testing.T) {
	t.Parallel()

	t.Run("invalid attestation fails before PCR check", func(t *testing.T) {
		t.Parallel()
		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)
		expectedPCRs := PCRValues384{
			PCR0: make([]byte, 48),
			PCR1: make([]byte, 48),
			PCR2: make([]byte, 48),
		}
		result, err := VerifyAttestationWithPCRCheck("aW52YWxpZCBkYXRh", nil, expectedPCRs, nil, &testTime)
		require.Error(t, err)
		require.False(t, result.Valid)
	})

	t.Run("uses production attestation with matching PCRs", func(t *testing.T) {
		attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
		require.NoError(t, err)
		attestationBase64 := string(attestationBase64Bytes)

		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

		// First get the actual PCRs from the attestation
		result, err := VerifyAttestationDocument(attestationBase64, nil, nil, &testTime)
		require.NoError(t, err)
		require.True(t, result.Valid)

		// Now verify with matching PCRs
		resultWithPCR, err := VerifyAttestationWithPCRCheck(
			attestationBase64, nil, result.PCRs, nil, &testTime,
		)
		require.NoError(t, err)
		require.True(t, resultWithPCR.Valid)
	})

	t.Run("uses production attestation with mismatched PCRs", func(t *testing.T) {
		attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
		require.NoError(t, err)
		attestationBase64 := string(attestationBase64Bytes)

		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

		// Use wrong PCR values
		wrongPCRs := PCRValues384{
			PCR0: make([]byte, 48),
			PCR1: make([]byte, 48),
			PCR2: make([]byte, 48),
		}
		wrongPCRs.PCR0[0] = 0xFF // Ensure it differs

		result, err := VerifyAttestationWithPCRCheck(
			attestationBase64, nil, wrongPCRs, nil, &testTime,
		)
		require.Error(t, err)
		require.False(t, result.Valid)
		require.Contains(t, result.ErrorMessage, "PCR validation failed")
	})

	t.Run("nil expected PCRs skips PCR comparison", func(t *testing.T) {
		attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
		require.NoError(t, err)
		attestationBase64 := string(attestationBase64Bytes)

		testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

		// Empty PCRs should skip comparison and pass
		emptyPCRs := PCRValues384{}
		result, err := VerifyAttestationWithPCRCheck(
			attestationBase64, nil, emptyPCRs, nil, &testTime,
		)
		require.NoError(t, err)
		require.True(t, result.Valid)
	})
}
