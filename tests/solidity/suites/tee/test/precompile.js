const { expect } = require('chai')
const hre = require('hardhat')
const crypto = require('crypto')

describe('TEE Precompile (0x900)', function () {
    const TEE_VERIFIER_ADDRESS = '0x0000000000000000000000000000000000000900'
    const GAS_LIMIT = 10_000_000

    let signer, TEETestHelper, helper

    before(async () => {
        [signer] = await hre.ethers.getSigners()

        // Deploy a minimal test helper for direct precompile access
        const HelperFactory = await hre.ethers.getContractFactory('TEETestHelper')

        // We need a dummy registry address for the helper constructor
        const dummyRegistry = hre.ethers.ZeroAddress
        helper = await HelperFactory.deploy(dummyRegistry)
        await helper.waitForDeployment()

        console.log('TEETestHelper deployed at:', await helper.getAddress())
    })

    describe('verifyRSAPSS', function () {
        let publicKeyDER, privateKey, publicKey

        before(() => {
            // Generate RSA key pair for testing
            const { publicKey: pubKey, privateKey: privKey } = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: {
                    type: 'spki',
                    format: 'der'
                },
                privateKeyEncoding: {
                    type: 'pkcs8',
                    format: 'pem'
                }
            })

            publicKeyDER = '0x' + pubKey.toString('hex')
            privateKey = privKey
            publicKey = crypto.createPublicKey(privKey)
        })

        it('should verify valid RSA-PSS signature', async function () {
            // Create a message hash (simulating keccak256 from Solidity)
            const message = 'test message'
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes(message))

            // Sign using RSA-PSS with SHA-256
            // The precompile expects: SHA256(messageHash) signed with RSA-PSS
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')
            const sha256Hash = crypto.createHash('sha256').update(messageHashBuffer).digest()

            const signature = crypto.sign(null, sha256Hash, {
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            console.log('Message hash:', messageHash)
            console.log('Signature length:', signature.length)

            const result = await helper.testVerifyRSAPSS(publicKeyDER, messageHash, signatureHex)
            const receipt = await result.wait()

            // Check for the SignatureVerificationResult event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.true

            console.log('✓ RSA-PSS signature verified successfully')
        })

        it('should reject invalid signature', async function () {
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test message'))
            const invalidSignature = '0x' + Buffer.alloc(256, 0).toString('hex') // All zeros

            const result = await helper.testVerifyRSAPSS(publicKeyDER, messageHash, invalidSignature)
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.false

            console.log('✓ Invalid signature rejected correctly')
        })

        it('should reject signature with wrong message hash', async function () {
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test message'))
            const wrongMessageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('wrong message'))

            // Sign the correct message
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')
            const sha256Hash = crypto.createHash('sha256').update(messageHashBuffer).digest()

            const signature = crypto.sign(null, sha256Hash, {
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            // Verify with wrong message hash
            const result = await helper.testVerifyRSAPSS(publicKeyDER, wrongMessageHash, signatureHex)
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.false

            console.log('✓ Signature with wrong message hash rejected')
        })

        it('should reject invalid public key format', async function () {
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test'))
            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')
            const invalidPublicKey = '0x0102030405' // Invalid DER

            const result = await helper.testVerifyRSAPSS(invalidPublicKey, messageHash, signature)
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.false

            console.log('✓ Invalid public key format rejected')
        })

        it('should reject weak RSA key (1024 bit)', async function () {
            // Generate weak 1024-bit key
            const { publicKey: weakPubKey, privateKey: weakPrivKey } = crypto.generateKeyPairSync('rsa', {
                modulusLength: 1024,
                publicKeyEncoding: {
                    type: 'spki',
                    format: 'der'
                },
                privateKeyEncoding: {
                    type: 'pkcs8',
                    format: 'pem'
                }
            })

            const weakPublicKeyDER = '0x' + weakPubKey.toString('hex')
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test'))

            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')
            const sha256Hash = crypto.createHash('sha256').update(messageHashBuffer).digest()

            const signature = crypto.sign(null, sha256Hash, {
                key: weakPrivKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            const result = await helper.testVerifyRSAPSS(weakPublicKeyDER, messageHash, signatureHex)
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.false

            console.log('✓ Weak 1024-bit RSA key rejected (minimum is 2048-bit)')
        })

        it('should handle empty inputs gracefully', async function () {
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test'))

            const result = await helper.testVerifyRSAPSS('0x', messageHash, '0x')
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'SignatureVerificationResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.valid).to.be.false

            console.log('✓ Empty inputs handled gracefully')
        })
    })

    describe('verifyAttestation', function () {
        it('should reject empty attestation document', async function () {
            const emptyAttestation = '0x'
            const dummyKey = '0x0102030405'
            const dummyCert = '0x0102030405'
            const rootCert = '0x'

            const result = await helper.testVerifyAttestation(
                emptyAttestation,
                dummyKey,
                dummyCert,
                rootCert
            )
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'PrecompileCallResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.success).to.be.false

            console.log('✓ Empty attestation document rejected')
        })

        it('should reject empty signing public key', async function () {
            const dummyAttestation = '0x0102030405'
            const emptyKey = '0x'
            const dummyCert = '0x0102030405'
            const rootCert = '0x'

            const result = await helper.testVerifyAttestation(
                dummyAttestation,
                emptyKey,
                dummyCert,
                rootCert
            )
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'PrecompileCallResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.success).to.be.false

            console.log('✓ Empty signing public key rejected')
        })

        it('should reject empty TLS certificate', async function () {
            const dummyAttestation = '0x0102030405'
            const dummyKey = '0x0102030405'
            const emptyCert = '0x'
            const rootCert = '0x'

            const result = await helper.testVerifyAttestation(
                dummyAttestation,
                dummyKey,
                emptyCert,
                rootCert
            )
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'PrecompileCallResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.success).to.be.false

            console.log('✓ Empty TLS certificate rejected')
        })

        it('should reject invalid attestation format', async function () {
            const invalidAttestation = '0x' + Buffer.alloc(100, 0xFF).toString('hex')
            const dummyKey = '0x' + Buffer.alloc(100, 0x01).toString('hex')
            const dummyCert = '0x' + Buffer.alloc(100, 0x02).toString('hex')
            const rootCert = '0x'

            const result = await helper.testVerifyAttestation(
                invalidAttestation,
                dummyKey,
                dummyCert,
                rootCert
            )
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'PrecompileCallResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.success).to.be.false

            console.log('✓ Invalid attestation format rejected')
        })

        it('should respect size limits for DoS prevention', async function () {
            // Test oversized attestation (> 16KB)
            const oversizedAttestation = '0x' + Buffer.alloc(17 * 1024, 0xAA).toString('hex')
            const normalKey = '0x' + Buffer.alloc(100, 0x01).toString('hex')
            const normalCert = '0x' + Buffer.alloc(100, 0x02).toString('hex')
            const rootCert = '0x'

            const result = await helper.testVerifyAttestation(
                oversizedAttestation,
                normalKey,
                normalCert,
                rootCert
            )
            const receipt = await result.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = helper.interface.parseLog(log)
                    return parsed.name === 'PrecompileCallResult'
                } catch {
                    return false
                }
            })

            expect(event).to.not.be.undefined
            const parsed = helper.interface.parseLog(event)
            expect(parsed.args.success).to.be.false

            console.log('✓ Oversized attestation rejected (DoS prevention)')
        })
    })

    describe('Gas Usage', function () {
        let publicKeyDER, privateKey

        before(() => {
            const { publicKey, privateKey: privKey } = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: {
                    type: 'spki',
                    format: 'der'
                },
                privateKeyEncoding: {
                    type: 'pkcs8',
                    format: 'pem'
                }
            })

            publicKeyDER = '0x' + publicKey.toString('hex')
            privateKey = privKey
        })

        it('should measure RSA-PSS verification gas usage', async function () {
            const messageHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test'))
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')
            const sha256Hash = crypto.createHash('sha256').update(messageHashBuffer).digest()

            const signature = crypto.sign(null, sha256Hash, {
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            const tx = await helper.estimateRSAPSSGas(publicKeyDER, messageHash, signatureHex)
            const receipt = await tx.wait()

            console.log('RSA-PSS gas used:', receipt.gasUsed.toString())
            console.log('✓ RSA-PSS gas measurement successful')
        })
    })
})
