//go:build integration

package tee

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Enclave endpoint paths — host is configured via TEE_ENCLAVE_HOST env var
const (
	enclavePort     = "443"
	attestationPath = "/enclave/attestation?nonce=0123456789abcdef0123456789abcdef01234567"
	signingKeyPath  = "/signing-key"
)

// AttestationResponse is the JSON structure returned by /signing-key
type AttestationResponse struct {
	PublicKey string `json:"public_key"`
}

// fetchFromEnclave fetches raw bytes from an enclave HTTP endpoint.
// InsecureSkipVerify is required because Nitriding generates a self-signed TLS
// certificate inside the enclave. Authenticity is verified via the attestation document.
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

// fetchSigningPublicKey fetches the DER-encoded RSA public key from /signing-key.
// The endpoint returns JSON: {"public_key": "<PEM>"}
func fetchSigningPublicKey(host string) ([]byte, error) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	url := fmt.Sprintf("https://%s%s", host, signingKeyPath)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var attestResp AttestationResponse
	if err := json.Unmarshal(rawBody, &attestResp); err != nil {
		return nil, fmt.Errorf("json unmarshal failed: %v", err)
	}

	// Handle literal \n in JSON string
	pemStr := strings.ReplaceAll(attestResp.PublicKey, `\n`, "\n")
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		block, _ = pem.Decode(rawBody)
	}
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM")
	}
	return block.Bytes, nil
}

// fetchTLSCertificate extracts the leaf TLS certificate (DER) from the TLS handshake.
// This is what Nitriding hashes in user_data[2:34].
func fetchTLSCertificate(host, port string) ([]byte, error) {
	conn, err := tls.Dial("tcp", host+":"+port, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("server presented no certificates")
	}

	cert := certs[0]
	if _, err := x509.ParseCertificate(cert.Raw); err != nil {
		return nil, fmt.Errorf("invalid certificate: %v", err)
	}
	return cert.Raw, nil
}

// decodeAttestationBase64 decodes a base64 attestation document (tries StdEncoding then URLEncoding).
func decodeAttestationBase64(b64 []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(string(b64))
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(string(b64))
		if err != nil {
			return nil, fmt.Errorf("base64 decode failed: %v", err)
		}
	}
	return raw, nil
}

// ---------------------------------------------------------------------------
// Integration Tests — require live enclave
// Run with: TEE_ENCLAVE_HOST=<ip> go test -tags=integration -v ./precompiles/tee/...
// Skipped automatically if TEE_ENCLAVE_HOST is not set.
// ---------------------------------------------------------------------------

