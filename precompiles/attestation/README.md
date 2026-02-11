# AttestationVerifier Precompile

**Address:** `0x0000000000000000000000000000000000000901`

## Overview

The AttestationVerifier precompile provides cryptographic verification of AWS Nitro Enclave attestation documents. It handles operations that cannot be efficiently performed in the EVM:

- CBOR parsing of attestation documents
- X.509 certificate chain verification
- AWS Nitro signature validation
- Nitriding dual-key binding verification (TLS cert + signing key)
- PCR measurement extraction

## Interface

```solidity
interface IAttestationVerifier {
    function verifyAttestation(
        bytes calldata attestationDocument,
        bytes calldata signingPublicKey,
        bytes calldata tlsCertificate,
        bytes calldata rootCertificate
    ) external view returns (bool valid, bytes32 pcrHash);
}
```

## Usage

```solidity
import "./IAttestationVerifier.sol";

contract MyContract {
    IAttestationVerifier constant ATTESTATION_VERIFIER =
        IAttestationVerifier(0x0000000000000000000000000000000000000901);

    function registerTEE(
        bytes calldata attestationDoc,
        bytes calldata signingKey,
        bytes calldata tlsCert
    ) external {
        bytes memory rootCert = getRootCertificate();

        (bool valid, bytes32 pcrHash) = ATTESTATION_VERIFIER.verifyAttestation(
            attestationDoc,
            signingKey,
            tlsCert,
            rootCert
        );

        require(valid, "Invalid attestation");
        // Use pcrHash to verify against approved PCRs...
    }
}
```

## Parameters

### Input

- **attestationDocument**: CBOR-encoded AWS Nitro attestation document
- **signingPublicKey**: DER-encoded RSA public key for settlement signatures
- **tlsCertificate**: DER-encoded X.509 certificate for TLS connections
- **rootCertificate**: DER-encoded AWS root certificate for chain verification

### Output

- **valid**: `true` if attestation is valid and dual-key binding is verified
- **pcrHash**: `keccak256(pcr0 || pcr1 || pcr2)` - hash of PCR measurements

## Verification Steps

1. **Parse attestation document** (CBOR format)
2. **Verify X.509 certificate chain** against AWS root certificate
3. **Extract PCR measurements** (PCR0, PCR1, PCR2) from attestation
4. **Verify Nitriding dual-key binding**:
   - TLS certificate public key must be in attestation's public_key field
   - Signing public key must be in attestation's user_data field
5. **Return** validation result and PCR hash

## Security Considerations

- Always verify the PCR hash against an approved list
- Store the AWS root certificate securely on-chain
- Validate both TLS cert and signing key to prevent key substitution attacks
- The precompile is read-only (view function) - no state changes

## Gas Cost

Approximate gas cost: **20,000 gas** (depends on certificate chain length)

## Integration

This precompile is used by the TEERegistry contract at `contracts/solidity/precompiles/tee/TEERegistry.sol` to verify TEE registrations.

## References

- [AWS Nitro Enclaves](https://aws.amazon.com/ec2/nitro/nitro-enclaves/)
- [Nitriding](https://github.com/brave/nitriding)
- [TEERegistry Contract](../../contracts/solidity/precompiles/tee/)
