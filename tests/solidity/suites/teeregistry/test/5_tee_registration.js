const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - TEE Registration', function () {
    let teeRegistry
    let owner, operator, user

    // Sample data for TEE registration
    const samplePCRs = {
        pcr0: '0x' + '11'.repeat(48),
        pcr1: '0x' + '22'.repeat(48),
        pcr2: '0x' + '33'.repeat(48)
    }

    const sampleAttestationDoc = '0x' + 'aa'.repeat(200)
    const sampleSigningPublicKey = '0x' + 'bb'.repeat(256)
    const sampleTLSCertificate = '0x' + 'cc'.repeat(200)
    const sampleAWSRootCert = '0x' + 'dd'.repeat(200)
    const sampleEndpoint = 'https://tee.example.com'

    before(async () => {
        [owner, operator, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()

        // Setup: Add TEE type, approve PCR, set AWS certificate
        await teeRegistry.addTEEType(1, 'AWS Nitro')
        await teeRegistry.approvePCR(samplePCRs, 'v1.0.0', hre.ethers.ZeroHash, 0)
        await teeRegistry.setAWSRootCertificate(sampleAWSRootCert)

        // Grant operator role
        const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()
        await teeRegistry.grantRole(TEE_OPERATOR, operator.address)
    })

    describe('registerTEEWithAttestation', function () {
        it('should revert with invalid TEE type', async function () {
            await expect(
                teeRegistry.connect(operator).registerTEEWithAttestation(
                    sampleAttestationDoc,
                    sampleSigningPublicKey,
                    sampleTLSCertificate,
                    operator.address,
                    sampleEndpoint,
                    99 // Invalid type
                )
            ).to.be.revertedWithCustomError(teeRegistry, 'InvalidTEEType')
        })

        it('should revert if not operator', async function () {
            await expect(
                teeRegistry.connect(user).registerTEEWithAttestation(
                    sampleAttestationDoc,
                    sampleSigningPublicKey,
                    sampleTLSCertificate,
                    user.address,
                    sampleEndpoint,
                    1
                )
            ).to.be.reverted
        })

        // Note: Full registration testing requires the precompile to be available
        // These tests will work when running against cosmos network with the precompile
        it('should compute TEE ID correctly', async function () {
            const teeId = await teeRegistry.computeTEEId(sampleSigningPublicKey)
            const expectedId = hre.ethers.keccak256(sampleSigningPublicKey)
            expect(teeId).to.equal(expectedId)
        })
    })

    describe('computeTEEId', function () {
        it('should compute TEE ID from public key', async function () {
            const publicKey = '0x' + '12'.repeat(256)
            const teeId = await teeRegistry.computeTEEId(publicKey)
            const expectedId = hre.ethers.keccak256(publicKey)
            expect(teeId).to.equal(expectedId)
        })

        it('should produce different IDs for different keys', async function () {
            const key1 = '0x' + '11'.repeat(256)
            const key2 = '0x' + '22'.repeat(256)

            const id1 = await teeRegistry.computeTEEId(key1)
            const id2 = await teeRegistry.computeTEEId(key2)

            expect(id1).to.not.equal(id2)
        })
    })
})
