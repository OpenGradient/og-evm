const { expect } = require('chai')
const crypto = require('crypto')
const truffleAssert = require('truffle-assertions')
const TEERegistry = artifacts.require('TEERegistry')
const TEETestHelper = artifacts.require('TEETestHelper')

contract('TEERegistry', function (accounts) {
    let owner, teeOperator, user1, user2
    let registry, helper

    before(async () => {
        [owner, teeOperator, user1, user2] = accounts

        // Deploy TEERegistry
        registry = await TEERegistry.new()

        // Deploy test helper
        helper = await TEETestHelper.new(registry.address)

        console.log('TEERegistry deployed at:', registry.address)
        console.log('TEETestHelper deployed at:', helper.address)

        // Grant TEE_OPERATOR role to teeOperator account
        const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()
        await registry.grantRole(TEE_OPERATOR_ROLE, teeOperator)

        console.log('Setup complete')
    })

    describe('Initialization', function () {
        it('should initialize with correct roles', async function () {
            const DEFAULT_ADMIN_ROLE = await registry.DEFAULT_ADMIN_ROLE()
            const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()

            expect(await registry.hasRole(DEFAULT_ADMIN_ROLE, owner)).to.be.true
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, owner)).to.be.true
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, teeOperator)).to.be.true

            console.log('✓ Roles initialized correctly')
        })

        it('should have correct precompile address', async function () {
            const VERIFIER_ADDRESS = await registry.VERIFIER()
            expect(VERIFIER_ADDRESS).to.equal('0x0000000000000000000000000000000000000900')

            console.log('✓ Precompile address is correct')
        })
    })

    describe('TEE Type Management', function () {
        const TYPE_AWS_NITRO = 1
        const TYPE_CUSTOM = 2

        it('should allow admin to add TEE type', async function () {
            const result = await registry.addTEEType(TYPE_AWS_NITRO, 'AWS Nitro')

            truffleAssert.eventEmitted(result, 'TEETypeAdded', (ev) => {
                return ev.typeId.toString() === TYPE_AWS_NITRO.toString() && ev.name === 'AWS Nitro'
            })

            expect(await registry.isValidTEEType(TYPE_AWS_NITRO)).to.be.true

            const typeInfo = await registry.teeTypes(TYPE_AWS_NITRO)
            expect(typeInfo.name).to.equal('AWS Nitro')
            expect(typeInfo.active).to.be.true

            console.log('✓ TEE type added successfully')
        })

        it('should prevent duplicate TEE type', async function () {
            await truffleAssert.reverts(
                registry.addTEEType(TYPE_AWS_NITRO, 'Duplicate')
            )

            console.log('✓ Duplicate TEE type prevented')
        })

        it('should reject non-admin adding TEE type', async function () {
            const DEFAULT_ADMIN_ROLE = await registry.DEFAULT_ADMIN_ROLE()

            await truffleAssert.reverts(
                registry.addTEEType(99, 'Unauthorized', { from: user1 })
            )

            console.log('✓ Non-admin cannot add TEE type')
        })

        it('should allow admin to deactivate TEE type', async function () {
            await registry.addTEEType(TYPE_CUSTOM, 'Custom TEE')
            expect(await registry.isValidTEEType(TYPE_CUSTOM)).to.be.true

            const result = await registry.deactivateTEEType(TYPE_CUSTOM)

            truffleAssert.eventEmitted(result, 'TEETypeDeactivated', (ev) => {
                return ev.typeId.toString() === TYPE_CUSTOM.toString()
            })

            expect(await registry.isValidTEEType(TYPE_CUSTOM)).to.be.false

            console.log('✓ TEE type deactivated successfully')
        })

        it('should reject non-admin deactivating TEE type', async function () {
            await truffleAssert.reverts(
                registry.deactivateTEEType(TYPE_AWS_NITRO, { from: user1 })
            )

            console.log('✓ Non-admin cannot deactivate TEE type')
        })

        it('should list all TEE types', async function () {
            const result = await registry.getTEETypes()
            const typeIds = result.typeIds || result[0]
            const infos = result.infos || result[1]

            expect(typeIds.length).to.be.greaterThan(0)
            expect(infos.length).to.equal(typeIds.length)

            console.log('TEE types count:', typeIds.length)
            console.log('✓ TEE types listed successfully')
        })
    })

    describe('PCR Management', function () {
        const pcrs = {
            pcr0: '0x' + Buffer.alloc(48, 0x01).toString('hex'),
            pcr1: '0x' + Buffer.alloc(48, 0x02).toString('hex'),
            pcr2: '0x' + Buffer.alloc(48, 0x03).toString('hex')
        }

        let pcrHash

        before(async function () {
            // Compute PCR hash
            pcrHash = await registry.computePCRHash(pcrs)
            console.log('PCR hash:', pcrHash)
        })

        it('should allow admin to approve PCR', async function () {
            const result = await registry.approvePCR(pcrs, 'v1.0.0', 1)

            truffleAssert.eventEmitted(result, 'PCRApproved', (ev) => {
                return ev.pcrHash === pcrHash && ev.teeType.toString() === '1' && ev.version === 'v1.0.0'
            })

            expect(await registry.isPCRApproved(pcrHash)).to.be.true

            const pcrInfo = await registry.approvedPCRs(pcrHash)
            expect(pcrInfo.active).to.be.true
            expect(pcrInfo.version).to.equal('v1.0.0')
            expect(pcrInfo.expiresAt.toString()).to.equal('0')

            console.log('✓ PCR approved successfully')
        })

        it('should handle PCR versioning with grace period via revokePCR', async function () {
            const pcrsV2 = {
                pcr0: '0x' + Buffer.alloc(48, 0x04).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x05).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x06).toString('hex')
            }

            const pcrHashV2 = await registry.computePCRHash(pcrsV2)
            const gracePeriod = 3600 // 1 hour

            // Approve v2
            await registry.approvePCR(pcrsV2, 'v2.0.0', 1)

            // Revoke v1 with grace period
            await registry.revokePCR(pcrHash, gracePeriod)

            // Both should be valid during grace period
            expect(await registry.isPCRApproved(pcrHash)).to.be.true
            expect(await registry.isPCRApproved(pcrHashV2)).to.be.true

            const pcrV1Info = await registry.approvedPCRs(pcrHash)
            expect(pcrV1Info.expiresAt.toNumber()).to.be.greaterThan(0)

            console.log('✓ PCR versioning with grace period works')
        })

        it('should allow admin to revoke PCR', async function () {
            const pcrsRevoke = {
                pcr0: '0x' + Buffer.alloc(48, 0x07).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x08).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x09).toString('hex')
            }

            const pcrHashRevoke = await registry.computePCRHash(pcrsRevoke)

            await registry.approvePCR(pcrsRevoke, 'v1.0.0-revoke', 1)
            expect(await registry.isPCRApproved(pcrHashRevoke)).to.be.true

            const result = await registry.revokePCR(pcrHashRevoke, 0)

            truffleAssert.eventEmitted(result, 'PCRRevoked', (ev) => {
                return ev.pcrHash === pcrHashRevoke && ev.gracePeriod.toString() === '0'
            })

            expect(await registry.isPCRApproved(pcrHashRevoke)).to.be.false

            console.log('✓ PCR revoked successfully')
        })

        it('should list active PCRs', async function () {
            const activePCRs = await registry.getActivePCRs()

            expect(activePCRs.length).to.be.greaterThan(0)
            console.log('Active PCRs count:', activePCRs.length)
            console.log('✓ Active PCRs listed successfully')
        })

        it('should reject non-admin approving PCR', async function () {
            const pcrsUnauth = {
                pcr0: '0x0A',
                pcr1: '0x0B',
                pcr2: '0x0C'
            }

            await truffleAssert.reverts(
                registry.approvePCR(pcrsUnauth, 'unauthorized', 1, { from: user1 })
            )

            console.log('✓ Non-admin cannot approve PCR')
        })

        it('should reject non-admin revoking PCR', async function () {
            await truffleAssert.reverts(
                registry.revokePCR(pcrHash, 0, { from: user1 })
            )

            console.log('✓ Non-admin cannot revoke PCR')
        })

        it('should reject expired PCR after grace period elapses', async function () {
            const pcrsExpiry = {
                pcr0: '0x' + Buffer.alloc(48, 0xE1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xE2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xE3).toString('hex')
            }

            const pcrHashExpiry = await registry.computePCRHash(pcrsExpiry)

            // Approve the PCR
            await registry.approvePCR(pcrsExpiry, 'v-expiry', 1)
            expect(await registry.isPCRApproved(pcrHashExpiry)).to.be.true

            // Revoke with gracePeriod=0, which sets expiresAt = block.timestamp.
            // Any subsequent block (where block.timestamp > expiresAt) will cause
            // isPCRApproved to return false.
            await registry.revokePCR(pcrHashExpiry, 1)

            // Send a dummy transaction to mine a new block with an advanced timestamp
            await registry.setAWSRootCertificate('0x01')
            await registry.setAWSRootCertificate('0x01')

            // The expiry PCR should now be rejected (block.timestamp > expiresAt)
            expect(await registry.isPCRApproved(pcrHashExpiry)).to.be.false

            console.log('✓ Expired PCR rejected after grace period elapses')
        })
    })

    describe('Certificate Management', function () {
        it('should allow admin to set AWS root certificate', async function () {
            const certData = '0x' + Buffer.from('AWS ROOT CERT', 'utf8').toString('hex')
            const certHash = web3.utils.keccak256(certData)

            const result = await registry.setAWSRootCertificate(certData)

            truffleAssert.eventEmitted(result, 'AWSCertificateUpdated', (ev) => {
                return ev.certHash === certHash
            })

            const storedCert = await registry.awsRootCertificate()
            expect(storedCert).to.equal(certData)

            console.log('✓ AWS root certificate set successfully')
        })

        it('should reject non-admin setting certificate', async function () {
            const certData = '0x0102030405'

            await truffleAssert.reverts(
                registry.setAWSRootCertificate(certData, { from: user1 })
            )

            console.log('✓ Non-admin cannot set certificate')
        })
    })

    describe('TEE Lifecycle Management', function () {
        let teeId, publicKey

        before(async function () {
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
        })

        it('should prevent registration without TEE_OPERATOR role', async function () {
            const dummyAttestation = '0x0102030405'
            const dummyCert = '0x0607080910'

            await truffleAssert.reverts(
                registry.registerTEEWithAttestation(
                    dummyAttestation,
                    publicKey,
                    dummyCert,
                    user1,
                    'https://tee.example.com',
                    1,
                    { from: user1 }
                )
            )

            console.log('✓ Non-operator cannot register TEE')
        })

        it('should reject invalid TEE type', async function () {
            const dummyAttestation = '0x0102030405'
            const dummyCert = '0x0607080910'

            await truffleAssert.reverts(
                registry.registerTEEWithAttestation(
                    dummyAttestation,
                    publicKey,
                    dummyCert,
                    teeOperator,
                    'https://tee.example.com',
                    99, // Invalid type
                    { from: teeOperator }
                )
            )

            console.log('✓ Invalid TEE type rejected')
        })

        it('should reject registration with invalid attestation', async function () {
            // First approve a PCR
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x01).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x02).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x03).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v1.0.0', 1)

            // Add TEE type if not exists
            try {
                await registry.addTEEType(1, 'AWS Nitro')
            } catch (e) {
                // Type already exists, continue
            }

            const invalidAttestation = '0x' + Buffer.alloc(100, 0xFF).toString('hex')
            const dummyCert = '0x' + Buffer.alloc(100, 0xAA).toString('hex')

            await truffleAssert.reverts(
                registry.registerTEEWithAttestation(
                    invalidAttestation,
                    publicKey,
                    dummyCert,
                    teeOperator,
                    'https://tee.example.com',
                    1,
                    { from: teeOperator }
                )
            )

            console.log('✓ Invalid attestation rejected during registration')
        })
    })

    describe('Query Functions', function () {
        it('should compute TEE ID correctly', async function () {
            const testKey = '0x0102030405'
            const expectedId = web3.utils.keccak256(testKey)

            const computedId = await registry.computeTEEId(testKey)
            expect(computedId).to.equal(expectedId)

            console.log('✓ TEE ID computation correct')
        })

        it('should compute PCR hash correctly', async function () {
            const pcrs = {
                pcr0: '0x01',
                pcr1: '0x02',
                pcr2: '0x03'
            }

            const expectedHash = web3.utils.keccak256(
                pcrs.pcr0 + pcrs.pcr1.slice(2) + pcrs.pcr2.slice(2)
            )

            const computedHash = await registry.computePCRHash(pcrs)
            expect(computedHash).to.equal(expectedHash)

            console.log('✓ PCR hash computation correct')
        })

        it('should compute message hash correctly', async function () {
            const inputHash = web3.utils.keccak256('0x01')
            const outputHash = web3.utils.keccak256('0x02')
            const timestamp = Math.floor(Date.now() / 1000)

            const expectedHash = web3.utils.keccak256(
                web3.eth.abi.encodeParameters(
                    ['bytes32', 'bytes32', 'uint256'],
                    [inputHash, outputHash, timestamp]
                )
            )

            const computedHash = await registry.computeMessageHash(inputHash, outputHash, timestamp)
            expect(computedHash).to.equal(expectedHash)

            console.log('✓ Message hash computation correct')
        })

        it('should handle getTEE for non-existent TEE', async function () {
            const nonExistentId = web3.utils.keccak256('0xDEADBEEF')

            await truffleAssert.reverts(
                registry.getTEE(nonExistentId)
            )

            console.log('✓ Non-existent TEE query handled correctly')
        })

        it('should return empty arrays for new owner', async function () {
            const tees = await registry.getTEEsByOwner(user2)
            expect(tees.length).to.equal(0)

            console.log('✓ Empty TEE array returned for new owner')
        })

        it('should return empty array for TEE type with no TEEs', async function () {
            const tees = await registry.getTEEsByType(50)
            expect(tees.length).to.equal(0)

            console.log('✓ Empty array returned for unused TEE type')
        })
    })

    describe('Access Control', function () {
        it('should enforce DEFAULT_ADMIN_ROLE for sensitive operations', async function () {
            // Test various admin-only functions
            await truffleAssert.reverts(
                registry.addTEEType(99, 'Test', { from: user1 })
            )

            await truffleAssert.reverts(
                registry.approvePCR(
                    { pcr0: '0x01', pcr1: '0x02', pcr2: '0x03' },
                    'v1',
                    1,
                    { from: user1 }
                )
            )

            await truffleAssert.reverts(
                registry.setAWSRootCertificate('0x01', { from: user1 })
            )

            console.log('✓ Admin role enforced correctly')
        })

        it('should allow role management by admin', async function () {
            const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()

            // Grant role
            await registry.grantRole(TEE_OPERATOR_ROLE, user1)
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, user1)).to.be.true

            // Revoke role
            await registry.revokeRole(TEE_OPERATOR_ROLE, user1)
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, user1)).to.be.false

            console.log('✓ Role management works correctly')
        })
    })

    describe('PCR Revocation Security', function () {
        it('should prevent duplicate PCR registration', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x20).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x21).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x22).toString('hex')
            }

            // First approval should succeed
            await registry.approvePCR(pcrs, 'v-duplicate-test', 1)

            // Second approval with same PCRs should fail
            await truffleAssert.reverts(
                registry.approvePCR(pcrs, 'v-duplicate-test-2', 1)
            )

            console.log('✓ Duplicate PCR registration prevented')
        })

        it('should allow re-approval of revoked PCR', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x25).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x26).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x27).toString('hex')
            }

            const pcrHash = await registry.computePCRHash(pcrs)

            await registry.approvePCR(pcrs, 'v-reapprove-1', 1)
            await registry.revokePCR(pcrHash, 0)
            expect(await registry.isPCRApproved(pcrHash)).to.be.false

            // Re-approval should succeed after revocation
            await registry.approvePCR(pcrs, 'v-reapprove-2', 1)
            expect(await registry.isPCRApproved(pcrHash)).to.be.true

            console.log('✓ Re-approval of revoked PCR works')
        })

        it('should revoke PCR immediately when gracePeriod is 0', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x30).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x31).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x32).toString('hex')
            }
            const pcrHash = await registry.computePCRHash(pcrs)

            await registry.approvePCR(pcrs, 'v-immediate-revoke', 1)

            const result = await registry.revokePCR(pcrHash, 0)

            truffleAssert.eventEmitted(result, 'PCRRevoked', (ev) => {
                return ev.pcrHash === pcrHash && ev.gracePeriod.toString() === '0'
            })

            expect(await registry.isPCRApproved(pcrHash)).to.be.false

            console.log('✓ Immediate PCR revocation works')
        })

        it('should revoke PCR with grace period', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x70).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x71).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x72).toString('hex')
            }

            const pcrHash = await registry.computePCRHash(pcrs)
            const gracePeriod = 3600 // 1 hour

            await registry.approvePCR(pcrs, 'v-grace-revoke', 1)

            const result = await registry.revokePCR(pcrHash, gracePeriod)

            truffleAssert.eventEmitted(result, 'PCRRevoked', (ev) => {
                return ev.pcrHash === pcrHash && ev.gracePeriod.toString() === gracePeriod.toString()
            })

            // Should still be valid during grace period
            expect(await registry.isPCRApproved(pcrHash)).to.be.true

            const pcrInfo = await registry.approvedPCRs(pcrHash)
            expect(pcrInfo.expiresAt.toNumber()).to.be.greaterThan(0)

            console.log('✓ PCR revocation with grace period works')
        })

        it('should maintain both old and new PCRs during grace period', async function () {
            const pcrsV1 = {
                pcr0: '0x' + Buffer.alloc(48, 0x80).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x81).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x82).toString('hex')
            }

            const pcrsV2 = {
                pcr0: '0x' + Buffer.alloc(48, 0x90).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x91).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x92).toString('hex')
            }

            const v1Hash = await registry.computePCRHash(pcrsV1)
            const v2Hash = await registry.computePCRHash(pcrsV2)

            // Approve v1, then v2
            await registry.approvePCR(pcrsV1, 'v1-grace', 1)
            await registry.approvePCR(pcrsV2, 'v2-grace', 1)

            // Revoke v1 with 1 hour grace period
            await registry.revokePCR(v1Hash, 3600)

            // Both should be valid during grace period
            expect(await registry.isPCRApproved(v1Hash)).to.be.true
            expect(await registry.isPCRApproved(v2Hash)).to.be.true

            // v1 should have expiry set
            const v1Info = await registry.approvedPCRs(v1Hash)
            expect(v1Info.expiresAt.toNumber()).to.be.greaterThan(0)

            console.log('✓ Both PCRs valid during grace period')
        })
    })
})
