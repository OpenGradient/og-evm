package tee

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// =============================================================================
// AWS Nitro Attestation Verification
// Reference: Python implementation (verify.py) from nitro-enclave-python-demo
// TODO: khalifa Different Improvement should be done later
// TODO: the registration from TEE directly (msg binary)
// TODO: Private request to investigate.
// =============================================================================

// AttestationDocument represents the decoded AWS Nitro attestation payload
type AttestationDocument struct {
	ModuleID    string         `cbor:"module_id"`
	Timestamp   uint64         `cbor:"timestamp"`
	Digest      string         `cbor:"digest"`
	PCRs        map[int][]byte `cbor:"pcrs"`
	Certificate []byte         `cbor:"certificate"`
	CABundle    [][]byte       `cbor:"cabundle"`
	PublicKey   []byte         `cbor:"public_key,omitempty"`
	UserData    []byte         `cbor:"user_data,omitempty"`
	Nonce       []byte         `cbor:"nonce,omitempty"`
}

// COSESign1Message represents a COSE Sign1 structure
type COSESign1Message struct {
	ProtectedHeader   []byte
	UnprotectedHeader map[any]any
	Payload           []byte
	Signature         []byte
}

// PCRValues384 holds PCR values for verification (48 bytes each for SHA-384)
type PCRValues384 struct {
	PCR0 []byte
	PCR1 []byte
	PCR2 []byte
}

// AttestationResult contains the verification outcome
type AttestationResult struct {
	Valid        bool
	PublicKey    []byte
	UserData     []byte
	PCRs         PCRValues384
	ErrorMessage string
}

// NitridingKeyHashes contains the two SHA256 hashes from user_data
type NitridingKeyHashes struct {
	TLSCertHash    []byte // SHA256 of entire TLS certificate DER
	SigningKeyHash []byte // SHA256 of signing public key DER
}

// Constants
const (
	SHA384HashLength    = 48
	SHA256HashLength    = 32
	P384SignatureLength = 96
	// TODo Kyle: is it alawys foloowing this format?
	// Nitriding user_data format: 0x1220 + hash(32) + 0x1220 + hash(32) = 68 bytes
	NitridingUserDataLength = 68
	MultihashSHA256Prefix   = 0x12 // SHA256 identifier in multihash
	MultihashLength32       = 0x20 // 32 bytes length indicator
)

// DefaultAWSNitroRootCertPEM is the AWS Nitro Attestation PKI root certificate
var DefaultAWSNitroRootCertPEM = []byte(`-----BEGIN CERTIFICATE-----
MIICETCCAZagAwIBAgIRAPkxdWgbkK/hHUbMtOTn+FYwCgYIKoZIzj0EAwMwSTEL
MAkGA1UEBhMCVVMxDzANBgNVBAoMBkFtYXpvbjEMMAoGA1UECwwDQVdTMRswGQYD
VQQDDBJhd3Mubml0cm8tZW5jbGF2ZXMwHhcNMTkxMDI4MTMyODA1WhcNNDkxMDI4
MTQyODA1WjBJMQswCQYDVQQGEwJVUzEPMA0GA1UECgwGQW1hem9uMQwwCgYDVQQL
DANBV1MxGzAZBgNVBAMMEmF3cy5uaXRyby1lbmNsYXZlczB2MBAGByqGSM49AgEG
BSuBBAAiA2IABPwCVOumCMHzaHDimtqQvkY4MpJzbolL//Zy2YlES1BR5TSksfbb
48C8WBoyt7F2Bw7eEtaaP+ohG2bnUs990d0JX28TcPQXCEPZ3BABIeTPYwEoCWZE
h8l5YoQwTcU/9KNCMEAwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUkCW1DdkF
R+eWw5b6cp3PmanfS5YwDgYDVR0PAQH/BAQDAgGGMAoGCCqGSM49BAMDA2kAMGYC
MQCjfy+Rocm9Xue4YnwWmNJVA44fA0P5W2OpYow9OYCVRaEevL8uO1XYru5xtMPW
rfMCMQCi85sWBbJwKKXdS6BptQFuZbT73o/gBh1qUxl/nNr12UO8Yfwr6wPLb+6N
IwLz3/Y=
-----END CERTIFICATE-----`)

