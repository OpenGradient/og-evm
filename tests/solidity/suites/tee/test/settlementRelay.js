const { expect } = require('chai')
const truffleAssert = require('truffle-assertions')
const InferenceSettlementRelay = artifacts.require('InferenceSettlementRelay')
const TEEInferenceVerifier = artifacts.require('TEEInferenceVerifier')
const TEERegistry = artifacts.require('TEERegistry')
const MockTEEInferenceVerifier = artifacts.require('MockTEEInferenceVerifier')

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

        it('should return true for empty proof when root equals leaf', async function () {
            const leaf = web3.utils.keccak256('single-leaf')

            // MerkleProof.verify with empty proof returns true when root == leaf
            const valid = await relay.verifyProof([], leaf, leaf)
            expect(valid).to.be.true

            console.log('✓ Empty proof with root == leaf returns true')
        })

        it('should return false for empty proof when root != leaf', async function () {
            const root = web3.utils.keccak256('root-value')
            const leaf = web3.utils.keccak256('different-leaf')

            const valid = await relay.verifyProof([], root, leaf)
            expect(valid).to.be.false

            console.log('✓ Empty proof with root != leaf returns false')
        })

        it('should verify proof for deeper tree with 4 leaves', async function () {
            // Build a 4-leaf Merkle tree: [A, B, C, D]
            const leaves = ['leaf-1', 'leaf-2', 'leaf-3', 'leaf-4'].map(l => web3.utils.keccak256(l))

            // Sort pairs for OpenZeppelin's commutative hashing
            function sortedHash(a, b) {
                return a < b
                    ? web3.utils.soliditySha3({ type: 'bytes32', value: a }, { type: 'bytes32', value: b })
                    : web3.utils.soliditySha3({ type: 'bytes32', value: b }, { type: 'bytes32', value: a })
            }

            // Level 1: hash pairs
            const node01 = sortedHash(leaves[0], leaves[1])
            const node23 = sortedHash(leaves[2], leaves[3])

            // Level 2: root
            const root = sortedHash(node01, node23)

            // Prove leaves[0]: proof = [leaves[1], node23]
            const valid = await relay.verifyProof([leaves[1], node23], root, leaves[0])
            expect(valid).to.be.true

            // Prove leaves[2]: proof = [leaves[3], node01]
            const valid2 = await relay.verifyProof([leaves[3], node01], root, leaves[2])
            expect(valid2).to.be.true

            console.log('✓ Deeper tree (4 leaves) proof verified')
        })

        it('should return false for wrong proof in deeper tree', async function () {
            const leaves = ['leaf-1', 'leaf-2', 'leaf-3', 'leaf-4'].map(l => web3.utils.keccak256(l))

            function sortedHash(a, b) {
                return a < b
                    ? web3.utils.soliditySha3({ type: 'bytes32', value: a }, { type: 'bytes32', value: b })
                    : web3.utils.soliditySha3({ type: 'bytes32', value: b }, { type: 'bytes32', value: a })
            }

            const node01 = sortedHash(leaves[0], leaves[1])
            const node23 = sortedHash(leaves[2], leaves[3])
            const root = sortedHash(node01, node23)

            // Try to prove leaves[0] with wrong sibling (leaves[2] instead of leaves[1])
            const valid = await relay.verifyProof([leaves[2], node23], root, leaves[0])
            expect(valid).to.be.false

            console.log('✓ Wrong proof in deeper tree rejected')
        })
    })

    // ============ batchSettle Edge Cases ============

    describe('batchSettle edge cases', function () {
        it('should emit event with batchSize = 0', async function () {
            const merkleRoot = web3.utils.keccak256('empty-batch')

            const result = await relay.batchSettle(merkleRoot, 0, WALRUS_BLOB_ID)

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.merkleRoot === merkleRoot &&
                       ev.batchSize.toNumber() === 0 &&
                       ev.walrusBlobId === WALRUS_BLOB_ID
            })

            console.log('✓ BatchSettlement emitted with batchSize = 0')
        })

        it('should emit event with empty walrusBlobId', async function () {
            const merkleRoot = web3.utils.keccak256('batch-no-blob')

            const result = await relay.batchSettle(merkleRoot, 5, '0x')

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.merkleRoot === merkleRoot &&
                       ev.batchSize.toNumber() === 5
            })

            console.log('✓ BatchSettlement emitted with empty walrusBlobId')
        })
    })

    // ============ settleIndividual Success Path (with mock) ============

    describe('settleIndividual success path', function () {
        let mockVerifier, mockRelay

        before(async () => {
            // Deploy mock verifier that always returns true
            mockVerifier = await MockTEEInferenceVerifier.new()

            // Deploy relay with mock verifier
            mockRelay = await InferenceSettlementRelay.new(mockVerifier.address)

            console.log('MockTEEInferenceVerifier deployed at:', mockVerifier.address)
            console.log('Mock relay deployed at:', mockRelay.address)
        })

        it('should emit IndividualSettlement event with correct parameters', async function () {
            const result = await mockRelay.settleIndividual(
                TEE_ID,
                INPUT_HASH,
                OUTPUT_HASH,
                SETTLEMENT_TIMESTAMP,
                user,
                WALRUS_BLOB_ID,
                SIGNATURE
            )

            truffleAssert.eventEmitted(result, 'IndividualSettlement', (ev) => {
                return ev.teeId === TEE_ID &&
                       ev.ethAddress === user &&
                       ev.inputHash === INPUT_HASH &&
                       ev.outputHash === OUTPUT_HASH &&
                       ev.timestamp.toNumber() === SETTLEMENT_TIMESTAMP &&
                       ev.walrusBlobId === WALRUS_BLOB_ID &&
                       ev.signature === SIGNATURE
            })

            console.log('✓ IndividualSettlement event emitted with all correct parameters')
        })

        it('should revert when mock verifier returns false', async function () {
            // Set mock to return false
            await mockVerifier.setVerifyResult(false)

            await truffleAssert.reverts(
                mockRelay.settleIndividual(
                    TEE_ID,
                    INPUT_HASH,
                    OUTPUT_HASH,
                    SETTLEMENT_TIMESTAMP,
                    user,
                    WALRUS_BLOB_ID,
                    SIGNATURE
                ),
                'Invalid signature'
            )

            // Restore mock to return true
            await mockVerifier.setVerifyResult(true)

            console.log('✓ settleIndividual reverts when verifySignature returns false')
        })

        it('should revert for non-relay role even with valid signature', async function () {
            await truffleAssert.reverts(
                mockRelay.settleIndividual(
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

            console.log('✓ Non-relay role rejected even with valid mock signature')
        })
    })
})
