# TEE Verification Precompile

A unified precompile for Trusted Execution Environment (TEE) verification, combining AWS Nitro attestation verification and RSA-PSS signature verification.

## Address

`0x0000000000000000000000000000000000000901`

## Overview

This precompile provides two cryptographic verification operations essential for TEE-based X402 settlements:

1. **Attestation Verification** - Verifies AWS Nitro Enclave attestation documents with Nitriding dual-key binding
2. **RSA-PSS Signature Verification** - Verifies RSA-PSS signatures used for settlement signing

By merging these two related operations into a single precompile, we reduce the number of precompile addresses used and keep related TEE functionality together.

## Methods

### verifyAttestation

Verifies an AWS Nitro attestation document and validates Nitriding dual-key binding.

**Signature:**
```solidity
function verifyAttestation(
    bytes calldata attestationDocument,
    bytes calldata signingPublicKey,
    bytes calldata tlsCertificate,
    bytes calldata rootCertificate
) external view returns (bool valid, bytes32 pcrHash);
```

**Parameters:**
- `attestationDocument`: CBOR-encoded AWS Nitro attestation document
- `signingPublicKey`: DER-encoded RSA public key for settlement signatures
- `tlsCertificate`: DER-encoded X.509 certificate for TLS connections
- `rootCertificate`: DER-encoded AWS root certificate (empty = use default)

**Returns:**
- `valid`: True if attestation is valid and keys are bound correctly
- `pcrHash`: keccak256(pcr0 || pcr1 || pcr2) - hash of PCR measurements

**Gas Cost:** 500,000

### verifyRSAPSS

Verifies an RSA-PSS signature using SHA-256.

**Signature:**
```solidity
function verifyRSAPSS(
    bytes calldata publicKeyDER,
    bytes32 messageHash,
    bytes calldata signature
) external view returns (bool valid);
```

**Parameters:**
- `publicKeyDER`: DER-encoded RSA public key
- `messageHash`: Keccak256 hash of the message (32 bytes)
- `signature`: RSA-PSS signature bytes

**Returns:**
- `valid`: True if signature is valid

**Gas Cost:** 20,000

## Implementation Details

### Attestation Verification

The attestation verification process:
1. Decodes the COSE Sign1 structure
2. Parses the CBOR attestation document
3. Verifies the ECDSA P-384 signature
4. Validates the certificate chain against the AWS root
5. Verifies Nitriding dual-key binding for TLS certificate
6. Verifies Nitriding dual-key binding for signing key
7. Extracts and returns PCR measurements as a hash

### RSA-PSS Verification

The RSA signature verification:
1. Parses the DER-encoded public key
2. Validates minimum key size (2048 bits)
3. Applies SHA-256 hash to the message hash
4. Verifies RSA-PSS signature with SHA-256 and PSSSaltLengthEqualsHash

## Usage Example

```solidity
import "precompiles/tee/ITEEVerifier.sol";

contract TEERegistry {
    address constant TEE_VERIFIER = 0x0000000000000000000000000000000000000901;

    function registerTEE(
        bytes calldata attestation,
        bytes calldata signingKey,
        bytes calldata tlsCert
    ) external {
        // Verify attestation
        (bool valid, bytes32 pcrHash) = ITEEVerifier(TEE_VERIFIER).verifyAttestation(
            attestation,
            signingKey,
            tlsCert,
            "" // use default AWS root cert
        );
        require(valid, "Invalid attestation");

        // Store TEE with verified PCR hash
        // ...
    }

    function verifySettlement(
        bytes32 teeId,
        bytes32 messageHash,
        bytes calldata signature
    ) external view returns (bool) {
        bytes memory publicKey = getTEEPublicKey(teeId);

        // Verify signature
        return ITEEVerifier(TEE_VERIFIER).verifyRSAPSS(
            publicKey,
            messageHash,
            signature
        );
    }
}
```

## Security Considerations

- The precompile enforces minimum RSA key size of 2048 bits
- Attestation verification includes full certificate chain validation
- Nitriding dual-key binding prevents key substitution attacks
- All cryptographic operations use industry-standard algorithms (ECDSA P-384, RSA-PSS, SHA-256)

## Migration from Separate Precompiles

This precompile replaces two previous precompiles:
- Attestation Verifier (0x901) - now merged into this precompile
- RSA Verifier (0x902) - functionality merged, address no longer used

Contracts should update imports from:
```solidity
import "precompiles/attestation/IAttestationVerifier.sol";
import "precompiles/rsa/IRSAVerifier.sol";
```

To:
```solidity
import "precompiles/tee/ITEEVerifier.sol";
```

And update addresses from two separate addresses to the single TEE verifier address (0x901).
