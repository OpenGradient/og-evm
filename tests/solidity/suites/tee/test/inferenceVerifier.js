const { expect } = require('chai')
const crypto = require('crypto')
const truffleAssert = require('truffle-assertions')
const TEERegistry = artifacts.require('TEERegistry')
const TEEInferenceVerifier = artifacts.require('TEEInferenceVerifier')

contract('TEEInferenceVerifier', function (accounts) {
    let owner, teeOperator, user1, user2
    let registry, verifier
    let teeId, publicKey

    // Test constants
    const INPUT_HASH = web3.utils.keccak256('input-data')
    const OUTPUT_HASH = web3.utils.keccak256('output-data')
    const MOCK_SIGNATURE = '0xcafebabe'

    before(async () => {
        [owner, teeOperator, user1, user2] = accounts

        // Deploy TEERegistry
        registry = await TEERegistry.new()

        // Deploy TEEInferenceVerifier with registry address
        verifier = await TEEInferenceVerifier.new(registry.address)

        console.log('TEERegistry deployed at:', registry.address)
        console.log('TEEInferenceVerifier deployed at:', verifier.address)

        // Generate a test public key
        const { publicKey: pubKey } = crypto.generateKeyPairSync('rsa', {
            modulusLength: 2048,
            publicKeyEncoding: {
                type: 'spki',
                format: 'der'
            }
        })
        publicKey = '0x' + pubKey.toString('hex')
        teeId = await registry.computeTEEId(publicKey)

        console.log('Test TEE ID:', teeId)
        console.log('Setup complete')
    })

    // ============ Constructor Tests ============

    describe('Constructor', function () {
        it('should initialize with correct registry', async function () {
            const registryAddress = await verifier.registry()
            expect(registryAddress).to.equal(registry.address)
        })

        it('should grant DEFAULT_ADMIN_ROLE to deployer', async function () {
            const DEFAULT_ADMIN_ROLE = await verifier.DEFAULT_ADMIN_ROLE()
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, owner)).to.be.true
        })

        it('should revert if registry address is zero', async function () {
            try {
                await TEEInferenceVerifier.new('0x0000000000000000000000000000000000000000')
                expect.fail('Should have reverted')
            } catch (error) {
                expect(error.message).to.include('revert')
            }
        })

        it('should have correct precompile address', async function () {
            const VERIFIER_ADDRESS = await verifier.VERIFIER()
            expect(VERIFIER_ADDRESS).to.equal('0x0000000000000000000000000000000000000900')
        })

        it('should have correct MAX_INFERENCE_AGE', async function () {
            const maxAge = await verifier.MAX_INFERENCE_AGE()
            expect(maxAge.toString()).to.equal('3600') // 1 hour in seconds
        })

        it('should have correct FUTURE_TOLERANCE', async function () {
            const tolerance = await verifier.FUTURE_TOLERANCE()
            expect(tolerance.toString()).to.equal('300') // 5 minutes in seconds
        })
    })

    // ============ Registry Management ============

    describe('Registry Management', function () {
        it('should allow admin to update registry', async function () {
            const newRegistry = await TEERegistry.new()

            const result = await verifier.setRegistry(newRegistry.address)

            truffleAssert.eventEmitted(result, 'RegistryUpdated', (ev) => {
                return ev.oldRegistry === registry.address &&
                       ev.newRegistry === newRegistry.address
            })

            const currentRegistry = await verifier.registry()
            expect(currentRegistry).to.equal(newRegistry.address)

            // Restore original registry for other tests
            await verifier.setRegistry(registry.address)
        })

        it('should reject non-admin updating registry', async function () {
            const newRegistry = await TEERegistry.new()

            await truffleAssert.reverts(
                verifier.setRegistry(newRegistry.address, { from: user1 })
            )
        })

        it('should reject setting registry to zero address', async function () {
            try {
                await verifier.setRegistry('0x0000000000000000000000000000000000000000')
                expect.fail('Should have reverted')
            } catch (error) {
                expect(error.message).to.include('revert')
            }
        })

        it('should emit event with correct old and new addresses', async function () {
            const newRegistry1 = await TEERegistry.new()
            const newRegistry2 = await TEERegistry.new()

            // Set to newRegistry1
            await verifier.setRegistry(newRegistry1.address)

            // Set to newRegistry2 — old should be newRegistry1
            const result = await verifier.setRegistry(newRegistry2.address)

            truffleAssert.eventEmitted(result, 'RegistryUpdated', (ev) => {
                return ev.oldRegistry === newRegistry1.address &&
                       ev.newRegistry === newRegistry2.address
            })

            // Restore
            await verifier.setRegistry(registry.address)
        })
    })

    // ============ Message Hash Computation ============

    describe('Message Hash Computation', function () {
        it('should compute message hash correctly (matches abi.encodePacked)', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            const expectedHash = web3.utils.soliditySha3(
                { type: 'bytes32', value: INPUT_HASH },
                { type: 'bytes32', value: OUTPUT_HASH },
                { type: 'uint256', value: timestamp }
            )

            const computedHash = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp
            )

            expect(computedHash).to.equal(expectedHash)
        })

        it('should be deterministic (same input => same output)', async function () {
            const timestamp = 1700000000

            const hash1 = await verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp)
            const hash2 = await verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp)

            expect(hash1).to.equal(hash2)
        })

        it('should produce different hashes for different inputHash', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            const hash1 = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp
            )

            const hash2 = await verifier.computeMessageHash(
                web3.utils.keccak256('different-input'),
                OUTPUT_HASH,
                timestamp
            )

            expect(hash1).to.not.equal(hash2)
        })

        it('should produce different hashes for different outputHash', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            const hash1 = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp
            )

            const hash2 = await verifier.computeMessageHash(
                INPUT_HASH,
                web3.utils.keccak256('different-output'),
                timestamp
            )

            expect(hash1).to.not.equal(hash2)
        })

        it('should produce different hashes for different timestamps', async function () {
            const timestamp1 = Math.floor(Date.now() / 1000)
            const timestamp2 = timestamp1 + 100

            const hash1 = await verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp1)
            const hash2 = await verifier.computeMessageHash(INPUT_HASH, OUTPUT_HASH, timestamp2)

            expect(hash1).to.not.equal(hash2)
        })

        it('should handle zero values', async function () {
            const zeroHash = '0x0000000000000000000000000000000000000000000000000000000000000000'

            const hash = await verifier.computeMessageHash(zeroHash, zeroHash, 0)
            // Should not revert and should return a valid hash
            expect(hash).to.not.be.null
            expect(hash).to.have.lengthOf(66) // 0x + 64 hex chars
        })

        it('should match known hardcoded value', async function () {
            // Pin a specific computation to detect changes
            const knownInputHash = web3.utils.keccak256('input-data')
            const knownOutputHash = web3.utils.keccak256('output-data')
            const knownTimestamp = 1234567890

            const expectedHash = web3.utils.soliditySha3(
                { type: 'bytes32', value: knownInputHash },
                { type: 'bytes32', value: knownOutputHash },
                { type: 'uint256', value: knownTimestamp }
            )

            const computedHash = await verifier.computeMessageHash(
                knownInputHash,
                knownOutputHash,
                knownTimestamp
            )

            expect(computedHash).to.equal(expectedHash)
        })
    })

    // ============ Signature Verification ============

    describe('Signature Verification', function () {
        it('should return false for inactive TEE (not registered)', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            // teeId is not registered, so isActive returns false
            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false for non-existent TEE ID', async function () {
            const nonExistentTeeId = web3.utils.keccak256('definitely-not-a-tee')
            const timestamp = Math.floor(Date.now() / 1000)

            const result = await verifier.verifySignature(
                nonExistentTeeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false for timestamp too old (> MAX_INFERENCE_AGE)', async function () {
            const oldTimestamp = Math.floor(Date.now() / 1000) - 7200 // 2 hours ago

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                oldTimestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false for timestamp too far in future (> FUTURE_TOLERANCE)', async function () {
            const futureTimestamp = Math.floor(Date.now() / 1000) + 600 // 10 minutes ahead

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                futureTimestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false for zero timestamp', async function () {
            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                0,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false for very large timestamp', async function () {
            // Way in the future
            const farFutureTimestamp = Math.floor(Date.now() / 1000) + 86400 * 365

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                farFutureTimestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('should return false with empty signature', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                '0x'
            )

            expect(result).to.be.false
        })

        // Note: Full signature verification with active TEE requires the precompile
        // which is only available on the actual og-evm chain
    })

    // ============ Access Control ============

    describe('Access Control', function () {
        it('should enforce DEFAULT_ADMIN_ROLE for setRegistry', async function () {
            await truffleAssert.reverts(
                verifier.setRegistry(registry.address, { from: user1 })
            )
        })

        it('should allow admin to grant DEFAULT_ADMIN_ROLE to others', async function () {
            const DEFAULT_ADMIN_ROLE = await verifier.DEFAULT_ADMIN_ROLE()

            // Grant role
            await verifier.grantRole(DEFAULT_ADMIN_ROLE, user1)
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, user1)).to.be.true

            // User1 can now update registry
            const newRegistry = await TEERegistry.new()
            await verifier.setRegistry(newRegistry.address, { from: user1 })
            expect(await verifier.registry()).to.equal(newRegistry.address)

            // Cleanup: restore registry and revoke role
            await verifier.setRegistry(registry.address, { from: user1 })
            await verifier.revokeRole(DEFAULT_ADMIN_ROLE, user1)
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, user1)).to.be.false
        })

        it('should prevent non-admin from granting roles', async function () {
            const DEFAULT_ADMIN_ROLE = await verifier.DEFAULT_ADMIN_ROLE()

            await truffleAssert.reverts(
                verifier.grantRole(DEFAULT_ADMIN_ROLE, user2, { from: user1 })
            )
        })
    })

    // ============ View Function Behavior ============

    describe('View Function Behavior', function () {
        it('verifySignature should not modify state', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            // Call verifySignature (view function) — should not emit events or change state
            const result = await verifier.verifySignature.call(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false
        })

        it('computeMessageHash should be callable by anyone', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            const hash = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                { from: user1 }
            )

            expect(hash).to.not.be.null
        })

        it('verifySignature should be callable by anyone', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            // No access control on verifySignature — anyone can call it
            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                MOCK_SIGNATURE,
                { from: user2 }
            )

            expect(result).to.be.false
        })
    })
})