// TestVerifyAttestation_TimestampFreshness tests timestamp validation with live enclave.
// Fetches fresh attestation, signing key (via JSON/PEM from /signing-key),
// and TLS cert (via TLS handshake), then verifies the 5-minute freshness window.
//
// NOTE: Mock block time is derived from the attestation's own Timestamp field, not
// time.Now(), so the test is not sensitive to wall clock time during execution.
func TestVerifyAttestation_TimestampFreshness(t *testing.T) {
	// Require TEE_ENCLAVE_HOST to be set
	host := os.Getenv("TEE_ENCLAVE_HOST")
	if host == "" {
		t.Skip("TEE_ENCLAVE_HOST not set — skipping integration test")
	}

	attestationURL := "https://" + host + attestationPath

	// --- Step 1: fetch fresh attestation (base64 from enclave) ---
	attestationBase64, err := fetchFromEnclave(attestationURL)
	if err != nil {
		t.Fatalf("WARNING: Could not fetch attestation from enclave (enclave unreachable): %v", err)
		return
	}
	t.Logf("Fetched attestation: %d bytes", len(attestationBase64))

	// Decode base64 → raw COSE bytes for the precompile
	// (the precompile re-encodes internally, so we must pass raw bytes)
	attestationRaw, err := decodeAttestationBase64(attestationBase64)
	if err != nil {
		t.Fatalf("WARNING: Could not decode attestation: %v", err)
		return
	}
	t.Logf("Decoded attestation raw: %d bytes", len(attestationRaw))

	// --- Step 2: fetch signing key (DER from JSON PEM) ---
	signingKey, err := fetchSigningPublicKey(host)
	if err != nil {
		t.Fatalf("WARNING: Could not fetch signing key from enclave: %v", err)
		return
	}
	t.Logf("Fetched signing key: %d bytes", len(signingKey))

	// --- Step 3: fetch TLS cert from TLS handshake ---
	tlsCert, err := fetchTLSCertificate(host, enclavePort)
	if err != nil {
		t.Fatalf("WARNING: Could not fetch TLS cert from enclave: %v", err)
		return
	}
	t.Logf("Fetched TLS cert: %d bytes", len(tlsCert))
	t.Logf("TLS cert SHA256: %x", sha256.Sum256(tlsCert))

	// --- Step 4: parse attestation to get timestamp ---
	now := time.Now()
	parseResult, err := VerifyAttestationDocument(string(attestationBase64), nil, nil, &now)
	if err != nil {
		t.Fatalf("WARNING: Could not parse attestation: %v", err)
		return
	}
	if !parseResult.Valid {
		t.Fatalf("WARNING: Attestation not valid: %s", parseResult.ErrorMessage)
		return
	}

	// Attestation timestamp from AWS Nitro is in milliseconds since Unix epoch.
	// Convert to seconds to compare with block timestamp (which is in seconds).
	attestationTimeSec := parseResult.Timestamp / 1000
	t.Logf("Attestation timestamp: %s", time.Unix(int64(attestationTimeSec), 0).UTC())

	// --- Step 5: cross-check TLS cert hash matches user_data ---
	if len(parseResult.UserData) == NitridingUserDataLength {
		hashes, err := ParseNitridingUserData(parseResult.UserData)
		if err == nil {
			computed := sha256.Sum256(tlsCert)
			tlsHashArr := [32]byte{}
			copy(tlsHashArr[:], hashes.TLSCertHash)
			t.Logf("user_data TLS hash:  %x", hashes.TLSCertHash)
			t.Logf("computed TLS hash:   %x", computed)
			if computed != tlsHashArr {
				t.Logf("WARNING: TLS cert hash mismatch — cert may have rotated since attestation")
			} else {
				t.Logf("TLS cert hash matches attestation user_data ✓")
			}
		}
	}

	// --- Step 6: run precompile tests ---
	p, err := NewPrecompile()
	if err != nil {
		t.Fatalf("WARNING: Could not create precompile: %v", err)
		return
	}

	method, ok := p.abi.Methods[MethodVerifyAttestation]
	if !ok {
		t.Fatalf("WARNING: Method not found")
		return
	}

	t.Run("fresh attestation accepted", func(t *testing.T) {
		// Block time 1 minute after attestation — within 5 min window
		blockTime := attestationTimeSec + 60
		mockEVM := newMockEVMWithTimestamp(blockTime)

		// Pass raw COSE bytes (not base64) — precompile encodes internally
		args := []interface{}{attestationRaw, signingKey, tlsCert, []byte{}}
		result, err := p.verifyAttestation(mockEVM, &method, args)
		if err != nil {
			t.Fatalf("verifyAttestation error: %v", err)
		}

		outputs, err := method.Outputs.Unpack(result)
		if err != nil {
			t.Fatalf("could not unpack result: %v", err)
		}

		valid := outputs[0].(bool)
		t.Logf("valid: %v", valid)
		if len(outputs) > 1 {
			t.Logf("pcrHash: %x", outputs[1])
		}

		require.True(t, valid, "Fresh attestation should be accepted")
		t.Logf("Fresh attestation correctly accepted ✓")
	})

	t.Run("old attestation rejected", func(t *testing.T) {
		// Block time 10 minutes after attestation — exceeds 5 min window
		blockTime := attestationTimeSec + 600
		mockEVM := newMockEVMWithTimestamp(blockTime)

		args := []interface{}{attestationRaw, signingKey, tlsCert, []byte{}}
		result, err := p.verifyAttestation(mockEVM, &method, args)
		if err != nil {
			t.Fatalf("verifyAttestation error: %v", err)
		}

		outputs, err := method.Outputs.Unpack(result)
		if err != nil {
			t.Fatalf("could not unpack result: %v", err)
		}

		valid := outputs[0].(bool)
		require.False(t, valid, "Old attestation should be rejected")
		t.Logf("Old attestation correctly rejected ✓")
	})
}