// ============================================================================
// NITRIDING USER_DATA PARSING
// ============================================================================

// ParseNitridingUserData extracts TLS cert and signing key hashes from Nitriding user_data
// Format: 0x1220 + SHA256(tlsCertDER) + 0x1220 + SHA256(signingKeyDER) = 68 bytes total
// 0x12 = SHA256 multihash type, 0x20 = 32 bytes length indicator
func ParseNitridingUserData(userData []byte) (*NitridingKeyHashes, error) {
	if len(userData) != NitridingUserDataLength {
		return nil, fmt.Errorf("invalid user_data length: got %d, expected %d", len(userData), NitridingUserDataLength)
	}

	// Verify first multihash prefix (TLS certificate hash)
	if userData[0] != MultihashSHA256Prefix || userData[1] != MultihashLength32 {
		return nil, fmt.Errorf("invalid first multihash prefix: expected 0x1220, got 0x%02x%02x", userData[0], userData[1])
	}

	// Verify second multihash prefix (Signing key hash)
	if userData[34] != MultihashSHA256Prefix || userData[35] != MultihashLength32 {
		return nil, fmt.Errorf("invalid second multihash prefix: expected 0x1220, got 0x%02x%02x", userData[34], userData[35])
	}

	return &NitridingKeyHashes{
		TLSCertHash:    userData[2:34],  // Skip 0x1220 prefix, get 32-byte hash
		SigningKeyHash: userData[36:68], // Skip 0x1220 prefix, get 32-byte hash
	}, nil
}

// ============================================================================
// VERIFICATION FUNCTIONS
// ============================================================================

// VerifyAttestationDocument performs full attestation verification using provided root cert
func VerifyAttestationDocument(
	attestationBase64 string,
	rootCertPEM []byte,
	expectedNonce []byte,
) (*AttestationResult, error) {
	result := &AttestationResult{Valid: false}

	// Use default cert if none provided
	if len(rootCertPEM) == 0 {
		rootCertPEM = DefaultAWSNitroRootCertPEM
	}

	// Step 1: Decode base64
	attestationBytes, err := base64.StdEncoding.DecodeString(attestationBase64)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("base64 decode failed: %v", err)
		return result, err
	}

	// Step 2: Decode COSE Sign1
	coseMsg, err := decodeCOSESign1(attestationBytes)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("COSE decode failed: %v", err)
		return result, err
	}

	// Step 3: Decode attestation document from payload
	var attestDoc AttestationDocument
	if err := cbor.Unmarshal(coseMsg.Payload, &attestDoc); err != nil {
		result.ErrorMessage = fmt.Sprintf("attestation decode failed: %v", err)
		return result, err
	}

	// Step 4: Extract PCRs
	result.PCRs = PCRValues384{
		PCR0: attestDoc.PCRs[0],
		PCR1: attestDoc.PCRs[1],
		PCR2: attestDoc.PCRs[2],
	}

	// Step 5: Validate nonce (optional)
	if len(expectedNonce) > 0 {
		if err := validateNonce(attestDoc.Nonce, expectedNonce); err != nil {
			result.ErrorMessage = fmt.Sprintf("nonce validation failed: %v", err)
			return result, err
		}
	}

	// Step 6: Verify COSE signature
	signingCert, err := x509.ParseCertificate(attestDoc.Certificate)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("cert parse failed: %v", err)
		return result, err
	}

	if err := verifyCOSESignatureES384(coseMsg, signingCert); err != nil {
		result.ErrorMessage = fmt.Sprintf("signature verification failed: %v", err)
		return result, err
	}

	// Step 7: Validate certificate chain using provided root cert
	if err := validateCertificateChain(signingCert, attestDoc.CABundle, rootCertPEM); err != nil {
		result.ErrorMessage = fmt.Sprintf("cert chain validation failed: %v", err)
		return result, err
	}

	// Success
	result.Valid = true
	result.PublicKey = attestDoc.PublicKey
	result.UserData = attestDoc.UserData
	return result, nil
}

