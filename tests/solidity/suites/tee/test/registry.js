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
            expect(typeInfo.addedAt.toNumber()).to.be.greaterThan(0)

            console.log('✓ TEE type added successfully')
        })

        it('should prevent duplicate TEE type', async function () {
            await truffleAssert.reverts(
                registry.addTEEType(TYPE_AWS_NITRO, 'Duplicate')
            )

            console.log('✓ Duplicate TEE type prevented')
        })

        it('should reject non-admin adding TEE type', async function () {
            await truffleAssert.reverts(
                registry.addTEEType(99, 'Unauthorized', { from: user1 })
            )

            console.log('✓ Non-admin cannot add TEE type')
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
        const TEE_TYPE = 1
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
            const result = await registry.approvePCR(pcrs, 'v1.0.0', TEE_TYPE)

            truffleAssert.eventEmitted(result, 'PCRApproved', (ev) => {
                return ev.pcrHash === pcrHash && ev.teeType.toString() === '1' && ev.version === 'v1.0.0'
            })

            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.true

            const pcrInfo = await registry.pcrRecords(TEE_TYPE, pcrHash)
            expect(pcrInfo.approved).to.be.true
            expect(pcrInfo.version).to.equal('v1.0.0')
            console.log('✓ PCR approved successfully')
        })

        it('should handle PCR versioning via revokePCR', async function () {
            const pcrsV2 = {
                pcr0: '0x' + Buffer.alloc(48, 0x04).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x05).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x06).toString('hex')
            }

            const pcrHashV2 = await registry.computePCRHash(pcrsV2)

            // Approve v2
            await registry.approvePCR(pcrsV2, 'v2.0.0', TEE_TYPE)

            // Revoke v1
            await registry.revokePCR(pcrHash, TEE_TYPE)

            // v1 should be revoked, v2 should still be valid
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.false
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHashV2)).to.be.true

            console.log('✓ PCR versioning works')
        })

        it('should allow admin to revoke PCR', async function () {
            const pcrsRevoke = {
                pcr0: '0x' + Buffer.alloc(48, 0x07).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x08).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x09).toString('hex')
            }

            const pcrHashRevoke = await registry.computePCRHash(pcrsRevoke)

            await registry.approvePCR(pcrsRevoke, 'v1.0.0-revoke', TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHashRevoke)).to.be.true

            const result = await registry.revokePCR(pcrHashRevoke, TEE_TYPE)

            truffleAssert.eventEmitted(result, 'PCRRevoked', (ev) => {
                return ev.pcrHash === pcrHashRevoke && ev.teeType.toNumber() === TEE_TYPE
            })

            expect(await registry.isPCRApproved(TEE_TYPE, pcrHashRevoke)).to.be.false

            console.log('✓ PCR revoked successfully')
        })

        it('should list approved PCRs', async function () {
            const approvedPCRs = await registry.getApprovedPCRs()

            expect(approvedPCRs.length).to.be.greaterThan(0)
            console.log('Approved PCRs count:', approvedPCRs.length)
            console.log('✓ Approved PCRs listed successfully')
        })

        it('should reject non-admin approving PCR', async function () {
            const pcrsUnauth = {
                pcr0: '0x0A',
                pcr1: '0x0B',
                pcr2: '0x0C'
            }

            await truffleAssert.reverts(
                registry.approvePCR(pcrsUnauth, 'unauthorized', TEE_TYPE, { from: user1 })
            )

            console.log('✓ Non-admin cannot approve PCR')
        })

        it('should reject non-admin revoking PCR', async function () {
            await truffleAssert.reverts(
                registry.revokePCR(pcrHash, TEE_TYPE, { from: user1 })
            )

            console.log('✓ Non-admin cannot revoke PCR')
        })

        it('should reject revoked PCR immediately', async function () {
            const pcrsExpiry = {
                pcr0: '0x' + Buffer.alloc(48, 0xE1).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0xE2).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0xE3).toString('hex')
            }

            const pcrHashExpiry = await registry.computePCRHash(pcrsExpiry)

            // Approve the PCR
            await registry.approvePCR(pcrsExpiry, 'v-expiry', TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHashExpiry)).to.be.true

            // Revoke
            await registry.revokePCR(pcrHashExpiry, TEE_TYPE)

            // The PCR should now be rejected
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHashExpiry)).to.be.false

            console.log('✓ Revoked PCR rejected immediately')
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

        it('should return empty array from getEnabledTEEs for unused type', async function () {
            const tees = await registry.getEnabledTEEs(50)
            expect(tees.length).to.equal(0)

            console.log('✓ Empty array returned from getEnabledTEEs for unused type')
        })

        it('should return empty array from getActiveTEEs for unused type', async function () {
            const tees = await registry.getActiveTEEs(50)
            expect(tees.length).to.equal(0)

            console.log('✓ Empty array returned from getActiveTEEs for unused type')
        })
    })

    describe('Access Control', function () {
        it('should enforce DEFAULT_ADMIN_ROLE for sensitive operations', async function () {
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
        const TEE_TYPE = 1

        it('should allow re-approval of active PCR (e.g. to update version)', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x20).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x21).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x22).toString('hex')
            }
            const pcrHash = await registry.computePCRHash(pcrs)

            // First approval
            await registry.approvePCR(pcrs, 'v-duplicate-test', TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.true

            // Re-approval should succeed and update version
            const result = await registry.approvePCR(pcrs, 'v-duplicate-test-2', TEE_TYPE)

            truffleAssert.eventEmitted(result, 'PCRApproved', (ev) => {
                return ev.pcrHash === pcrHash && ev.version === 'v-duplicate-test-2'
            })

            const pcrInfo = await registry.pcrRecords(TEE_TYPE, pcrHash)
            expect(pcrInfo.version).to.equal('v-duplicate-test-2')

            console.log('✓ Re-approval of approved PCR updates version')
        })

        it('should allow re-approval after revocation', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x23).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x24).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x28).toString('hex')
            }
            const pcrHash = await registry.computePCRHash(pcrs)

            // Approve, then revoke
            await registry.approvePCR(pcrs, 'v-revoke-reapprove', TEE_TYPE)
            await registry.revokePCR(pcrHash, TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.false

            // Re-approve
            await registry.approvePCR(pcrs, 'v-revoke-reapprove-fixed', TEE_TYPE)

            const infoAfter = await registry.pcrRecords(TEE_TYPE, pcrHash)
            expect(infoAfter.approved).to.be.true
            expect(infoAfter.version).to.equal('v-revoke-reapprove-fixed')

            console.log('✓ Re-approval after revocation works')
        })

        it('should allow re-approval of revoked PCR', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x25).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x26).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x27).toString('hex')
            }

            const pcrHash = await registry.computePCRHash(pcrs)

            await registry.approvePCR(pcrs, 'v-reapprove-1', TEE_TYPE)
            await registry.revokePCR(pcrHash, TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.false

            // Re-approval should succeed after revocation
            await registry.approvePCR(pcrs, 'v-reapprove-2', TEE_TYPE)
            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.true

            console.log('✓ Re-approval of revoked PCR works')
        })

        it('should revoke PCR immediately', async function () {
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x30).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x31).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x32).toString('hex')
            }
            const pcrHash = await registry.computePCRHash(pcrs)

            await registry.approvePCR(pcrs, 'v-immediate-revoke', TEE_TYPE)

            const result = await registry.revokePCR(pcrHash, TEE_TYPE)

            truffleAssert.eventEmitted(result, 'PCRRevoked', (ev) => {
                return ev.pcrHash === pcrHash && ev.teeType.toNumber() === TEE_TYPE
            })

            expect(await registry.isPCRApproved(TEE_TYPE, pcrHash)).to.be.false

            console.log('✓ PCR revocation works')
        })
    })
})
