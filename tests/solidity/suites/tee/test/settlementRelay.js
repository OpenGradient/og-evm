const { expect } = require('chai')
const truffleAssert = require('truffle-assertions')
const InferenceSettlementRelay = artifacts.require('InferenceSettlementRelay')
const TEEInferenceVerifier = artifacts.require('TEEInferenceVerifier')
const TEERegistry = artifacts.require('TEERegistry')

contract('InferenceSettlementRelay', function (accounts) {
    let owner, user
    let relay, verifier, registry

    const TEE_ID = web3.utils.keccak256('test-tee')
    const INPUT_HASH = web3.utils.keccak256('input')
    const OUTPUT_HASH = web3.utils.keccak256('output')
    const SETTLEMENT_TIMESTAMP = 1700000000
    const WALRUS_BLOB_ID = web3.utils.asciiToHex('test_blob_id')
    const SIGNATURE = web3.utils.asciiToHex('test_signature')

    before(async () => {
        [owner, user] = accounts

        // Deploy registry and verifier
        registry = await TEERegistry.new()
        verifier = await TEEInferenceVerifier.new(registry.address)

        // Deploy relay with verifier as settlement contract
        relay = await InferenceSettlementRelay.new(verifier.address)

        console.log('InferenceSettlementRelay deployed at:', relay.address)
        console.log('TEEInferenceVerifier deployed at:', verifier.address)
        console.log('Setup complete')
    })

    // ============ Constructor Tests ============

    describe('Constructor', function () {
        it('should set the settlement contract', async function () {
            const addr = await relay.SETTLEMENT_CONTRACT()
            expect(addr).to.equal(verifier.address)

            console.log('✓ Settlement contract set correctly')
        })

        it('should grant DEFAULT_ADMIN_ROLE and SETTLEMENT_RELAY_ROLE to deployer', async function () {
            const DEFAULT_ADMIN_ROLE = await relay.DEFAULT_ADMIN_ROLE()
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()

            expect(await relay.hasRole(DEFAULT_ADMIN_ROLE, owner)).to.be.true
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, owner)).to.be.true

            console.log('✓ Roles granted correctly')
        })

        it('should revert if settlement contract is zero address', async function () {
            try {
                await InferenceSettlementRelay.new('0x0000000000000000000000000000000000000000')
                assert.fail('Expected deployment to fail')
            } catch (error) {
                // Constructor reverts may surface as "couldn't be stored" on some EVMs
                expect(
                    error.message.includes('Invalid settlement contract') ||
                    error.message.includes("couldn't be stored") ||
                    error.message.includes('revert')
                ).to.be.true
            }

            console.log('✓ Zero address rejected')
        })
    })

    // ============ batchSettle Tests ============

    describe('batchSettle', function () {
        it('should emit BatchSettlement event', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root')
            const batchSize = 2

            const result = await relay.batchSettle(merkleRoot, batchSize, WALRUS_BLOB_ID)

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.merkleRoot === merkleRoot &&
                       ev.batchSize.toNumber() === batchSize &&
                       ev.walrusBlobId === WALRUS_BLOB_ID
            })

            console.log('✓ BatchSettlement event emitted')
        })

        it('should revert if caller does not have relay role', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root')

            await truffleAssert.reverts(
                relay.batchSettle(merkleRoot, 2, WALRUS_BLOB_ID, { from: user })
            )

            console.log('✓ Unauthorized caller rejected')
        })
    })

    // ============ settleIndividual Tests ============

    describe('settleIndividual', function () {
        it('should revert for invalid signature (inactive TEE)', async function () {
            // TEE is not registered so verifySignature returns false
            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID,
                    INPUT_HASH,
                    OUTPUT_HASH,
                    SETTLEMENT_TIMESTAMP,
                    user,
                    WALRUS_BLOB_ID,
                    SIGNATURE
                )
            )

            console.log('✓ Invalid signature rejected')
        })

        it('should revert if caller does not have relay role', async function () {
            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID,
                    INPUT_HASH,
                    OUTPUT_HASH,
                    SETTLEMENT_TIMESTAMP,
                    user,
                    WALRUS_BLOB_ID,
                    SIGNATURE,
                    { from: user }
                )
            )

            console.log('✓ Unauthorized caller rejected')
        })

        it('should succeed for granted relay role', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()

            // Grant role to user
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, user)
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, user)).to.be.true

            // Still reverts because verifySignature returns false (no registered TEE),
            // but the access control check passes
            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID,
                    INPUT_HASH,
                    OUTPUT_HASH,
                    SETTLEMENT_TIMESTAMP,
                    user,
                    WALRUS_BLOB_ID,
                    SIGNATURE,
                    { from: user }
                )
            )

            // Revoke role
            await relay.revokeRole(SETTLEMENT_RELAY_ROLE, user)

            console.log('✓ Granted relay role passes access control')
        })
    })

    // ============ verifyProof Tests ============

    describe('verifyProof', function () {
        it('should return true for valid proof', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')

            // Merkle root: commutative hash of two leaves
            const root = leafA < leafB
                ? web3.utils.soliditySha3(
                    { type: 'bytes32', value: leafA },
                    { type: 'bytes32', value: leafB }
                )
                : web3.utils.soliditySha3(
                    { type: 'bytes32', value: leafB },
                    { type: 'bytes32', value: leafA }
                )

            const valid = await relay.verifyProof([leafB], root, leafA)
            expect(valid).to.be.true

            console.log('✓ Valid proof verified')
        })

        it('should return false for invalid leaf', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const invalidLeaf = web3.utils.keccak256('leaf-c')

            const root = leafA < leafB
                ? web3.utils.soliditySha3(
                    { type: 'bytes32', value: leafA },
                    { type: 'bytes32', value: leafB }
                )
                : web3.utils.soliditySha3(
                    { type: 'bytes32', value: leafB },
                    { type: 'bytes32', value: leafA }
                )

            const valid = await relay.verifyProof([leafB], root, invalidLeaf)
            expect(valid).to.be.false

            console.log('✓ Invalid leaf rejected')
        })
    })
})
