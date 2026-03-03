package tee

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// base64DecodeAttestation is a helper to decode the base64 attestation doc for testing.
func base64DecodeAttestation(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}

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

// TestProductionAttestationParsing_Regression pins the exact parsed output of the production
// attestation document. If CBOR/COSE decoding, PCR extraction, user_data parsing, or PCR hash
// computation changes, this test will catch it.
func TestProductionAttestationParsing_Regression(t *testing.T) {
	attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
	require.NoError(t, err)

	// Certificate valid window: 2026-02-12T17:55:59Z to 2026-02-12T20:56:02Z
	// Attestation timestamp ~17:56:02 UTC
	testTime := time.Date(2026, 2, 12, 18, 0, 0, 0, time.UTC)

	result, err := VerifyAttestationDocument(string(attestationBase64Bytes), nil, nil, &testTime)
	require.NoError(t, err)
	require.True(t, result.Valid, "attestation should be valid; error: %s", result.ErrorMessage)

	// --- Pin exact PCR values (SHA-384, 48 bytes each) ---
	expectedPCR0, _ := hex.DecodeString("9baef83909784e4d2cb84466c02931bb8125e948b62029029e80bfa1698bfd7069408e4ab20d1c99c859e8f774cce0ff")
	expectedPCR1, _ := hex.DecodeString("4b4d5b3661b3efc12920900c80e126e4ce783c522de6c02a2a5bf7af3a2b9327b86776f188e4be1c1c404a129dbda493")
	expectedPCR2, _ := hex.DecodeString("769925c8ae0c5c2d1aa86de149213b1c5dec642557bf0c426c6581f0e562a042d1c7d2578443515d103f74ce55337666")

	require.Equal(t, expectedPCR0, result.PCRs.PCR0, "PCR0 mismatch — CBOR parsing may have changed")
	require.Equal(t, expectedPCR1, result.PCRs.PCR1, "PCR1 mismatch — CBOR parsing may have changed")
	require.Equal(t, expectedPCR2, result.PCRs.PCR2, "PCR2 mismatch — CBOR parsing may have changed")

	// --- Pin exact user_data (68 bytes, Nitriding dual-key format) ---
	expectedUserData, _ := hex.DecodeString("1220b735546536e78ee12beea2d384fcda35f7491446c6858f79fa7ad5e4ae89b49f1220abf9d7453422eb419fc1391604b070a81bd6a61f4d9ae0ee71ab24d77670c154")
	require.Equal(t, expectedUserData, result.UserData, "user_data mismatch — CBOR or Nitriding format parsing may have changed")

	// --- Pin exact public key ---
	expectedPublicKey, _ := hex.DecodeString("64756d6d79") // "dummy"
	require.Equal(t, expectedPublicKey, result.PublicKey, "public_key mismatch")

	// --- Pin Nitriding parsed hashes ---
	require.Len(t, result.UserData, NitridingUserDataLength)
	hashes, err := ParseNitridingUserData(result.UserData)
	require.NoError(t, err)

	expectedTLSHash, _ := hex.DecodeString("b735546536e78ee12beea2d384fcda35f7491446c6858f79fa7ad5e4ae89b49f")
	expectedSigningHash, _ := hex.DecodeString("abf9d7453422eb419fc1391604b070a81bd6a61f4d9ae0ee71ab24d77670c154")
	require.Equal(t, expectedTLSHash, hashes.TLSCertHash, "TLS cert hash mismatch — Nitriding user_data parsing may have changed")
	require.Equal(t, expectedSigningHash, hashes.SigningKeyHash, "signing key hash mismatch — Nitriding user_data parsing may have changed")

	// --- Pin exact PCR hash (Keccak256 of pcr0||pcr1||pcr2) ---
	expectedPCRHash := common.HexToHash("8082af22571613eabd5dad9c7e857c4f16154ded3d387386d8193b53906f6a24")
	actualPCRHash := computePCRHash(result.PCRs.PCR0, result.PCRs.PCR1, result.PCRs.PCR2)
	require.Equal(t, expectedPCRHash, actualPCRHash, "PCR hash mismatch — Keccak256 computation may have changed")
}

// TestCOSESign1Decoding_Regression ensures the COSE Sign1 envelope is decoded correctly
// and the inner attestation document has the expected structure.
func TestCOSESign1Decoding_Regression(t *testing.T) {
	attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
	require.NoError(t, err)

	// Decode base64 → raw COSE bytes
	attestationBytes, err := base64DecodeAttestation(string(attestationBase64Bytes))
	require.NoError(t, err)

	// Decode COSE Sign1 envelope
	coseMsg, err := decodeCOSESign1(attestationBytes)
	require.NoError(t, err)

	// The COSE structure must have all four fields populated
	require.NotEmpty(t, coseMsg.ProtectedHeader, "protected header must not be empty")
	require.NotEmpty(t, coseMsg.Payload, "payload must not be empty")
	require.NotEmpty(t, coseMsg.Signature, "signature must not be empty")

	// ECDSA P-384 signature should be exactly 96 bytes (r||s, 48 each)
	require.Len(t, coseMsg.Signature, P384SignatureLength,
		"COSE signature must be %d bytes for ES384", P384SignatureLength)

	// Re-decode the payload to verify stable CBOR round-trip
	var attestDoc AttestationDocument
	err = cbor.Unmarshal(coseMsg.Payload, &attestDoc)
	require.NoError(t, err)

	require.Equal(t, "i-0340e0cb833504eb6-enc019c12d31f78864d", attestDoc.ModuleID,
		"module_id mismatch — CBOR decoding may have changed")
	require.Equal(t, "SHA384", attestDoc.Digest)
	require.NotEmpty(t, attestDoc.PCRs, "PCR map must not be empty")
	// PCR0, PCR1, PCR2 must exist (these are the ones we use for pcrHash)
	require.Contains(t, attestDoc.PCRs, 0, "PCR0 must be present")
	require.Contains(t, attestDoc.PCRs, 1, "PCR1 must be present")
	require.Contains(t, attestDoc.PCRs, 2, "PCR2 must be present")
	require.NotEmpty(t, attestDoc.Certificate, "attestation signing certificate must be present")
	require.NotEmpty(t, attestDoc.CABundle, "CA bundle must be present")
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
