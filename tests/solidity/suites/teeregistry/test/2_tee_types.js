const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - TEE Type Management', function () {
    let teeRegistry
    let owner, operator, user

    before(async () => {
        [owner, operator, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('addTEEType', function () {
        it('should add a TEE type', async function () {
            const tx = await teeRegistry.addTEEType(1, 'AWS Nitro')
            const receipt = await tx.wait()

            // Check event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'TEETypeAdded'
                } catch {
                    return false
                }
            })
            expect(event).to.not.be.undefined

            // Verify storage
            const teeType = await teeRegistry.teeTypes(1)
            expect(teeType.name).to.equal('AWS Nitro')
            expect(teeType.active).to.be.true
            expect(teeType.addedAt).to.be.gt(0)
        })

        it('should add multiple TEE types', async function () {
            await teeRegistry.addTEEType(2, 'Intel SGX')
            await teeRegistry.addTEEType(3, 'AMD SEV')

            const [typeIds, infos] = await teeRegistry.getTEETypes()
            expect(typeIds.length).to.equal(3)
            expect(infos.length).to.equal(3)
            expect(infos[1].name).to.equal('Intel SGX')
            expect(infos[2].name).to.equal('AMD SEV')
        })

        it('should revert if TEE type already exists', async function () {
            await expect(
                teeRegistry.addTEEType(1, 'Duplicate')
            ).to.be.revertedWithCustomError(teeRegistry, 'TEETypeExists')
        })

        it('should revert if not admin', async function () {
            await expect(
                teeRegistry.connect(user).addTEEType(4, 'Unauthorized')
            ).to.be.reverted
        })
    })

    describe('deactivateTEEType', function () {
        it('should deactivate a TEE type', async function () {
            const tx = await teeRegistry.deactivateTEEType(1)
            const receipt = await tx.wait()

            // Check event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'TEETypeDeactivated'
                } catch {
                    return false
                }
            })
            expect(event).to.not.be.undefined

            // Verify storage
            const teeType = await teeRegistry.teeTypes(1)
            expect(teeType.active).to.be.false
        })

        it('should revert if TEE type not found', async function () {
            await expect(
                teeRegistry.deactivateTEEType(99)
            ).to.be.revertedWithCustomError(teeRegistry, 'TEETypeNotFound')
        })

        it('should revert if not admin', async function () {
            await expect(
                teeRegistry.connect(user).deactivateTEEType(2)
            ).to.be.reverted
        })
    })

    describe('isValidTEEType', function () {
        it('should return false for deactivated type', async function () {
            const valid = await teeRegistry.isValidTEEType(1)
            expect(valid).to.be.false
        })

        it('should return true for active type', async function () {
            const valid = await teeRegistry.isValidTEEType(2)
            expect(valid).to.be.true
        })

        it('should return false for non-existent type', async function () {
            const valid = await teeRegistry.isValidTEEType(99)
            expect(valid).to.be.false
        })
    })

    describe('getTEETypes', function () {
        it('should return all TEE types', async function () {
            const [typeIds, infos] = await teeRegistry.getTEETypes()
            expect(typeIds.length).to.equal(3)
            expect(typeIds[0]).to.equal(1)
            expect(typeIds[1]).to.equal(2)
            expect(typeIds[2]).to.equal(3)
        })
    })
})
