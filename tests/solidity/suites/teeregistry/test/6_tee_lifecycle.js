const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - TEE Lifecycle', function () {
    let teeRegistry
    let owner, user

    // Mock TEE ID for testing lifecycle
    const mockTeeId = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('mock-tee-1'))

    before(async () => {
        [owner, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('deactivateTEE', function () {
        it('should revert if TEE not found', async function () {
            await expect(
                teeRegistry.deactivateTEE(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })

        // Note: Full lifecycle testing requires a registered TEE
        // These would work when integrated with the precompile
    })

    describe('activateTEE', function () {
        it('should revert if TEE not found', async function () {
            await expect(
                teeRegistry.activateTEE(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })
    })

    describe('query functions', function () {
        it('getTEE should revert if not found', async function () {
            await expect(
                teeRegistry.getTEE(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })

        it('getPublicKey should revert if not found', async function () {
            await expect(
                teeRegistry.getPublicKey(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })

        it('getTLSCertificate should revert if not found', async function () {
            await expect(
                teeRegistry.getTLSCertificate(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })

        it('getPaymentAddress should revert if not found', async function () {
            await expect(
                teeRegistry.getPaymentAddress(mockTeeId)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEENotFound')
        })

        it('isActive should return false for non-existent TEE', async function () {
            const active = await teeRegistry.isActive(mockTeeId)
            expect(active).to.be.false
        })

        it('getActiveTEEs should return empty array initially', async function () {
            const activeTEEs = await teeRegistry.getActiveTEEs()
            expect(activeTEEs.length).to.equal(0)
        })

        it('getTEEsByType should return empty array for type', async function () {
            const tees = await teeRegistry.getTEEsByType(1)
            expect(tees.length).to.equal(0)
        })

        it('getTEEsByOwner should return empty array for owner', async function () {
            const tees = await teeRegistry.getTEEsByOwner(owner.address)
            expect(tees.length).to.equal(0)
        })
    })
})
