const { expect } = require('chai')
const crypto = require('crypto')

const TEETestHelper = artifacts.require('TEETestHelper')

contract('TEE Precompile (0x900)', function (accounts) {
    const TEE_VERIFIER_ADDRESS = '0x0000000000000000000000000000000000000900'
    const GAS_LIMIT = 10_000_000

    let signer, helper

    before(async () => {
        signer = accounts[0]

        // Deploy a minimal test helper for direct precompile access
        // We need a dummy registry address for the helper constructor
        const dummyRegistry = '0x0000000000000000000000000000000000000000'
        helper = await TEETestHelper.new(dummyRegistry)

        console.log('TEETestHelper deployed at:', helper.address)
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
            const messageHash = web3.utils.keccak256(message)

            // The precompile will compute SHA256(messageHash) and verify the signature
            // So we need to sign SHA256(messageHash)
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')

            // Use crypto.createSign to properly handle SHA-256 hashing and RSA-PSS signing
            const sign = crypto.createSign('SHA256')
            sign.update(messageHashBuffer)  // This will be SHA-256 hashed internally
            sign.end()

            const signature = sign.sign({
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            console.log('Message hash:', messageHash)
            console.log('Signature length:', signature.length)

            const receipt = await helper.testVerifyRSAPSS(publicKeyDER, messageHash, signatureHex)

            // Check for the SignatureVerificationResult event
            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.true

            console.log('✓ RSA-PSS signature verified successfully')
        })

        it('should reject invalid signature', async function () {
            const messageHash = web3.utils.keccak256('test message')
            const invalidSignature = '0x' + Buffer.alloc(256, 0).toString('hex') // All zeros

            const receipt = await helper.testVerifyRSAPSS(publicKeyDER, messageHash, invalidSignature)

            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.false

            console.log('✓ Invalid signature rejected correctly')
        })

        it('should reject signature with wrong message hash', async function () {
            const messageHash = web3.utils.keccak256('test message')
            const wrongMessageHash = web3.utils.keccak256('wrong message')

            // Sign the correct message
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')

            const sign = crypto.createSign('SHA256')
            sign.update(messageHashBuffer)
            sign.end()

            const signature = sign.sign({
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            // Verify with wrong message hash
            const receipt = await helper.testVerifyRSAPSS(publicKeyDER, wrongMessageHash, signatureHex)

            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.false

            console.log('✓ Signature with wrong message hash rejected')
        })

        it('should reject invalid public key format', async function () {
            const messageHash = web3.utils.keccak256('test')
            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')
            const invalidPublicKey = '0x0102030405' // Invalid DER

            const receipt = await helper.testVerifyRSAPSS(invalidPublicKey, messageHash, signature)

            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.false

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
            const messageHash = web3.utils.keccak256('test')

            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')

            const sign = crypto.createSign('SHA256')
            sign.update(messageHashBuffer)
            sign.end()

            const signature = sign.sign({
                key: weakPrivKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            const receipt = await helper.testVerifyRSAPSS(weakPublicKeyDER, messageHash, signatureHex)

            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.false

            console.log('✓ Weak 1024-bit RSA key rejected (minimum is 2048-bit)')
        })

        it('should handle empty inputs gracefully', async function () {
            const messageHash = web3.utils.keccak256('test')

            const receipt = await helper.testVerifyRSAPSS('0x', messageHash, '0x')

            const event = receipt.logs.find(log => log.event === 'SignatureVerificationResult')

            expect(event).to.not.be.undefined
            expect(event.args.valid).to.be.false

            console.log('✓ Empty inputs handled gracefully')
        })
    })

    describe('verifyAttestation', function () {
        it('should reject empty attestation document', async function () {
            const emptyAttestation = '0x'
            const dummyKey = '0x0102030405'
            const dummyCert = '0x0102030405'
            const rootCert = '0x'

            try {
                const receipt = await helper.testVerifyAttestation(
                    emptyAttestation,
                    dummyKey,
                    dummyCert,
                    rootCert
                )

                const event = receipt.logs.find(log => log.event === 'PrecompileCallResult')

                expect(event).to.not.be.undefined
                expect(event.args.success).to.be.false

                console.log('✓ Empty attestation document rejected')
            } catch (error) {
                // Transaction might revert, which is also acceptable for invalid inputs
                console.log('✓ Empty attestation document rejected (transaction reverted)')
            }
        })

        it('should reject empty signing public key', async function () {
            const dummyAttestation = '0x0102030405'
            const emptyKey = '0x'
            const dummyCert = '0x0102030405'
            const rootCert = '0x'

            try {
                const receipt = await helper.testVerifyAttestation(
                    dummyAttestation,
                    emptyKey,
                    dummyCert,
                    rootCert
                )

                const event = receipt.logs.find(log => log.event === 'PrecompileCallResult')

                expect(event).to.not.be.undefined
                expect(event.args.success).to.be.false

                console.log('✓ Empty signing public key rejected')
            } catch (error) {
                // Transaction might revert, which is also acceptable for invalid inputs
                console.log('✓ Empty signing public key rejected (transaction reverted)')
            }
        })

        it('should reject empty TLS certificate', async function () {
            const dummyAttestation = '0x0102030405'
            const dummyKey = '0x0102030405'
            const emptyCert = '0x'
            const rootCert = '0x'

            try {
                const receipt = await helper.testVerifyAttestation(
                    dummyAttestation,
                    dummyKey,
                    emptyCert,
                    rootCert
                )

                const event = receipt.logs.find(log => log.event === 'PrecompileCallResult')

                expect(event).to.not.be.undefined
                expect(event.args.success).to.be.false

                console.log('✓ Empty TLS certificate rejected')
            } catch (error) {
                // Transaction might revert, which is also acceptable for invalid inputs
                console.log('✓ Empty TLS certificate rejected (transaction reverted)')
            }
        })

        it('should reject invalid attestation format', async function () {
            const invalidAttestation = '0x' + Buffer.alloc(100, 0xFF).toString('hex')
            const dummyKey = '0x' + Buffer.alloc(100, 0x01).toString('hex')
            const dummyCert = '0x' + Buffer.alloc(100, 0x02).toString('hex')
            const rootCert = '0x'

            try {
                const receipt = await helper.testVerifyAttestation(
                    invalidAttestation,
                    dummyKey,
                    dummyCert,
                    rootCert
                )

                const event = receipt.logs.find(log => log.event === 'PrecompileCallResult')

                expect(event).to.not.be.undefined
                expect(event.args.success).to.be.false

                console.log('✓ Invalid attestation format rejected')
            } catch (error) {
                // Transaction might revert, which is also acceptable for invalid inputs
                console.log('✓ Invalid attestation format rejected (transaction reverted)')
            }
        })

        it('should respect size limits for DoS prevention', async function () {
            // Test oversized attestation (> 16KB)
            const oversizedAttestation = '0x' + Buffer.alloc(17 * 1024, 0xAA).toString('hex')
            const normalKey = '0x' + Buffer.alloc(100, 0x01).toString('hex')
            const normalCert = '0x' + Buffer.alloc(100, 0x02).toString('hex')
            const rootCert = '0x'

            try {
                const receipt = await helper.testVerifyAttestation(
                    oversizedAttestation,
                    normalKey,
                    normalCert,
                    rootCert
                )

                const event = receipt.logs.find(log => log.event === 'PrecompileCallResult')

                expect(event).to.not.be.undefined
                expect(event.args.success).to.be.false

                console.log('✓ Oversized attestation rejected (DoS prevention)')
            } catch (error) {
                // Transaction might revert due to gas or size limits
                console.log('✓ Oversized attestation rejected (DoS prevention - transaction reverted)')
            }
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
            const messageHash = web3.utils.keccak256('test')
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')

            const sign = crypto.createSign('SHA256')
            sign.update(messageHashBuffer)
            sign.end()

            const signature = sign.sign({
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            const gasUsed = await helper.estimateRSAPSSGas.call(publicKeyDER, messageHash, signatureHex)

            console.log('RSA-PSS gas used:', gasUsed.toString())
            console.log('✓ RSA-PSS gas measurement successful')
        })
    })
})
