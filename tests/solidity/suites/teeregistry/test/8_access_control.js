const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - Access Control', function () {
    let teeRegistry
    let owner, operator, user

    const samplePCRs = {
        pcr0: '0x' + '11'.repeat(48),
        pcr1: '0x' + '22'.repeat(48),
        pcr2: '0x' + '33'.repeat(48)
    }

    before(async () => {
        [owner, operator, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('Role Management', function () {
        it('should allow admin to grant TEE_OPERATOR role', async function () {
            const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()

            await teeRegistry.grantRole(TEE_OPERATOR, operator.address)

            const hasRole = await teeRegistry.hasRole(TEE_OPERATOR, operator.address)
            expect(hasRole).to.be.true
        })

        it('should allow admin to revoke TEE_OPERATOR role', async function () {
            const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()

            await teeRegistry.revokeRole(TEE_OPERATOR, operator.address)

            const hasRole = await teeRegistry.hasRole(TEE_OPERATOR, operator.address)
            expect(hasRole).to.be.false
        })

        it('should not allow non-admin to grant roles', async function () {
            const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()

            await expect(
                teeRegistry.connect(user).grantRole(TEE_OPERATOR, user.address)
            ).to.be.reverted
        })

        it('should allow admin to transfer admin role', async function () {
            const DEFAULT_ADMIN_ROLE = await teeRegistry.DEFAULT_ADMIN_ROLE()

            await teeRegistry.grantRole(DEFAULT_ADMIN_ROLE, operator.address)

            const hasRole = await teeRegistry.hasRole(DEFAULT_ADMIN_ROLE, operator.address)
            expect(hasRole).to.be.true

            // Clean up: revoke for other tests
            await teeRegistry.revokeRole(DEFAULT_ADMIN_ROLE, operator.address)
        })
    })

    describe('Admin-Only Functions', function () {
        it('addTEEType requires admin', async function () {
            await expect(
                teeRegistry.connect(user).addTEEType(10, 'Test')
            ).to.be.reverted
        })

        it('deactivateTEEType requires admin', async function () {
            await teeRegistry.addTEEType(10, 'Test')

            await expect(
                teeRegistry.connect(user).deactivateTEEType(10)
            ).to.be.reverted
        })

        it('approvePCR requires admin', async function () {
            await expect(
                teeRegistry.connect(user).approvePCR(
                    samplePCRs,
                    'v1.0.0',
                    hre.ethers.ZeroHash,
                    0
                )
            ).to.be.reverted
        })

        it('revokePCR requires admin', async function () {
            const pcrHash = await teeRegistry.computePCRHash(samplePCRs)

            await expect(
                teeRegistry.connect(user).revokePCR(pcrHash)
            ).to.be.reverted
        })

        it('setAWSRootCertificate requires admin', async function () {
            await expect(
                teeRegistry.connect(user).setAWSRootCertificate('0x1234')
            ).to.be.reverted
        })
    })

    describe('Operator-Only Functions', function () {
        const sampleAttestationDoc = '0x' + 'aa'.repeat(200)
        const sampleSigningPublicKey = '0x' + 'bb'.repeat(256)
        const sampleTLSCertificate = '0x' + 'cc'.repeat(200)

        before(async () => {
            // Grant operator role for these tests
            const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()
            await teeRegistry.grantRole(TEE_OPERATOR, operator.address)

            // Setup requirements
            await teeRegistry.addTEEType(11, 'Test Type')
            await teeRegistry.approvePCR(samplePCRs, 'v1.0.0', hre.ethers.ZeroHash, 0)
            await teeRegistry.setAWSRootCertificate('0x' + 'dd'.repeat(200))
        })

        it('registerTEEWithAttestation requires operator', async function () {
            await expect(
                teeRegistry.connect(user).registerTEEWithAttestation(
                    sampleAttestationDoc,
                    sampleSigningPublicKey,
                    sampleTLSCertificate,
                    user.address,
                    'https://example.com',
                    11
                )
            ).to.be.reverted
        })
    })

    describe('View Functions', function () {
        it('anyone can call view functions', async function () {
            // These should not revert for any caller
            await teeRegistry.connect(user).getTEETypes()
            await teeRegistry.connect(user).getActivePCRs()
            await teeRegistry.connect(user).getActiveTEEs()
            await teeRegistry.connect(user).isValidTEEType(1)

            const pcrHash = await teeRegistry.computePCRHash(samplePCRs)
            await teeRegistry.connect(user).isPCRApproved(pcrHash)
        })
    })
})
