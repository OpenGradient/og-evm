const { expect } = require('chai')
const hre = require('hardhat')
const crypto = require('crypto')

describe('TEERegistry', function () {
    let owner, teeOperator, user1, user2
    let TEERegistry, registry, TEETestHelper, helper

    before(async () => {
        [owner, teeOperator, user1, user2] = await hre.ethers.getSigners()

        // Deploy TEERegistry
        const RegistryFactory = await hre.ethers.getContractFactory('TEERegistry')
        registry = await RegistryFactory.deploy()
        await registry.waitForDeployment()

        // Deploy test helper
        const HelperFactory = await hre.ethers.getContractFactory('TEETestHelper')
        helper = await HelperFactory.deploy(await registry.getAddress())
        await helper.waitForDeployment()

        console.log('TEERegistry deployed at:', await registry.getAddress())
        console.log('TEETestHelper deployed at:', await helper.getAddress())

        // Grant TEE_OPERATOR role to teeOperator account
        const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()
        await registry.grantRole(TEE_OPERATOR_ROLE, teeOperator.address)

        console.log('Setup complete')
    })

    describe('Initialization', function () {
        it('should initialize with correct roles', async function () {
            const DEFAULT_ADMIN_ROLE = await registry.DEFAULT_ADMIN_ROLE()
            const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()

            expect(await registry.hasRole(DEFAULT_ADMIN_ROLE, owner.address)).to.be.true
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, owner.address)).to.be.true
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, teeOperator.address)).to.be.true

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
            await expect(registry.addTEEType(TYPE_AWS_NITRO, 'AWS Nitro'))
                .to.emit(registry, 'TEETypeAdded')
                .withArgs(TYPE_AWS_NITRO, 'AWS Nitro')

            expect(await registry.isValidTEEType(TYPE_AWS_NITRO)).to.be.true

            const typeInfo = await registry.teeTypes(TYPE_AWS_NITRO)
            expect(typeInfo.name).to.equal('AWS Nitro')
            expect(typeInfo.active).to.be.true

            console.log('✓ TEE type added successfully')
        })

        it('should prevent duplicate TEE type', async function () {
            await expect(registry.addTEEType(TYPE_AWS_NITRO, 'Duplicate'))
                .to.be.revertedWithCustomError(registry, 'TEETypeExists')

            console.log('✓ Duplicate TEE type prevented')
        })

        it('should reject non-admin adding TEE type', async function () {
            const DEFAULT_ADMIN_ROLE = await registry.DEFAULT_ADMIN_ROLE()

            await expect(
                registry.connect(user1).addTEEType(99, 'Unauthorized')
            ).to.be.reverted

            console.log('✓ Non-admin cannot add TEE type')
        })

        it('should allow admin to deactivate TEE type', async function () {
            await registry.addTEEType(TYPE_CUSTOM, 'Custom TEE')
            expect(await registry.isValidTEEType(TYPE_CUSTOM)).to.be.true

            await expect(registry.deactivateTEEType(TYPE_CUSTOM))
                .to.emit(registry, 'TEETypeDeactivated')
                .withArgs(TYPE_CUSTOM)

            expect(await registry.isValidTEEType(TYPE_CUSTOM)).to.be.false

            console.log('✓ TEE type deactivated successfully')
        })

        it('should list all TEE types', async function () {
            const [typeIds, infos] = await registry.getTEETypes()

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
            await expect(
                registry.approvePCR(pcrs, 'v1.0.0', hre.ethers.ZeroHash, 0)
            )
                .to.emit(registry, 'PCRApproved')
                .withArgs(pcrHash, 'v1.0.0')

            expect(await registry.isPCRApproved(pcrHash)).to.be.true

            const pcrInfo = await registry.approvedPCRs(pcrHash)
            expect(pcrInfo.active).to.be.true
            expect(pcrInfo.version).to.equal('v1.0.0')
            expect(pcrInfo.expiresAt).to.equal(0n)

            console.log('✓ PCR approved successfully')
        })

        it('should handle PCR versioning with grace period', async function () {
            const pcrsV2 = {
                pcr0: '0x' + Buffer.alloc(48, 0x04).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x05).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x06).toString('hex')
            }

            const pcrHashV2 = await registry.computePCRHash(pcrsV2)
            const gracePeriod = 3600 // 1 hour

            // Approve v2 with v1 as previous
            await registry.approvePCR(pcrsV2, 'v2.0.0', pcrHash, gracePeriod)

            // Both should be valid during grace period
            expect(await registry.isPCRApproved(pcrHash)).to.be.true
            expect(await registry.isPCRApproved(pcrHashV2)).to.be.true

            const pcrV1Info = await registry.approvedPCRs(pcrHash)
            expect(pcrV1Info.expiresAt).to.be.greaterThan(0n)

            console.log('✓ PCR versioning with grace period works')
        })

        it('should allow admin to revoke PCR', async function () {
            const pcrsRevoke = {
                pcr0: '0x' + Buffer.alloc(48, 0x07).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x08).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x09).toString('hex')
            }

            const pcrHashRevoke = await registry.computePCRHash(pcrsRevoke)

            await registry.approvePCR(pcrsRevoke, 'v1.0.0-revoke', hre.ethers.ZeroHash, 0)
            expect(await registry.isPCRApproved(pcrHashRevoke)).to.be.true

            await expect(registry.revokePCR(pcrHashRevoke))
                .to.emit(registry, 'PCRRevoked')
                .withArgs(pcrHashRevoke)

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

            await expect(
                registry.connect(user1).approvePCR(pcrsUnauth, 'unauthorized', hre.ethers.ZeroHash, 0)
            ).to.be.reverted

            console.log('✓ Non-admin cannot approve PCR')
        })
    })

    describe('Certificate Management', function () {
        it('should allow admin to set AWS root certificate', async function () {
            const certData = '0x' + Buffer.from('AWS ROOT CERT', 'utf8').toString('hex')
            const certHash = hre.ethers.keccak256(certData)

            await expect(registry.setAWSRootCertificate(certData))
                .to.emit(registry, 'AWSCertificateUpdated')
                .withArgs(certHash)

            const storedCert = await registry.awsRootCertificate()
            expect(storedCert).to.equal(certData)

            console.log('✓ AWS root certificate set successfully')
        })

        it('should reject non-admin setting certificate', async function () {
            const certData = '0x0102030405'

            await expect(
                registry.connect(user1).setAWSRootCertificate(certData)
            ).to.be.reverted

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

            await expect(
                registry.connect(user1).registerTEEWithAttestation(
                    dummyAttestation,
                    publicKey,
                    dummyCert,
                    user1.address,
                    'https://tee.example.com',
                    1
                )
            ).to.be.reverted

            console.log('✓ Non-operator cannot register TEE')
        })

        it('should reject invalid TEE type', async function () {
            const dummyAttestation = '0x0102030405'
            const dummyCert = '0x0607080910'

            await expect(
                registry.connect(teeOperator).registerTEEWithAttestation(
                    dummyAttestation,
                    publicKey,
                    dummyCert,
                    teeOperator.address,
                    'https://tee.example.com',
                    99 // Invalid type
                )
            ).to.be.revertedWithCustomError(registry, 'InvalidTEEType')

            console.log('✓ Invalid TEE type rejected')
        })

        it('should reject registration with invalid attestation', async function () {
            // First approve a PCR
            const pcrs = {
                pcr0: '0x' + Buffer.alloc(48, 0x01).toString('hex'),
                pcr1: '0x' + Buffer.alloc(48, 0x02).toString('hex'),
                pcr2: '0x' + Buffer.alloc(48, 0x03).toString('hex')
            }
            await registry.approvePCR(pcrs, 'v1.0.0', hre.ethers.ZeroHash, 0)

            // Add TEE type if not exists
            try {
                await registry.addTEEType(1, 'AWS Nitro')
            } catch (e) {
                // Type already exists, continue
            }

            const invalidAttestation = '0x' + Buffer.alloc(100, 0xFF).toString('hex')
            const dummyCert = '0x' + Buffer.alloc(100, 0xAA).toString('hex')

            await expect(
                registry.connect(teeOperator).registerTEEWithAttestation(
                    invalidAttestation,
                    publicKey,
                    dummyCert,
                    teeOperator.address,
                    'https://tee.example.com',
                    1
                )
            ).to.be.revertedWithCustomError(registry, 'AttestationInvalid')

            console.log('✓ Invalid attestation rejected during registration')
        })
    })

    describe('Query Functions', function () {
        it('should compute TEE ID correctly', async function () {
            const testKey = '0x0102030405'
            const expectedId = hre.ethers.keccak256(testKey)

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

            const expectedHash = hre.ethers.keccak256(
                hre.ethers.concat([pcrs.pcr0, pcrs.pcr1, pcrs.pcr2])
            )

            const computedHash = await registry.computePCRHash(pcrs)
            expect(computedHash).to.equal(expectedHash)

            console.log('✓ PCR hash computation correct')
        })

        it('should compute message hash correctly', async function () {
            const inputHash = hre.ethers.keccak256('0x01')
            const outputHash = hre.ethers.keccak256('0x02')
            const timestamp = Math.floor(Date.now() / 1000)

            const expectedHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'uint256'],
                    [inputHash, outputHash, timestamp]
                )
            )

            const computedHash = await registry.computeMessageHash(inputHash, outputHash, timestamp)
            expect(computedHash).to.equal(expectedHash)

            console.log('✓ Message hash computation correct')
        })

        it('should handle getTEE for non-existent TEE', async function () {
            const nonExistentId = hre.ethers.keccak256('0xDEADBEEF')

            await expect(registry.getTEE(nonExistentId))
                .to.be.revertedWithCustomError(registry, 'TEENotFound')

            console.log('✓ Non-existent TEE query handled correctly')
        })

        it('should return empty arrays for new owner', async function () {
            const tees = await registry.getTEEsByOwner(user2.address)
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
            await expect(registry.connect(user1).addTEEType(99, 'Test'))
                .to.be.reverted

            await expect(
                registry.connect(user1).approvePCR(
                    { pcr0: '0x01', pcr1: '0x02', pcr2: '0x03' },
                    'v1',
                    hre.ethers.ZeroHash,
                    0
                )
            ).to.be.reverted

            await expect(
                registry.connect(user1).setAWSRootCertificate('0x01')
            ).to.be.reverted

            console.log('✓ Admin role enforced correctly')
        })

        it('should allow role management by admin', async function () {
            const TEE_OPERATOR_ROLE = await registry.TEE_OPERATOR()

            // Grant role
            await registry.grantRole(TEE_OPERATOR_ROLE, user1.address)
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, user1.address)).to.be.true

            // Revoke role
            await registry.revokeRole(TEE_OPERATOR_ROLE, user1.address)
            expect(await registry.hasRole(TEE_OPERATOR_ROLE, user1.address)).to.be.false

            console.log('✓ Role management works correctly')
        })
    })
})
