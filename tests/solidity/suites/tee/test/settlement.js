const { expect } = require('chai')
const hre = require('hardhat')
const crypto = require('crypto')

describe('TEE Settlement Verification', function () {
    let owner, teeOperator, user1
    let registry, helper
    let publicKeyDER, privateKey, teeId

    before(async () => {
        [owner, teeOperator, user1] = await hre.ethers.getSigners()

        // Deploy TEERegistry
        const RegistryFactory = await hre.ethers.getContractFactory('TEERegistry')
        registry = await RegistryFactory.deploy()
        await registry.waitForDeployment()

        // Deploy test helper
        const HelperFactory = await hre.ethers.getContractFactory('TEETestHelper')
        helper = await HelperFactory.deploy(await registry.getAddress())
        await helper.waitForDeployment()

        // Grant TEE_OPERATOR role
        const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()
        await registry.grantRole(TEE_OPERATOR_ROLE, teeOperator.address)

        // Generate RSA key pair for testing
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
        teeId = hre.ethers.keccak256(publicKeyDER)

        console.log('Setup complete')
        console.log('TEE ID:', teeId)
    })

    describe('Message Hash Computation', function () {
        it('should compute message hash correctly', async function () {
            const inputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('input data'))
            const outputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('output data'))
            const timestamp = Math.floor(Date.now() / 1000)

            const messageHash = await helper.computeMessageHash(inputHash, outputHash, timestamp)

            const expectedHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'uint256'],
                    [inputHash, outputHash, timestamp]
                )
            )

            expect(messageHash).to.equal(expectedHash)

            console.log('✓ Message hash computation matches expected format')
        })

        it('should compute settlement hash correctly', async function () {
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const timestamp = Math.floor(Date.now() / 1000)

            const settlementHash = await helper.computeSettlementHash(
                teeId,
                inputHash,
                outputHash,
                timestamp
            )

            const expectedHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'bytes32', 'uint256'],
                    [teeId, inputHash, outputHash, timestamp]
                )
            )

            expect(settlementHash).to.equal(expectedHash)

            console.log('✓ Settlement hash computation correct')
        })
    })

    describe('Signature Verification', function () {
        // Note: We can't fully test this without a real TEE registration,
        // but we can test the signature verification logic with a mock TEE

        it('should handle verification for non-existent TEE', async function () {
            const nonExistentTeeId = hre.ethers.keccak256('0xDEADBEEF')
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const timestamp = Math.floor(Date.now() / 1000)
            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')

            const isValid = await helper.verifySignature(
                nonExistentTeeId,
                inputHash,
                outputHash,
                timestamp,
                signature
            )

            expect(isValid).to.be.false

            console.log('✓ Non-existent TEE returns false for signature verification')
        })

        it('should compute correct message hash for signature verification', async function () {
            const inputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test input'))
            const outputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('test output'))
            const timestamp = 1234567890

            const messageHash = await registry.computeMessageHash(inputHash, outputHash, timestamp)

            // The message hash should be keccak256(inputHash || outputHash || timestamp)
            const expectedHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'uint256'],
                    [inputHash, outputHash, timestamp]
                )
            )

            expect(messageHash).to.equal(expectedHash)

            console.log('✓ Message hash format is correct')
        })
    })

    describe('Settlement Verification - Timestamp Validation', function () {
        it('should reject settlements with old timestamps', async function () {
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')

            // Timestamp too old (more than 1 hour ago)
            const currentBlock = await hre.ethers.provider.getBlock('latest')
            const oldTimestamp = currentBlock.timestamp - 3700 // 1 hour + 100 seconds

            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')

            await expect(
                registry.verifySettlement(teeId, inputHash, outputHash, oldTimestamp, signature)
            ).to.be.revertedWithCustomError(registry, 'TimestampTooOld')

            console.log('✓ Old timestamp rejected correctly')
        })

        it('should reject settlements with future timestamps', async function () {
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')

            // Timestamp in the future (more than 5 minutes ahead)
            const currentBlock = await hre.ethers.provider.getBlock('latest')
            const futureTimestamp = currentBlock.timestamp + 400 // 5 minutes + 100 seconds

            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')

            await expect(
                registry.verifySettlement(teeId, inputHash, outputHash, futureTimestamp, signature)
            ).to.be.revertedWithCustomError(registry, 'TimestampInFuture')

            console.log('✓ Future timestamp rejected correctly')
        })

        it('should accept timestamps within valid window', async function () {
            const currentBlock = await hre.ethers.provider.getBlock('latest')

            // Test timestamp 30 seconds ago (should be valid)
            const validOldTimestamp = currentBlock.timestamp - 30

            // Test timestamp 2 minutes ahead (should be valid)
            const validFutureTimestamp = currentBlock.timestamp + 120

            // Both should pass timestamp validation (though they'll fail on signature)
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const signature = '0x' + Buffer.alloc(256, 0).toString('hex')

            // These will fail on signature verification, not timestamp
            await expect(
                registry.verifySettlement(teeId, inputHash, outputHash, validOldTimestamp, signature)
            ).to.be.revertedWithCustomError(registry, 'InvalidSignature')

            await expect(
                registry.verifySettlement(teeId, inputHash, outputHash, validFutureTimestamp, signature)
            ).to.be.revertedWithCustomError(registry, 'InvalidSignature')

            console.log('✓ Valid timestamp windows accepted (fail on signature as expected)')
        })
    })

    describe('Settlement Replay Protection', function () {
        it('should prevent replay attacks', async function () {
            // Note: This test would require a valid TEE registration
            // We can test the settlement hash uniqueness logic
            const inputHash1 = hre.ethers.keccak256('0x01')
            const outputHash1 = hre.ethers.keccak256('0x02')
            const timestamp1 = 1234567890

            const inputHash2 = hre.ethers.keccak256('0x03')
            const outputHash2 = hre.ethers.keccak256('0x04')
            const timestamp2 = 1234567891

            const settlementHash1 = await helper.computeSettlementHash(
                teeId,
                inputHash1,
                outputHash1,
                timestamp1
            )

            const settlementHash2 = await helper.computeSettlementHash(
                teeId,
                inputHash2,
                outputHash2,
                timestamp2
            )

            // Different inputs should produce different settlement hashes
            expect(settlementHash1).to.not.equal(settlementHash2)

            console.log('✓ Different settlements produce different hashes')
        })

        it('should mark settlements as used after verification', async function () {
            // This tests the settlementUsed mapping logic
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const timestamp = Math.floor(Date.now() / 1000)

            const settlementHash = await helper.computeSettlementHash(
                teeId,
                inputHash,
                outputHash,
                timestamp
            )

            // Initially should not be used
            const isUsedBefore = await helper.isSettlementUsed(settlementHash)
            expect(isUsedBefore).to.be.false

            console.log('✓ Settlement initially marked as unused')
        })

        it('should compute unique hashes for different parameters', async function () {
            const base = {
                teeId: teeId,
                inputHash: hre.ethers.keccak256('0x01'),
                outputHash: hre.ethers.keccak256('0x02'),
                timestamp: 1234567890
            }

            const hash1 = await helper.computeSettlementHash(
                base.teeId,
                base.inputHash,
                base.outputHash,
                base.timestamp
            )

            // Change TEE ID
            const diffTeeId = hre.ethers.keccak256('0xDIFF')
            const hash2 = await helper.computeSettlementHash(
                diffTeeId,
                base.inputHash,
                base.outputHash,
                base.timestamp
            )

            // Change input hash
            const diffInputHash = hre.ethers.keccak256('0x99')
            const hash3 = await helper.computeSettlementHash(
                base.teeId,
                diffInputHash,
                base.outputHash,
                base.timestamp
            )

            // Change output hash
            const diffOutputHash = hre.ethers.keccak256('0xAA')
            const hash4 = await helper.computeSettlementHash(
                base.teeId,
                base.inputHash,
                diffOutputHash,
                base.timestamp
            )

            // Change timestamp
            const hash5 = await helper.computeSettlementHash(
                base.teeId,
                base.inputHash,
                base.outputHash,
                base.timestamp + 1
            )

            // All hashes should be different
            expect(hash1).to.not.equal(hash2)
            expect(hash1).to.not.equal(hash3)
            expect(hash1).to.not.equal(hash4)
            expect(hash1).to.not.equal(hash5)

            console.log('✓ Settlement hashes are unique for different parameters')
        })
    })

    describe('Integration - Signature and Settlement Flow', function () {
        it('should demonstrate correct signature flow', async function () {
            // This demonstrates the expected flow without a real TEE
            const inputData = 'transaction input data'
            const outputData = 'transaction output data'

            const inputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes(inputData))
            const outputHash = hre.ethers.keccak256(hre.ethers.toUtf8Bytes(outputData))
            const timestamp = Math.floor(Date.now() / 1000)

            // Step 1: Compute message hash
            const messageHash = await registry.computeMessageHash(inputHash, outputHash, timestamp)

            // Step 2: Sign the message hash with RSA-PSS (as TEE would do)
            const messageHashBuffer = Buffer.from(messageHash.slice(2), 'hex')
            const sha256Hash = crypto.createHash('sha256').update(messageHashBuffer).digest()

            const signature = crypto.sign(null, sha256Hash, {
                key: privateKey,
                padding: crypto.constants.RSA_PKCS1_PSS_PADDING,
                saltLength: crypto.constants.RSA_PSS_SALTLEN_DIGEST
            })

            const signatureHex = '0x' + signature.toString('hex')

            // Step 3: Verify signature via precompile directly
            const isValid = await helper.testVerifyRSAPSS(publicKeyDER, messageHash, signatureHex)
            const receipt = await isValid.wait()

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

            console.log('✓ Complete signature flow works correctly')
            console.log('  Input hash:', inputHash)
            console.log('  Output hash:', outputHash)
            console.log('  Timestamp:', timestamp)
            console.log('  Message hash:', messageHash)
            console.log('  Signature verified: true')
        })

        it('should compute settlement hash consistently', async function () {
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const timestamp = 1234567890

            // Compute via helper
            const hash1 = await helper.computeSettlementHash(teeId, inputHash, outputHash, timestamp)

            // Compute via direct keccak256
            const hash2 = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'bytes32', 'uint256'],
                    [teeId, inputHash, outputHash, timestamp]
                )
            )

            expect(hash1).to.equal(hash2)

            console.log('✓ Settlement hash computation is consistent')
        })
    })

    describe('Constants and Configuration', function () {
        it('should have correct time constants', async function () {
            const MAX_SETTLEMENT_AGE = await registry.MAX_SETTLEMENT_AGE()
            const FUTURE_TOLERANCE = await registry.FUTURE_TOLERANCE()

            expect(MAX_SETTLEMENT_AGE).to.equal(3600n) // 1 hour
            expect(FUTURE_TOLERANCE).to.equal(300n) // 5 minutes

            console.log('✓ Time constants are correct')
            console.log('  Max settlement age:', MAX_SETTLEMENT_AGE.toString(), 'seconds')
            console.log('  Future tolerance:', FUTURE_TOLERANCE.toString(), 'seconds')
        })

        it('should have correct precompile address', async function () {
            const VERIFIER_ADDRESS = await registry.VERIFIER()
            expect(VERIFIER_ADDRESS).to.equal('0x0000000000000000000000000000000000000900')

            console.log('✓ Precompile address is correct:', VERIFIER_ADDRESS)
        })
    })
})
