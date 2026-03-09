const { expect } = require('chai')
const crypto = require('crypto')
const truffleAssert = require('truffle-assertions')
const MockTEERegistry = artifacts.require('MockTEERegistry')

contract('TEERegistry Lifecycle & Queries', function (accounts) {
    let owner, teeOperator, user1, user2
    let registry

    // Test TEE data
    const TEE_TYPE_NITRO = 1
    const ENDPOINT = 'https://tee.example.com'
    const PCR_HASH = web3.utils.keccak256('0x' + Buffer.alloc(48, 0x01).toString('hex') +
        Buffer.alloc(48, 0x02).toString('hex') +
        Buffer.alloc(48, 0x03).toString('hex'))

    let publicKey1, publicKey2, tlsCert1, tlsCert2
    let teeId1, teeId2

    before(async () => {
        [owner, teeOperator, user1, user2] = accounts

        // Deploy MockTEERegistry
        registry = await MockTEERegistry.new()

        // Grant TEE_OPERATOR role to teeOperator
        const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()
        await registry.grantRole(TEE_OPERATOR_ROLE, teeOperator)

        // Add TEE type and approve PCR measurements
        await registry.addTEEType(TEE_TYPE_NITRO, 'AWS Nitro')

        // Approve the PCR so activateTEE() passes _requirePCRValidForTEE
        const pcrs = {
            pcr0: '0x' + Buffer.alloc(48, 0x01).toString('hex'),
            pcr1: '0x' + Buffer.alloc(48, 0x02).toString('hex'),
            pcr2: '0x' + Buffer.alloc(48, 0x03).toString('hex')
        }
        await registry.approvePCR(pcrs, 'v1.0.0', TEE_TYPE_NITRO)

        // Generate test keys
        const keyPair1 = crypto.generateKeyPairSync('rsa', {
            modulusLength: 2048,
            publicKeyEncoding: { type: 'spki', format: 'der' }
        })
        publicKey1 = '0x' + keyPair1.publicKey.toString('hex')
        tlsCert1 = '0x' + Buffer.alloc(100, 0xAA).toString('hex')
        teeId1 = await registry.computeTEEId(publicKey1)

        const keyPair2 = crypto.generateKeyPairSync('rsa', {
            modulusLength: 2048,
            publicKeyEncoding: { type: 'spki', format: 'der' }
        })
        publicKey2 = '0x' + keyPair2.publicKey.toString('hex')
        tlsCert2 = '0x' + Buffer.alloc(100, 0xBB).toString('hex')
        teeId2 = await registry.computeTEEId(publicKey2)

        console.log('MockTEERegistry deployed at:', registry.address)
        console.log('TEE ID 1:', teeId1)
        console.log('TEE ID 2:', teeId2)
        console.log('Setup complete')
    })

    // Helper: register and activate a TEE via mock
    async function registerAndActivate(pubKey, tlsCert, paymentAddr, endpoint, teeType, pcrHash, from) {
        const result = await registry.registerTEEForTesting(
            pubKey, tlsCert, paymentAddr, endpoint, teeType, pcrHash, { from }
        )
        const teeId = await registry.computeTEEId(pubKey)
        // activateTEE must be called by the owner (msg.sender of registerTEEForTesting)
        await registry.activateTEE(teeId, { from })
        return teeId
    }

    // ============ deactivateTEE Tests ============

    describe('deactivateTEE', function () {
        let localTeeId

        before(async function () {
            // Register and activate TEE 1 via teeOperator (who is the owner)
            localTeeId = await registerAndActivate(
                publicKey1, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, PCR_HASH, teeOperator
            )
        })

        it('should allow owner to deactivate their TEE', async function () {
            const result = await registry.deactivateTEE(localTeeId, { from: teeOperator })

            truffleAssert.eventEmitted(result, 'TEEDeactivated', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await registry.isActive(localTeeId)).to.be.false

            console.log('✓ Owner deactivated TEE')
        })

        it('should remove TEE from active list after deactivation', async function () {
            const activeTEEs = await registry.getActiveTEEs()
            const found = activeTEEs.some(id => id === localTeeId)
            expect(found).to.be.false

            console.log('✓ TEE removed from active list')
        })

        it('should be a no-op when deactivating already inactive TEE', async function () {
            // TEE is already inactive from the previous test
            const result = await registry.deactivateTEE(localTeeId, { from: teeOperator })

            // No event should be emitted for a no-op
            truffleAssert.eventNotEmitted(result, 'TEEDeactivated')

            console.log('✓ Double-deactivation is a no-op')
        })

        it('should allow admin to deactivate any TEE', async function () {
            // Re-activate first
            await registry.activateTEE(localTeeId, { from: teeOperator })
            expect(await registry.isActive(localTeeId)).to.be.true

            // Admin (owner/accounts[0]) deactivates
            const result = await registry.deactivateTEE(localTeeId, { from: owner })

            truffleAssert.eventEmitted(result, 'TEEDeactivated', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await registry.isActive(localTeeId)).to.be.false

            console.log('✓ Admin deactivated TEE')
        })

        it('should revert when non-owner/non-admin deactivates', async function () {
            // Re-activate first
            await registry.activateTEE(localTeeId, { from: teeOperator })

            await truffleAssert.reverts(
                registry.deactivateTEE(localTeeId, { from: user1 })
            )

            console.log('✓ Non-owner/non-admin rejected')
        })

        it('should revert for non-existent teeId', async function () {
            const fakeTeeId = web3.utils.keccak256('0xDEADBEEF')

            await truffleAssert.reverts(
                registry.deactivateTEE(fakeTeeId, { from: owner })
            )

            console.log('✓ Non-existent teeId reverts')
        })
    })

    // ============ activateTEE Tests ============

    describe('activateTEE', function () {
        let localTeeId

        before(async function () {
            // Register TEE 2 (inactive by default from mock)
            await registry.registerTEEForTesting(
                publicKey2, tlsCert2, user2, ENDPOINT, TEE_TYPE_NITRO, PCR_HASH, { from: teeOperator }
            )
            localTeeId = teeId2
        })

        it('should allow owner to activate their deactivated TEE', async function () {
            // TEE starts inactive from mock registration
            expect(await registry.isActive(localTeeId)).to.be.false

            const result = await registry.activateTEE(localTeeId, { from: teeOperator })

            truffleAssert.eventEmitted(result, 'TEEActivated', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await registry.isActive(localTeeId)).to.be.true

            console.log('✓ Owner activated TEE')
        })

        it('should add TEE back to active list after activation', async function () {
            const activeTEEs = await registry.getActiveTEEs()
            const found = activeTEEs.some(id => id === localTeeId)
            expect(found).to.be.true

            console.log('✓ TEE added to active list')
        })

        it('should be a no-op when activating already active TEE', async function () {
            // TEE is already active from the previous test
            const result = await registry.activateTEE(localTeeId, { from: teeOperator })

            // No event should be emitted for a no-op
            truffleAssert.eventNotEmitted(result, 'TEEActivated')

            console.log('✓ Double-activation is a no-op')
        })

        it('should allow admin to activate any TEE', async function () {
            // Deactivate first
            await registry.deactivateTEE(localTeeId, { from: teeOperator })
            expect(await registry.isActive(localTeeId)).to.be.false

            // Admin (owner/accounts[0]) activates
            const result = await registry.activateTEE(localTeeId, { from: owner })

            truffleAssert.eventEmitted(result, 'TEEActivated', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await registry.isActive(localTeeId)).to.be.true

            console.log('✓ Admin activated TEE')
        })

        it('should revert when non-owner/non-admin activates', async function () {
            // Deactivate first
            await registry.deactivateTEE(localTeeId, { from: teeOperator })

            await truffleAssert.reverts(
                registry.activateTEE(localTeeId, { from: user1 })
            )

            console.log('✓ Non-owner/non-admin rejected')
        })

        it('should revert for non-existent teeId', async function () {
            const fakeTeeId = web3.utils.keccak256('0xDEADBEEF')

            await truffleAssert.reverts(
                registry.activateTEE(fakeTeeId, { from: owner })
            )

            console.log('✓ Non-existent teeId reverts')
        })
    })

    // ============ Query Function Tests ============

    describe('Query functions', function () {
        // At this point teeId1 is registered and active (from deactivateTEE tests, re-activated),
        // teeId2 is registered but may be inactive. We'll set known state.

        before(async function () {
            // Ensure teeId1 is active
            try {
                await registry.activateTEE(teeId1, { from: teeOperator })
            } catch (e) {
                // May already be active, ignore
            }
            // Ensure teeId2 is active
            try {
                await registry.activateTEE(teeId2, { from: teeOperator })
            } catch (e) {
                // May already be active, ignore
            }
        })

        it('should return correct public key for registered TEE', async function () {
            const key = await registry.getPublicKey(teeId1)
            expect(key).to.equal(publicKey1)

            console.log('✓ getPublicKey returns correct key')
        })

        it('should revert getPublicKey for non-existent TEE', async function () {
            const fakeTeeId = web3.utils.keccak256('0xNONEXISTENT')

            await truffleAssert.reverts(
                registry.getPublicKey(fakeTeeId)
            )

            console.log('✓ getPublicKey reverts for non-existent TEE')
        })

        it('should return correct TLS certificate for registered TEE', async function () {
            const cert = await registry.getTLSCertificate(teeId1)
            expect(cert).to.equal(tlsCert1)

            console.log('✓ getTLSCertificate returns correct cert')
        })

        it('should revert getTLSCertificate for non-existent TEE', async function () {
            const fakeTeeId = web3.utils.keccak256('0xNONEXISTENT')

            await truffleAssert.reverts(
                registry.getTLSCertificate(fakeTeeId)
            )

            console.log('✓ getTLSCertificate reverts for non-existent TEE')
        })

        it('should return correct payment address for registered TEE', async function () {
            const addr = await registry.getPaymentAddress(teeId1)
            expect(addr).to.equal(user1)

            console.log('✓ getPaymentAddress returns correct address')
        })

        it('should revert getPaymentAddress for non-existent TEE', async function () {
            const fakeTeeId = web3.utils.keccak256('0xNONEXISTENT')

            await truffleAssert.reverts(
                registry.getPaymentAddress(fakeTeeId)
            )

            console.log('✓ getPaymentAddress reverts for non-existent TEE')
        })

        it('should return correct active TEE list', async function () {
            const activeTEEs = await registry.getActiveTEEs()
            expect(activeTEEs.length).to.be.greaterThan(0)

            // Both TEEs should be in the list
            expect(activeTEEs.some(id => id === teeId1)).to.be.true
            expect(activeTEEs.some(id => id === teeId2)).to.be.true

            console.log('✓ getActiveTEEs returns correct list')
        })

        it('should update active TEE list after deactivate and activate', async function () {
            // Deactivate teeId2
            await registry.deactivateTEE(teeId2, { from: teeOperator })

            let activeTEEs = await registry.getActiveTEEs()
            expect(activeTEEs.some(id => id === teeId2)).to.be.false
            expect(activeTEEs.some(id => id === teeId1)).to.be.true

            // Re-activate teeId2
            await registry.activateTEE(teeId2, { from: teeOperator })

            activeTEEs = await registry.getActiveTEEs()
            expect(activeTEEs.some(id => id === teeId2)).to.be.true

            console.log('✓ getActiveTEEs updates after deactivate/activate')
        })

        it('should return true for active TEE via isActive', async function () {
            expect(await registry.isActive(teeId1)).to.be.true

            console.log('✓ isActive returns true for active TEE')
        })

        it('should return false for inactive TEE via isActive', async function () {
            await registry.deactivateTEE(teeId1, { from: teeOperator })
            expect(await registry.isActive(teeId1)).to.be.false

            // Re-activate for subsequent tests
            await registry.activateTEE(teeId1, { from: teeOperator })

            console.log('✓ isActive returns false for inactive TEE')
        })

        it('should return false for non-existent TEE via isActive', async function () {
            const fakeTeeId = web3.utils.keccak256('0xNONEXISTENT')
            expect(await registry.isActive(fakeTeeId)).to.be.false

            console.log('✓ isActive returns false for non-existent TEE')
        })
    })
})
