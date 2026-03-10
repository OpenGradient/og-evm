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

        // Approve the PCR so enableTEE() passes _requirePCRValidForTEE
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

    // Helper: register and enable a TEE via mock
    async function registerAndEnable(pubKey, tlsCert, paymentAddr, endpoint, teeType, pcrHash, from) {
        const result = await registry.registerTEEForTesting(
            pubKey, tlsCert, paymentAddr, endpoint, teeType, pcrHash, { from }
        )
        const teeId = await registry.computeTEEId(pubKey)
        // enableTEE must be called by the owner (msg.sender of registerTEEForTesting)
        await registry.enableTEE(teeId, { from })
        return teeId
    }

    // Helper: check active status via getTEE
    async function isTEEEnabled(teeId) {
        const tee = await registry.getTEE(teeId)
        return tee.enabled
    }

    // ============ disableTEE Tests ============

    describe('disableTEE', function () {
        let localTeeId

        before(async function () {
            // Register and enable TEE 1 via teeOperator (who is the owner)
            localTeeId = await registerAndEnable(
                publicKey1, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, PCR_HASH, teeOperator
            )
        })

        it('should allow owner to disable their TEE', async function () {
            const result = await registry.disableTEE(localTeeId, { from: teeOperator })

            truffleAssert.eventEmitted(result, 'TEEDisabled', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await isTEEEnabled(localTeeId)).to.be.false

            console.log('✓ Owner disabled TEE')
        })

        it('should remove TEE from enabled list after disabling', async function () {
            const enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            const found = enabledTEEs.some(id => id === localTeeId)
            expect(found).to.be.false

            console.log('✓ TEE removed from enabled list')
        })

        it('should be a no-op when disabling already disabled TEE', async function () {
            // TEE is already inactive from the previous test
            const result = await registry.disableTEE(localTeeId, { from: teeOperator })

            // No event should be emitted for a no-op
            truffleAssert.eventNotEmitted(result, 'TEEDisabled')

            console.log('✓ Double-disabling is a no-op')
        })

        it('should allow admin to disable any TEE', async function () {
            // Re-enable first
            await registry.enableTEE(localTeeId, { from: teeOperator })
            expect(await isTEEEnabled(localTeeId)).to.be.true

            // Admin (owner/accounts[0]) disables
            const result = await registry.disableTEE(localTeeId, { from: owner })

            truffleAssert.eventEmitted(result, 'TEEDisabled', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await isTEEEnabled(localTeeId)).to.be.false

            console.log('✓ Admin disabled TEE')
        })

        it('should revert when non-owner/non-admin disables', async function () {
            // Re-enable first
            await registry.enableTEE(localTeeId, { from: teeOperator })

            await truffleAssert.reverts(
                registry.disableTEE(localTeeId, { from: user1 })
            )

            console.log('✓ Non-owner/non-admin rejected')
        })

        it('should revert for non-existent teeId', async function () {
            const fakeTeeId = web3.utils.keccak256('0xDEADBEEF')

            await truffleAssert.reverts(
                registry.disableTEE(fakeTeeId, { from: owner })
            )

            console.log('✓ Non-existent teeId reverts')
        })
    })

    // ============ enableTEE Tests ============

    describe('enableTEE', function () {
        let localTeeId

        before(async function () {
            // Register TEE 2 (inactive by default from mock)
            await registry.registerTEEForTesting(
                publicKey2, tlsCert2, user2, ENDPOINT, TEE_TYPE_NITRO, PCR_HASH, { from: teeOperator }
            )
            localTeeId = teeId2
        })

        it('should allow owner to enable their disabled TEE', async function () {
            // TEE starts inactive from mock registration
            expect(await isTEEEnabled(localTeeId)).to.be.false

            const result = await registry.enableTEE(localTeeId, { from: teeOperator })

            truffleAssert.eventEmitted(result, 'TEEEnabled', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await isTEEEnabled(localTeeId)).to.be.true

            console.log('✓ Owner enabled TEE')
        })

        it('should add TEE back to enabled list after enabling', async function () {
            const enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            const found = enabledTEEs.some(id => id === localTeeId)
            expect(found).to.be.true

            console.log('✓ TEE added to enabled list')
        })

        it('should be a no-op when enabling already enabled TEE', async function () {
            // TEE is already active from the previous test
            const result = await registry.enableTEE(localTeeId, { from: teeOperator })

            // No event should be emitted for a no-op
            truffleAssert.eventNotEmitted(result, 'TEEEnabled')

            console.log('✓ Double-enable is a no-op')
        })

        it('should allow admin to enable any TEE', async function () {
            // Disable first
            await registry.disableTEE(localTeeId, { from: teeOperator })
            expect(await isTEEEnabled(localTeeId)).to.be.false

            // Admin (owner/accounts[0]) enables
            const result = await registry.enableTEE(localTeeId, { from: owner })

            truffleAssert.eventEmitted(result, 'TEEEnabled', (ev) => {
                return ev.teeId === localTeeId
            })

            expect(await isTEEEnabled(localTeeId)).to.be.true

            console.log('✓ Admin enabled TEE')
        })

        it('should revert when non-owner/non-admin enables', async function () {
            // Disable first
            await registry.disableTEE(localTeeId, { from: teeOperator })

            await truffleAssert.reverts(
                registry.enableTEE(localTeeId, { from: user1 })
            )

            console.log('✓ Non-owner/non-admin rejected')
        })

        it('should revert for non-existent teeId', async function () {
            const fakeTeeId = web3.utils.keccak256('0xDEADBEEF')

            await truffleAssert.reverts(
                registry.enableTEE(fakeTeeId, { from: owner })
            )

            console.log('✓ Non-existent teeId reverts')
        })
    })

    // ============ PCR Enforcement Tests ============

    describe('PCR enforcement on enableTEE', function () {
        let pcrTeeId, pcrPublicKey

        before(async function () {
            const keyPair = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            pcrPublicKey = '0x' + keyPair.publicKey.toString('hex')
        })

        it('should revert enableTEE when PCR is immediately revoked', async function () {
            // Approve a fresh PCR
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0xA1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xA2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xA3).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v-revoke-enable', TEE_TYPE_NITRO)
            const pcrHash = await registry.computePCRHash(pcrs)

            // Register TEE (inactive) with this PCR
            await registry.registerTEEForTesting(
                pcrPublicKey, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            pcrTeeId = await registry.computeTEEId(pcrPublicKey)

            // Revoke the PCR
            await registry.revokePCR(pcrHash, TEE_TYPE_NITRO)
            expect(await registry.isPCRApproved(TEE_TYPE_NITRO, pcrHash)).to.be.false

            // enableTEE should revert with PCRNotApproved
            await truffleAssert.reverts(
                registry.enableTEE(pcrTeeId, { from: teeOperator })
            )

            expect(await isTEEEnabled(pcrTeeId)).to.be.false

            console.log('✓ enableTEE reverts when PCR is revoked')
        })

        it('should revert enableTEE when PCR is revoked (not just immediate)', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0xB1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xB2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xB3).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v-revoke-enable-2', TEE_TYPE_NITRO)
            const pcrHash = await registry.computePCRHash(pcrs)

            const keyPair = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            const pubKey = '0x' + keyPair.publicKey.toString('hex')

            // Register TEE (inactive) with this PCR
            await registry.registerTEEForTesting(
                pubKey, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            const teeId = await registry.computeTEEId(pubKey)

            // Revoke the PCR
            await registry.revokePCR(pcrHash, TEE_TYPE_NITRO)
            expect(await registry.isPCRApproved(TEE_TYPE_NITRO, pcrHash)).to.be.false

            // enableTEE should revert with PCRNotApproved
            await truffleAssert.reverts(
                registry.enableTEE(teeId, { from: teeOperator })
            )

            expect(await isTEEEnabled(teeId)).to.be.false

            console.log('✓ enableTEE reverts when PCR is revoked')
        })

        it('should revert enableTEE when PCR type does not match TEE type', async function () {
            // Add a second TEE type
            const TEE_TYPE_OTHER = 3
            await registry.addTEEType(TEE_TYPE_OTHER, 'Other TEE')

            // Approve a PCR only for TEE_TYPE_OTHER
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0xC1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xC2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xC3).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v-type-mismatch', TEE_TYPE_OTHER)
            const pcrHash = await registry.computePCRHash(pcrs)

            const keyPair = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            const pubKey = '0x' + keyPair.publicKey.toString('hex')

            // Register TEE as TEE_TYPE_NITRO but with a PCR approved for TEE_TYPE_OTHER
            await registry.registerTEEForTesting(
                pubKey, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            const teeId = await registry.computeTEEId(pubKey)

            // enableTEE should revert
            await truffleAssert.reverts(
                registry.enableTEE(teeId, { from: teeOperator })
            )

            expect(await isTEEEnabled(teeId)).to.be.false

            console.log('✓ enableTEE reverts on PCR type mismatch')
        })
    })

    describe('PCR revocation actively disables TEEs', function () {
        it('should disable enabled TEE when its PCR is revoked', async function () {
            // Approve a fresh PCR
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0xD1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xD2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xD3).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v-active-revoke', TEE_TYPE_NITRO)
            const pcrHash = await registry.computePCRHash(pcrs)

            const keyPair = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            const pubKey = '0x' + keyPair.publicKey.toString('hex')

            // Register and enable TEE with valid PCR
            await registry.registerTEEForTesting(
                pubKey, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            const teeId = await registry.computeTEEId(pubKey)
            await registry.enableTEE(teeId, { from: teeOperator })
            expect(await isTEEEnabled(teeId)).to.be.true

            // Revoke the PCR - TEE should be actively disabled
            const result = await registry.revokePCR(pcrHash, TEE_TYPE_NITRO)

            // TEE should now be disabled
            expect(await isTEEEnabled(teeId)).to.be.false

            // TEE should be removed from enabled list
            const enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            expect(enabledTEEs.some(id => id === teeId)).to.be.false

            // TEEDisabled event should be emitted
            truffleAssert.eventEmitted(result, 'TEEDisabled', (ev) => {
                return ev.teeId === teeId
            })

            console.log('✓ TEE disabled when PCR is revoked')
        })

        it('should disable multiple TEEs when their shared PCR is revoked', async function () {
            // Approve a fresh PCR
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0xE1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xE2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xE3).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v-multi-revoke', TEE_TYPE_NITRO)
            const pcrHash = await registry.computePCRHash(pcrs)

            // Register and enable two TEEs with the same PCR
            const keyPair1 = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            const pubKey1 = '0x' + keyPair1.publicKey.toString('hex')

            const keyPair2 = crypto.generateKeyPairSync('rsa', {
                modulusLength: 2048,
                publicKeyEncoding: { type: 'spki', format: 'der' }
            })
            const pubKey2 = '0x' + keyPair2.publicKey.toString('hex')

            await registry.registerTEEForTesting(
                pubKey1, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            await registry.registerTEEForTesting(
                pubKey2, tlsCert1, user1, ENDPOINT, TEE_TYPE_NITRO, pcrHash, { from: teeOperator }
            )
            const teeId1 = await registry.computeTEEId(pubKey1)
            const teeId2 = await registry.computeTEEId(pubKey2)

            await registry.enableTEE(teeId1, { from: teeOperator })
            await registry.enableTEE(teeId2, { from: teeOperator })
            expect(await isTEEEnabled(teeId1)).to.be.true
            expect(await isTEEEnabled(teeId2)).to.be.true

            // Revoke the PCR - both TEEs should be disabled
            await registry.revokePCR(pcrHash, TEE_TYPE_NITRO)

            expect(await isTEEEnabled(teeId1)).to.be.false
            expect(await isTEEEnabled(teeId2)).to.be.false

            console.log('✓ Multiple TEEs disabled when shared PCR is revoked')
        })
    })

    // ============ Query Function Tests ============

    describe('Query functions', function () {
        // At this point teeId1 is registered and active (from disableTEE tests, re-enabled),
        // teeId2 is registered but may be inactive. We'll set known state.

        before(async function () {
            // Ensure teeId1 is active
            try {
                await registry.enableTEE(teeId1, { from: teeOperator })
            } catch (e) {
                // May already be active, ignore
            }
            // Ensure teeId2 is active
            try {
                await registry.enableTEE(teeId2, { from: teeOperator })
            } catch (e) {
                // May already be active, ignore
            }
        })

        it('should return correct public key via getTEE', async function () {
            const tee = await registry.getTEE(teeId1)
            expect(tee.publicKey).to.equal(publicKey1)

            console.log('✓ getTEE returns correct public key')
        })

        it('should revert getTEE for non-existent TEE', async function () {
            const fakeTeeId = web3.utils.keccak256('0xNONEXISTENT')

            await truffleAssert.reverts(
                registry.getTEE(fakeTeeId)
            )

            console.log('✓ getTEE reverts for non-existent TEE')
        })

        it('should return correct TLS certificate via getTEE', async function () {
            const tee = await registry.getTEE(teeId1)
            expect(tee.tlsCertificate).to.equal(tlsCert1)

            console.log('✓ getTEE returns correct TLS cert')
        })

        it('should return correct payment address via getTEE', async function () {
            const tee = await registry.getTEE(teeId1)
            expect(tee.paymentAddress).to.equal(user1)

            console.log('✓ getTEE returns correct payment address')
        })

        it('should return correct enabled TEE list', async function () {
            const enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            expect(enabledTEEs.length).to.be.greaterThan(0)

            // Both TEEs should be in the list
            expect(enabledTEEs.some(id => id === teeId1)).to.be.true
            expect(enabledTEEs.some(id => id === teeId2)).to.be.true

            console.log('✓ getEnabledTEEs returns correct list')
        })

        it('should update enabled TEE list after disable and enable', async function () {
            // Disable teeId2
            await registry.disableTEE(teeId2, { from: teeOperator })

            let enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            expect(enabledTEEs.some(id => id === teeId2)).to.be.false
            expect(enabledTEEs.some(id => id === teeId1)).to.be.true

            // Re-enable teeId2
            await registry.enableTEE(teeId2, { from: teeOperator })

            enabledTEEs = await registry.getEnabledTEEs(TEE_TYPE_NITRO)
            expect(enabledTEEs.some(id => id === teeId2)).to.be.true

            console.log('✓ getEnabledTEEs updates after disable/enable')
        })

        it('should return active status via getTEE', async function () {
            const tee = await registry.getTEE(teeId1)
            expect(tee.enabled).to.be.true

            console.log('✓ getTEE returns active=true for active TEE')
        })

        it('should return inactive status via getTEE after disabling', async function () {
            await registry.disableTEE(teeId1, { from: teeOperator })
            const tee = await registry.getTEE(teeId1)
            expect(tee.enabled).to.be.false

            // Re-enable for subsequent tests
            await registry.enableTEE(teeId1, { from: teeOperator })

            console.log('✓ getTEE returns active=false for inactive TEE')
        })

        it('should return all TEEs by type including inactive', async function () {
            const allTEEs = await registry.getTEEsByType(TEE_TYPE_NITRO)
            // Should include all registered TEEs (active and inactive from PCR tests)
            expect(allTEEs.length).to.be.greaterThanOrEqual(2)
            expect(allTEEs.some(id => id === teeId1)).to.be.true
            expect(allTEEs.some(id => id === teeId2)).to.be.true

            console.log('✓ getTEEsByType returns all TEEs including inactive')
        })

        it('should return active TEEs filtered by heartbeat and PCR', async function () {
            const activeTEEs = await registry.getActiveTEEs(TEE_TYPE_NITRO)
            // activeTEEs returns TEEInfo structs, check they have valid fields
            for (let i = 0; i < activeTEEs.length; i++) {
                expect(activeTEEs[i].enabled).to.be.true
                expect(Number(activeTEEs[i].registeredAt)).to.be.greaterThan(0)
            }

            console.log('Active TEEs count:', activeTEEs.length)
            console.log('✓ getActiveTEEs returns filtered results')
        })

        it('should return empty from getActiveTEEs for unused type', async function () {
            const activeTEEs = await registry.getActiveTEEs(50)
            expect(activeTEEs.length).to.equal(0)

            console.log('✓ getActiveTEEs returns empty for unused type')
        })
    })

})
