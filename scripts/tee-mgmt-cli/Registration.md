# How to Register a New TEE

This guide walks through the complete process of registering a Trusted Execution Environment (TEE) on the OpenGradient network.

## Overview

TEE registration establishes a hardware-rooted chain of trust:
1. **AWS Nitro Hardware** signs an attestation document
2. **Precompile verifies** attestation against AWS root certificate
3. **Contract validates** PCR measurements match approved enclave code
4. **Keys are bound** — signing key & TLS cert cryptographically tied to verified enclave
5. **TEE registered** — now ready to serve verified AI inference requests

## Prerequisites

### 1. Access & Permissions

- **RPC endpoint** to OpenGradient network
- **Funded account** with gas for transactions
- **TEE_OPERATOR role** (granted by admin) to register TEEs
- **Registry address** of the deployed TEERegistry contract

### 2. Running Enclave

- **AWS Nitro Enclave** running approved code
- Accessible at `<enclave-host>:443` with HTTPS endpoints:
  - `GET /enclave/attestation?nonce=<40-char-hex>` — returns base64 attestation document
  - `GET /signing-key` — returns JSON with signing public key PEM
  - TLS endpoint exposing certificate at `<enclave-host>:443`

### 3. PCR Measurements

- **measurements.txt** file with PCR0, PCR1, PCR2 values from your enclave build
- Format:
```json
  {
    "Measurements": {
      "PCR0": "8c7b728e1a8e034aa1cc6c82521adeacec05118b766d5203c80aaf84322b73d095e05672d98fba613ba2b3aaa0e6a482",
      "PCR1": "4b4d5b3661b3efc12920900c80e126e4ce783c522de6c02a2a5bf7af3a2b9327b86776f188e4be1c1c404a129dbda493",
      "PCR2": "74787f27d0c4bbead44d7a61a02df3b8297b0ab1faffb8ebd113a34b434147acb7cd21504b82eeea34100034ccaaed94"
    }
  }
```

## Step-by-Step Registration

### Step 1: Configure CLI
```bash
# Option A: Use .env file
cp .env.example .env
```

Edit `.env`:
```bash
RPC_URL=https://ogevmdevnet.opengradient.ai
TEE_REGISTRY_ADDRESS=0x4e72238852f3c918f4E4e57AeC9280dDB0c80248
TEE_PRIVATE_KEY=your_private_key_here  # Account with TEE_OPERATOR role
```
```bash
### Step 2: One-Time Admin Setup (Skip if you are using our mainnet)

These steps only need to be performed once by an admin:
```bash
# Add TEE type (type 0 = LLMProxy)
./tee-cli type add 0 LLMProxy

# Set AWS Nitro root certificate (validates attestation documents)
./tee-cli cert set-aws aws_nitro_root.pem

# Approve your enclave's PCR measurements
./tee-cli pcr approve \
  -m measurements.txt \
  -v "v1.0.0" \
  --tee-type 0
```

Verify PCR is approved:
```bash
# Compute the PCR hash first
./tee-cli pcr compute -m measurements.txt
# Output: PCR Hash: 0x77786f3515030fe50a260c26d229eff15d2db0e211008f1581dc3e91bfd25703

# Check approval status
./tee-cli pcr check 0x77786f3515030fe50a260c26d229eff15d2db0e211008f1581dc3e91bfd25703
```

### Step 3: Register Your TEE
```bash
./tee-cli tee register 
```

**What happens during registration:**

1. ✅ CLI generates a random 20-byte nonce
2. ✅ Fetches attestation document from `https://enclave_host/enclave/attestation?nonce=<nonce>`
3. ✅ Fetches signing public key from `https://enclave_host/signing-key`
4. ✅ Fetches TLS certificate via TLS handshake to `enclave_host:443`
5. ✅ Computes expected TEE ID: `keccak256(signing_public_key)`
6. ✅ Submits transaction: `registerTEEWithAttestation(attestation, signingKey, tlsCert, paymentAddr, endpoint, teeType)`
7. ✅ Contract verifies attestation via precompile (checks AWS signature, PCR approval)
8. ✅ TEE is registered and **enabled** by default

### Step 4: Verify Registration
```bash
# List all enabled TEEs
./tee-cli tee list

# Show your TEE details
./tee-cli tee show 0xe10366dfcd1a40e97042fbd7b422cd9033921291d0d1b7f40a2a15fc748ae711
```

Expected output:
```
=== TEE Details: 0xe10366dfcd1a40e97042fbd7b422cd9033921291d0d1b7f40a2a15fc748ae711 ===
  Owner:          0x24E4BEa7164BCFb52CCAe10EdE4f5a0cB9F09C4b
  Payment Addr:   0x24E4BEa7164BCFb52CCAe10EdE4f5a0cB9F09C4b
  Endpoint:       https://3.15.214.21
  PCR Hash:       0x77786f3515030fe50a260c26d229eff15d2db0e211008f1581dc3e91bfd25703
  TEE Type:       0 (LLMProxy)
  Enabled:        true
  Registered:     2026-03-11 10:36:37 UTC
  Last Heartbeat: 2026-03-11 10:36:37 UTC
```

## Lifecycle Management
```bash
# Temporarily disable a TEE (can be re-enabled)
./tee-cli tee deactivate 0xe10366dfcd1a40e97042fbd7b422cd9033921291d0d1b7f40a2a15fc748ae711

# Re-enable a disabled TEE (validates PCR is still approved)
./tee-cli tee activate 0xe10366dfcd1a40e97042fbd7b422cd9033921291d0d1b7f40a2a15fc748ae711
```

## Common Issues

### "PCR not approved"
**Cause:** Your enclave's PCR measurements haven't been approved by an admin.

**Solution:**
```bash
# Check which PCRs are approved
./tee-cli pcr list

# Admin must approve your PCR
./tee-cli pcr approve -m measurements.txt -v "v1.0.0" --tee-type 0
```

### "Attestation verification failed"
**Causes:**
- AWS root certificate not set in contract
- Attestation document expired or invalid
- Enclave not running approved code

**Solutions:**
```bash
# Verify AWS cert is set
./tee-cli cert set-aws aws_nitro_root.pem

# Check enclave is accessible
curl -k https://enclave_host/enclave/attestation?nonce=abc123...

# Verify PCR values match approved measurements
./tee-cli pcr compute -m measurements.txt
```

### "Key not found" or "insufficient funds"
**Cause:** Account doesn't have TEE_OPERATOR role or gas funds.

**Solution:**
```bash
# Check role
./tee-cli role check operator account_address

# Admin grants role
./tee-cli role grant-operator account_address

# Fund account (transfer from funded account)
```

### "TEE already exists"
**Cause:** A TEE with the same signing public key is already registered.

**Solution:**
- Each enclave instance has a unique signing key bound at boot
- If you restarted the enclave, it will have a new key → new TEE ID
- If the old TEE ID exists, disable it first:
```bash
  ./tee-cli tee deactivate <old_tee_id>
```

## Quick Reference
```bash
# Complete registration flow (assuming admin setup done)
./tee-cli tee register --enclave-host 3.15.214.21 --tee-type 0
./tee-cli tee list
./tee-cli tee show <tee_id>

# Disable/re-enable
./tee-cli tee deactivate <tee_id>
./tee-cli tee activate <tee_id>

# Check PCR approval
./tee-cli pcr compute -m measurements.txt
./tee-cli pcr check <pcr_hash>
```
