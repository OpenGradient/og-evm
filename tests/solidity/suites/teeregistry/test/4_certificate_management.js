const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - Certificate Management', function () {
    let teeRegistry
    let owner, user

    const sampleCertificate = '0x' + '01'.repeat(100)
    const updatedCertificate = '0x' + '02'.repeat(100)

    before(async () => {
        [owner, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('setAWSRootCertificate', function () {
        it('should set AWS root certificate', async function () {
            const tx = await teeRegistry.setAWSRootCertificate(sampleCertificate)
            const receipt = await tx.wait()

            // Check event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'AWSCertificateUpdated'
                } catch {
                    return false
                }
            })
            expect(event).to.not.be.undefined

            // Verify storage
            const storedCert = await teeRegistry.awsRootCertificate()
            expect(storedCert).to.equal(sampleCertificate)
        })

        it('should update AWS root certificate', async function () {
            await teeRegistry.setAWSRootCertificate(updatedCertificate)

            const storedCert = await teeRegistry.awsRootCertificate()
            expect(storedCert).to.equal(updatedCertificate)
        })

        it('should emit correct hash in event', async function () {
            const tx = await teeRegistry.setAWSRootCertificate(sampleCertificate)
            const receipt = await tx.wait()

            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'AWSCertificateUpdated'
                } catch {
                    return false
                }
            })

            const expectedHash = hre.ethers.keccak256(sampleCertificate)
            const parsedEvent = teeRegistry.interface.parseLog(event)
            expect(parsedEvent.args.certHash).to.equal(expectedHash)
        })

        it('should revert if not admin', async function () {
            await expect(
                teeRegistry.connect(user).setAWSRootCertificate(sampleCertificate)
            ).to.be.reverted
        })
    })
})