// VerifyAttestationWithPCRCheck verifies attestation and checks PCRs against expected values
func VerifyAttestationWithPCRCheck(
	attestationBase64 string,
	rootCertPEM []byte,
	expectedPCRs PCRValues384,
	expectedNonce []byte,
) (*AttestationResult, error) {
	// First verify attestation
	result, err := VerifyAttestationDocument(attestationBase64, rootCertPEM, expectedNonce)
	if err != nil {
		return result, err
	}
	if !result.Valid {
		return result, errors.New(result.ErrorMessage)
	}

	// Then check PCRs match
	if err := comparePCRs(result.PCRs, expectedPCRs); err != nil {
		result.Valid = false
		result.ErrorMessage = fmt.Sprintf("PCR validation failed: %v", err)
		return result, err
	}

	return result, nil
}

// VerifyAttestationWithDefaultCert verifies using the default AWS root certificate
func VerifyAttestationWithDefaultCert(
	attestationBase64 string,
	expectedNonce []byte,
) (*AttestationResult, error) {
	return VerifyAttestationDocument(attestationBase64, DefaultAWSNitroRootCertPEM, expectedNonce)
}

// ValidateRootCertificate checks if a certificate is valid PEM format
func ValidateRootCertificate(certPEM []byte) error {
	if len(certPEM) == 0 {
		return ErrInvalidCertificate
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return ErrInvalidCertificate
	}

	return nil
}

// ============================================================================
// DUAL-KEY BINDING VERIFICATION (FIXED FOR NITRIDING FORMAT)
// ============================================================================

// VerifyTLSCertificateBinding verifies that TLS certificate hash matches attestation user_data
// Nitriding stores SHA256(entire TLS certificate DER) in user_data[2:34]
func VerifyTLSCertificateBinding(tlsCertDER []byte, userData []byte) error {
	if len(tlsCertDER) == 0 {
		return fmt.Errorf("empty TLS certificate")
	}

	// Parse user_data to get TLS cert hash
	hashes, err := ParseNitridingUserData(userData)
	if err != nil {
		return fmt.Errorf("failed to parse user_data: %w", err)
	}

	// FIXED: Hash the ENTIRE TLS certificate DER (not just the public key)
	expectedHash := sha256.Sum256(tlsCertDER)

	// Verify match
	if !bytes.Equal(hashes.TLSCertHash, expectedHash[:]) {
		return fmt.Errorf("%w: TLS certificate hash mismatch (expected %x, got %x)",
			ErrPublicKeyBindingFailed, expectedHash[:8], hashes.TLSCertHash[:8])
	}

	return nil
}

