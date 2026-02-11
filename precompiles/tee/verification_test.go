package tee

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProductionAttestation(t *testing.T) {
	// Load production attestation document from testdata (already base64-encoded)
	attestationBase64Bytes, err := os.ReadFile("testdata/attestation_doc.bin")
	require.NoError(t, err)

	// Convert to string (it's already base64)
	attestationBase64 := string(attestationBase64Bytes)

	// Verify and parse the attestation document
	result, err := VerifyAttestationDocument(attestationBase64, nil, nil)
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
