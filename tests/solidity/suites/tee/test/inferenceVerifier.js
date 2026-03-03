const { expect } = require('chai')
const crypto = require('crypto')
const truffleAssert = require('truffle-assertions')
const TEERegistry = artifacts.require('TEERegistry')
const TEEInferenceVerifier = artifacts.require('TEEInferenceVerifier')

contract('TEEInferenceVerifier', function (accounts) {
    let owner, teeOperator, user1
    let registry, verifier
    let teeId, publicKey

    // Test constants
    const INPUT_HASH = web3.utils.keccak256('input-data')
    const OUTPUT_HASH = web3.utils.keccak256('output-data')
    const MOCK_SIGNATURE = '0xcafebabe'

    before(async () => {
        [owner, teeOperator, user1] = accounts

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

    describe('Initialization', function () {
        it('should initialize with correct registry', async function () {
            const registryAddress = await verifier.registry()
            expect(registryAddress).to.equal(registry.address)

            console.log('✓ Registry address set correctly')
        })

        it('should grant DEFAULT_ADMIN_ROLE to deployer', async function () {
            const DEFAULT_ADMIN_ROLE = await verifier.DEFAULT_ADMIN_ROLE()
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, owner)).to.be.true

            console.log('✓ Admin role granted correctly')
        })

        it('should have correct precompile address', async function () {
            const VERIFIER_ADDRESS = await verifier.VERIFIER()
            expect(VERIFIER_ADDRESS).to.equal('0x0000000000000000000000000000000000000900')

            console.log('✓ Precompile address is correct')
        })

        it('should have correct MAX_INFERENCE_AGE', async function () {
            const maxAge = await verifier.MAX_INFERENCE_AGE()
            expect(maxAge.toString()).to.equal('3600') // 1 hour in seconds

            console.log('✓ MAX_INFERENCE_AGE is 1 hour')
        })

        it('should have correct FUTURE_TOLERANCE', async function () {
            const tolerance = await verifier.FUTURE_TOLERANCE()
            expect(tolerance.toString()).to.equal('300') // 5 minutes in seconds

            console.log('✓ FUTURE_TOLERANCE is 5 minutes')
        })
    })

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

            console.log('✓ Registry updated successfully')
        })

        it('should reject non-admin updating registry', async function () {
            const newRegistry = await TEERegistry.new()

            await truffleAssert.reverts(
                verifier.setRegistry(newRegistry.address, { from: user1 })
            )

            console.log('✓ Non-admin cannot update registry')
        })
    })

    describe('Message Hash Computation', function () {
        it('should compute message hash correctly', async function () {
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

            console.log('✓ Message hash computation correct')
        })

        it('should produce different hashes for different inputs', async function () {
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

            console.log('✓ Different inputs produce different hashes')
        })

        it('should produce different hashes for different timestamps', async function () {
            const timestamp1 = Math.floor(Date.now() / 1000)
            const timestamp2 = timestamp1 + 100

            const hash1 = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp1
            )

            const hash2 = await verifier.computeMessageHash(
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp2
            )

            expect(hash1).to.not.equal(hash2)

            console.log('✓ Different timestamps produce different hashes')
        })
    })

    describe('Signature Verification', function () {
        it('should return false for inactive TEE', async function () {
            const timestamp = Math.floor(Date.now() / 1000)

            // teeId is not registered, so should return false
            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                timestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false

            console.log('✓ Inactive TEE returns false')
        })

        it('should return false for timestamp too old', async function () {
            const oldTimestamp = Math.floor(Date.now() / 1000) - 7200 // 2 hours ago

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                oldTimestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false

            console.log('✓ Old timestamp returns false')
        })

        it('should return false for timestamp too far in future', async function () {
            const futureTimestamp = Math.floor(Date.now() / 1000) + 600 // 10 minutes ahead

            const result = await verifier.verifySignature(
                teeId,
                INPUT_HASH,
                OUTPUT_HASH,
                futureTimestamp,
                MOCK_SIGNATURE
            )

            expect(result).to.be.false

            console.log('✓ Future timestamp returns false')
        })

        // Note: Full signature verification requires the precompile
        // which is only available on the actual og-evm chain
    })

    describe('Access Control', function () {
        it('should enforce DEFAULT_ADMIN_ROLE for setRegistry', async function () {
            await truffleAssert.reverts(
                verifier.setRegistry(registry.address, { from: user1 })
            )

            console.log('✓ Admin role enforced for setRegistry')
        })

        it('should allow role management by admin', async function () {
            const DEFAULT_ADMIN_ROLE = await verifier.DEFAULT_ADMIN_ROLE()

            // Grant role
            await verifier.grantRole(DEFAULT_ADMIN_ROLE, user1)
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, user1)).to.be.true

            // Revoke role
            await verifier.revokeRole(DEFAULT_ADMIN_ROLE, user1)
            expect(await verifier.hasRole(DEFAULT_ADMIN_ROLE, user1)).to.be.false

            console.log('✓ Role management works correctly')
        })
    })
})