// VerifySigningKeyBinding verifies that signing key hash matches attestation user_data
// Nitriding stores SHA256(signing public key DER) in user_data[36:68]
func VerifySigningKeyBinding(signingKeyDER []byte, userData []byte) error {
	if len(signingKeyDER) == 0 {
		return fmt.Errorf("empty signing key")
	}

	// Parse user_data to get signing key hash
	hashes, err := ParseNitridingUserData(userData)
	if err != nil {
		return fmt.Errorf("failed to parse user_data: %w", err)
	}

	// Hash the signing public key DER
	expectedHash := sha256.Sum256(signingKeyDER)

	// Verify match
	if !bytes.Equal(hashes.SigningKeyHash, expectedHash[:]) {
		return fmt.Errorf("%w: signing key hash mismatch (expected %x, got %x)",
			ErrPublicKeyBindingFailed, expectedHash[:8], hashes.SigningKeyHash[:8])
	}

	return nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// decodeCOSESign1 decodes COSE Sign1 structure
func decodeCOSESign1(data []byte) (*COSESign1Message, error) {
	var raw []cbor.RawMessage
	if err := cbor.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("COSE unmarshal: %w", err)
	}
	if len(raw) != 4 {
		return nil, fmt.Errorf("invalid COSE: expected 4 elements, got %d", len(raw))
	}

	msg := &COSESign1Message{}

	if err := cbor.Unmarshal(raw[0], &msg.ProtectedHeader); err != nil {
		return nil, fmt.Errorf("unmarshal protected header: %w", err)
	}
	if err := cbor.Unmarshal(raw[1], &msg.UnprotectedHeader); err != nil {
		return nil, fmt.Errorf("unmarshal unprotected header: %w", err)
	}
	if err := cbor.Unmarshal(raw[2], &msg.Payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if err := cbor.Unmarshal(raw[3], &msg.Signature); err != nil {
		return nil, fmt.Errorf("unmarshal signature: %w", err)
	}

	return msg, nil
}

// comparePCRs compares extracted PCRs with expected values
func comparePCRs(extracted, expected PCRValues384) error {
	if len(expected.PCR0) > 0 {
		if !constantTimeEqual(extracted.PCR0, expected.PCR0) {
			return fmt.Errorf("PCR0 mismatch: got %s", hex.EncodeToString(extracted.PCR0))
		}
	}
	if len(expected.PCR1) > 0 {
		if !constantTimeEqual(extracted.PCR1, expected.PCR1) {
			return fmt.Errorf("PCR1 mismatch: got %s", hex.EncodeToString(extracted.PCR1))
		}
	}
	if len(expected.PCR2) > 0 {
		if !constantTimeEqual(extracted.PCR2, expected.PCR2) {
			return fmt.Errorf("PCR2 mismatch: got %s", hex.EncodeToString(extracted.PCR2))
		}
	}
	return nil
}

// validateNonce checks nonce for replay protection
func validateNonce(docNonce, expectedNonce []byte) error {
	if len(expectedNonce) == 0 {
		return nil
	}
	if len(docNonce) == 0 {
		return errors.New("missing nonce in attestation")
	}
	if !constantTimeEqual(docNonce, expectedNonce) {
		return errors.New("nonce mismatch")
	}
	return nil
}

// verifyCOSESignatureES384 verifies COSE Sign1 with ES384 (ECDSA P-384)
func verifyCOSESignatureES384(msg *COSESign1Message, cert *x509.Certificate) error {
	pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("certificate does not contain ECDSA key")
	}
	if pubKey.Curve != elliptic.P384() {
		return errors.New("certificate key is not P-384 curve")
	}

	// Construct Sig_structure: ["Signature1", protected, external_aad, payload]
	sigStructure := []any{"Signature1", msg.ProtectedHeader, []byte{}, msg.Payload}
	sigStructureBytes, err := cbor.Marshal(sigStructure)
	if err != nil {
		return fmt.Errorf("marshal sig_structure: %w", err)
	}

	// Hash with SHA-384
	hash := sha512.Sum384(sigStructureBytes)

	// Parse signature (r || s, each 48 bytes)
	if len(msg.Signature) != P384SignatureLength {
		return fmt.Errorf("invalid signature length: got %d, expected %d", len(msg.Signature), P384SignatureLength)
	}

	r := new(big.Int).SetBytes(msg.Signature[:48])
	s := new(big.Int).SetBytes(msg.Signature[48:])

	if !ecdsa.Verify(pubKey, hash[:], r, s) {
		return errors.New("ECDSA signature verification failed")
	}
	return nil
}

// validateCertificateChain validates the signing certificate against provided root
func validateCertificateChain(signingCert *x509.Certificate, caBundle [][]byte, rootCertPEM []byte) error {
	// Parse root certificate
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(rootCertPEM) {
		return errors.New("failed to parse root certificate")
	}

	// Build intermediate certificate pool
	intermediatePool := x509.NewCertPool()
	for i, certDER := range caBundle {
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return fmt.Errorf("parse CA bundle cert %d: %w", i, err)
		}
		intermediatePool.AddCert(cert)
	}

	// Verify certificate chain
	_, err := signingCert.Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
	})
	if err != nil {
		return fmt.Errorf("certificate chain verification: %w", err)
	}

	return nil
}

// constantTimeEqual performs constant-time byte comparison
func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
