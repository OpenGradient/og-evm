# RSAVerifier Precompile

**Address:** `0x0000000000000000000000000000000000000902`

## Overview

The RSAVerifier precompile provides RSA-PSS signature verification with SHA-256 hashing. RSA signature verification cannot be efficiently performed in the EVM, so this precompile enables TEE settlement verification.

## Interface

```solidity
interface IRSAVerifier {
    function verifyRSAPSS(
        bytes calldata publicKeyDER,
        bytes32 messageHash,
        bytes calldata signature
    ) external view returns (bool valid);
}
```

## Usage

```solidity
import "./IRSAVerifier.sol";

contract MyContract {
    IRSAVerifier constant RSA_VERIFIER =
        IRSAVerifier(0x0000000000000000000000000000000000000902);

    function verifySettlement(
        bytes memory signingPublicKey,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes memory signature
    ) public view returns (bool) {
        bytes32 messageHash = keccak256(
            abi.encodePacked(inputHash, outputHash, timestamp)
        );

        return RSA_VERIFIER.verifyRSAPSS(
            signingPublicKey,
            messageHash,
            signature
        );
    }
}
```

## Parameters

### Input

- **publicKeyDER**: DER-encoded RSA public key (typically 2048-bit or 4096-bit)
- **messageHash**: SHA-256 hash of the message (32 bytes)
- **signature**: RSA-PSS signature bytes

### Output

- **valid**: `true` if signature is valid for the given message hash and public key

## Signature Scheme

- **Algorithm**: RSA-PSS (Probabilistic Signature Scheme)
- **Hash Function**: SHA-256
- **Salt Length**: SHA-256 output length (32 bytes)
- **MGF**: MGF1 with SHA-256

## Security Considerations

- Always hash the message before passing to this precompile
- Use keccak256 for on-chain message hashing (consistent with EVM)
- The precompile expects the message to already be hashed (32 bytes)
- The precompile is read-only (view function) - no state changes

## Gas Cost

Approximate gas cost: **20,000 gas** (depends on key size and signature length)

## Integration

This precompile is used by the TEERegistry contract at `contracts/solidity/precompiles/tee/TEERegistry.sol` to verify settlement signatures from TEEs.

## Example Flow

1. TEE signs a settlement: `signature = RSA_PSS_Sign(privateKey, messageHash)`
2. Settlement is submitted on-chain with signature
3. Contract calls: `RSA_VERIFIER.verifyRSAPSS(publicKey, messageHash, signature)`
4. If valid, settlement is processed

## References

- [RSA-PSS Specification (RFC 8017)](https://www.rfc-editor.org/rfc/rfc8017)
- [TEERegistry Contract](../../contracts/solidity/precompiles/tee/)
