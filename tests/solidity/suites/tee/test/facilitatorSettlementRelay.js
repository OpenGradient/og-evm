const { expect } = require('chai')
const truffleAssert = require('truffle-assertions')

const FacilitatorSettlementRelay = artifacts.require('FacilitatorSettlementRelay')
const MockSettlementContract = artifacts.require('MockSettlementContract')

contract('FacilitatorSettlementRelay', function (accounts) {
    let admin, relayOperator, user1, user2
    let relay, mockSettlement

    // Test constants
    const TEE_ID = web3.utils.keccak256('test-tee')
    const INPUT_HASH = web3.utils.keccak256('input-data')
    const OUTPUT_HASH = web3.utils.keccak256('output-data')
    const SETTLEMENT_TIMESTAMP = 1700000000
    const WALRUS_BLOB_ID = web3.utils.asciiToHex('walrus_blob_12345')
    const SIGNATURE = web3.utils.asciiToHex('mock_signature_bytes')

    before(async () => {
        [admin, relayOperator, user1, user2] = accounts

        // Deploy mock settlement contract
        mockSettlement = await MockSettlementContract.new()

        // Deploy relay with mock settlement
        relay = await FacilitatorSettlementRelay.new(mockSettlement.address)

        console.log('MockSettlementContract deployed at:', mockSettlement.address)
        console.log('FacilitatorSettlementRelay deployed at:', relay.address)
        console.log('Setup complete')
    })

    // ============ Constructor Tests ============

    describe('Constructor', function () {
        it('should set the settlement contract address', async function () {
            const addr = await relay.SETTLEMENT_CONTRACT()
            expect(addr).to.equal(mockSettlement.address)
        })

        it('should grant DEFAULT_ADMIN_ROLE to deployer', async function () {
            const DEFAULT_ADMIN_ROLE = await relay.DEFAULT_ADMIN_ROLE()
            expect(await relay.hasRole(DEFAULT_ADMIN_ROLE, admin)).to.be.true
        })

        it('should grant SETTLEMENT_RELAY_ROLE to deployer', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, admin)).to.be.true
        })

        it('should revert if settlement contract is zero address', async function () {
            await truffleAssert.reverts(
                FacilitatorSettlementRelay.new('0x0000000000000000000000000000000000000000'),
                'Invalid settlement contract'
            )
        })

        it('should have the correct SETTLEMENT_RELAY_ROLE hash', async function () {
            const role = await relay.SETTLEMENT_RELAY_ROLE()
            const expected = web3.utils.keccak256('SETTLEMENT_RELAY_ROLE')
            expect(role).to.equal(expected)
        })
    })

    // ============ Role Management ============

    describe('Role Management', function () {
        it('should allow admin to grant SETTLEMENT_RELAY_ROLE', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, relayOperator, { from: admin })
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, relayOperator)).to.be.true
        })

        it('should allow admin to revoke SETTLEMENT_RELAY_ROLE', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            // Grant first
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, user2, { from: admin })
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, user2)).to.be.true

            // Revoke
            await relay.revokeRole(SETTLEMENT_RELAY_ROLE, user2, { from: admin })
            expect(await relay.hasRole(SETTLEMENT_RELAY_ROLE, user2)).to.be.false
        })

        it('should prevent non-admin from granting roles', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            await truffleAssert.reverts(
                relay.grantRole(SETTLEMENT_RELAY_ROLE, user1, { from: user1 })
            )
        })
    })

    // ============ batchSettle Tests ============

    describe('batchSettle', function () {
        it('should emit BatchSettlement event with correct parameters', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-1')
            const batchSize = 10

            const result = await relay.batchSettle(merkleRoot, batchSize, WALRUS_BLOB_ID, { from: admin })

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.merkleRoot === merkleRoot &&
                       ev.batchSize.toString() === batchSize.toString() &&
                       ev.walrusBlobId === WALRUS_BLOB_ID
            })
        })

        it('should allow granted relay operator to batch settle', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, relayOperator, { from: admin })

            const merkleRoot = web3.utils.keccak256('batch-root-2')
            const result = await relay.batchSettle(merkleRoot, 5, WALRUS_BLOB_ID, { from: relayOperator })

            truffleAssert.eventEmitted(result, 'BatchSettlement')
        })

        it('should revert if caller lacks SETTLEMENT_RELAY_ROLE', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-3')
            await truffleAssert.reverts(
                relay.batchSettle(merkleRoot, 5, WALRUS_BLOB_ID, { from: user1 })
            )
        })

        it('should handle zero batch size', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-zero')
            const result = await relay.batchSettle(merkleRoot, 0, WALRUS_BLOB_ID, { from: admin })

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.batchSize.toString() === '0'
            })
        })

        it('should handle large batch size', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-large')
            const largeBatchSize = '1000000'
            const result = await relay.batchSettle(merkleRoot, largeBatchSize, WALRUS_BLOB_ID, { from: admin })

            truffleAssert.eventEmitted(result, 'BatchSettlement', (ev) => {
                return ev.batchSize.toString() === largeBatchSize
            })
        })

        it('should handle empty walrusBlobId', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-empty-blob')
            const result = await relay.batchSettle(merkleRoot, 1, '0x', { from: admin })
            truffleAssert.eventEmitted(result, 'BatchSettlement')
        })

        it('should allow same merkle root to be submitted multiple times', async function () {
            const merkleRoot = web3.utils.keccak256('batch-root-dup')
            await relay.batchSettle(merkleRoot, 3, WALRUS_BLOB_ID, { from: admin })
            const result = await relay.batchSettle(merkleRoot, 3, WALRUS_BLOB_ID, { from: admin })
            truffleAssert.eventEmitted(result, 'BatchSettlement')
        })
    })

    // ============ settleIndividual Tests ============

    describe('settleIndividual', function () {
        it('should emit IndividualSettlement event with correct parameters', async function () {
            const result = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )

            truffleAssert.eventEmitted(result, 'IndividualSettlement', (ev) => {
                return ev.teeId === TEE_ID &&
                       ev.ethAddress === user1 &&
                       ev.inputHash === INPUT_HASH &&
                       ev.outputHash === OUTPUT_HASH &&
                       ev.timestamp.toString() === SETTLEMENT_TIMESTAMP.toString() &&
                       ev.walrusBlobId === WALRUS_BLOB_ID &&
                       ev.signature === SIGNATURE
            })
        })

        it('should revert when signature verification fails', async function () {
            await mockSettlement.setShouldVerify(false)

            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                    user1, WALRUS_BLOB_ID, SIGNATURE,
                    { from: admin }
                ),
                'Invalid signature'
            )

            // Restore
            await mockSettlement.setShouldVerify(true)
        })

        it('should revert if caller lacks SETTLEMENT_RELAY_ROLE', async function () {
            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                    user1, WALRUS_BLOB_ID, SIGNATURE,
                    { from: user1 }
                )
            )
        })

        it('should succeed for granted relay operator', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, relayOperator, { from: admin })

            const result = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: relayOperator }
            )

            truffleAssert.eventEmitted(result, 'IndividualSettlement')
        })

        it('should settle with different ETH addresses', async function () {
            const result1 = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )
            truffleAssert.eventEmitted(result1, 'IndividualSettlement', (ev) => {
                return ev.ethAddress === user1
            })

            const result2 = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user2, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )
            truffleAssert.eventEmitted(result2, 'IndividualSettlement', (ev) => {
                return ev.ethAddress === user2
            })
        })

        it('should allow settling same data multiple times (no replay protection)', async function () {
            await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )

            // Second identical settlement should also succeed
            const result = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )
            truffleAssert.eventEmitted(result, 'IndividualSettlement')
        })

        it('should handle empty walrusBlobId', async function () {
            const result = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, '0x', SIGNATURE,
                { from: admin }
            )
            truffleAssert.eventEmitted(result, 'IndividualSettlement')
        })

        it('should revert after relay role is revoked', async function () {
            const SETTLEMENT_RELAY_ROLE = await relay.SETTLEMENT_RELAY_ROLE()

            // Grant to user2 then revoke
            await relay.grantRole(SETTLEMENT_RELAY_ROLE, user2, { from: admin })
            await relay.revokeRole(SETTLEMENT_RELAY_ROLE, user2, { from: admin })

            await truffleAssert.reverts(
                relay.settleIndividual(
                    TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                    user1, WALRUS_BLOB_ID, SIGNATURE,
                    { from: user2 }
                )
            )
        })
    })

    // ============ verifyProof Tests ============

    describe('verifyProof', function () {
        // Helper: OpenZeppelin MerkleProof uses commutative hash (sorted pairs)
        function commutativeKeccak256(a, b) {
            if (a.toLowerCase() < b.toLowerCase()) {
                return web3.utils.keccak256(a + b.slice(2))
            } else {
                return web3.utils.keccak256(b + a.slice(2))
            }
        }

        it('should return true for valid two-leaf proof', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const root = commutativeKeccak256(leafA, leafB)

            const valid = await relay.verifyProof([leafB], root, leafA)
            expect(valid).to.be.true
        })

        it('should return false for invalid leaf', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const invalidLeaf = web3.utils.keccak256('leaf-c')
            const root = commutativeKeccak256(leafA, leafB)

            const valid = await relay.verifyProof([leafB], root, invalidLeaf)
            expect(valid).to.be.false
        })

        it('should return false for wrong root', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const wrongRoot = web3.utils.keccak256('wrong-root')

            const valid = await relay.verifyProof([leafB], wrongRoot, leafA)
            expect(valid).to.be.false
        })

        it('should return false for empty proof against non-root leaf', async function () {
            const leaf = web3.utils.keccak256('leaf')
            const root = web3.utils.keccak256('different-root')

            const valid = await relay.verifyProof([], root, leaf)
            expect(valid).to.be.false
        })

        it('should return true for single-leaf tree (leaf == root, empty proof)', async function () {
            const leaf = web3.utils.keccak256('only-leaf')

            const valid = await relay.verifyProof([], leaf, leaf)
            expect(valid).to.be.true
        })

        it('should verify four-leaf Merkle tree', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const leafC = web3.utils.keccak256('leaf-c')
            const leafD = web3.utils.keccak256('leaf-d')

            // Build tree: layer1 = [hash(A,B), hash(C,D)], root = hash(layer1[0], layer1[1])
            const hashAB = commutativeKeccak256(leafA, leafB)
            const hashCD = commutativeKeccak256(leafC, leafD)
            const root = commutativeKeccak256(hashAB, hashCD)

            // Proof for leafA: [leafB, hashCD]
            const valid = await relay.verifyProof([leafB, hashCD], root, leafA)
            expect(valid).to.be.true

            // Proof for leafC: [leafD, hashAB]
            const validC = await relay.verifyProof([leafD, hashAB], root, leafC)
            expect(validC).to.be.true
        })

        it('should reject proof with wrong sibling', async function () {
            const leafA = web3.utils.keccak256('leaf-a')
            const leafB = web3.utils.keccak256('leaf-b')
            const leafC = web3.utils.keccak256('leaf-c')
            const leafD = web3.utils.keccak256('leaf-d')

            const hashAB = commutativeKeccak256(leafA, leafB)
            const hashCD = commutativeKeccak256(leafC, leafD)
            const root = commutativeKeccak256(hashAB, hashCD)

            // Wrong proof: using leafC as sibling instead of leafB
            const valid = await relay.verifyProof([leafC, hashCD], root, leafA)
            expect(valid).to.be.false
        })
    })

    // ============ Event Indexing Tests ============

    describe('Event Indexing', function () {
        it('should index merkleRoot in BatchSettlement', async function () {
            const merkleRoot = web3.utils.keccak256('indexed-batch')
            const result = await relay.batchSettle(merkleRoot, 1, WALRUS_BLOB_ID, { from: admin })

            // Verify the first topic (after event signature) is the indexed merkleRoot
            const log = result.receipt.rawLogs[0]
            expect(log.topics.length).to.be.at.least(2)
            expect(log.topics[1]).to.equal(merkleRoot)
        })

        it('should index teeId and ethAddress in IndividualSettlement', async function () {
            const result = await relay.settleIndividual(
                TEE_ID, INPUT_HASH, OUTPUT_HASH, SETTLEMENT_TIMESTAMP,
                user1, WALRUS_BLOB_ID, SIGNATURE,
                { from: admin }
            )

            const log = result.receipt.rawLogs[0]
            // topics[0] = event sig, topics[1] = teeId, topics[2] = ethAddress (padded)
            expect(log.topics.length).to.be.at.least(3)
            expect(log.topics[1]).to.equal(TEE_ID)
            expect(log.topics[2]).to.equal(
                '0x' + user1.slice(2).toLowerCase().padStart(64, '0')
            )
        })
    })
})
