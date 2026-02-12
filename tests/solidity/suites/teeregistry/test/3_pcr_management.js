const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - PCR Management', function () {
    let teeRegistry
    let owner, user

    const samplePCRs = {
        pcr0: '0x' + '11'.repeat(48),
        pcr1: '0x' + '22'.repeat(48),
        pcr2: '0x' + '33'.repeat(48)
    }

    const samplePCRs2 = {
        pcr0: '0x' + 'aa'.repeat(48),
        pcr1: '0x' + 'bb'.repeat(48),
        pcr2: '0x' + 'cc'.repeat(48)
    }

    before(async () => {
        [owner, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('computePCRHash', function () {
        it('should compute PCR hash correctly', async function () {
            const hash = await teeRegistry.computePCRHash(samplePCRs)

            // Should be keccak256 of pcr0 + pcr1 + pcr2
            const expectedHash = hre.ethers.keccak256(
                hre.ethers.concat([samplePCRs.pcr0, samplePCRs.pcr1, samplePCRs.pcr2])
            )
            expect(hash).to.equal(expectedHash)
        })

        it('should produce different hashes for different PCRs', async function () {
            const hash1 = await teeRegistry.computePCRHash(samplePCRs)
            const hash2 = await teeRegistry.computePCRHash(samplePCRs2)
            expect(hash1).to.not.equal(hash2)
        })
    })

    describe('approvePCR', function () {
        let pcrHash

        it('should approve a PCR', async function () {
            pcrHash = await teeRegistry.computePCRHash(samplePCRs)

            const tx = await teeRegistry.approvePCR(
                samplePCRs,
                'v1.0.0',
                hre.ethers.ZeroHash,
                0
            )
            const receipt = await tx.wait()

            // Check event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'PCRApproved'
                } catch {
                    return false
                }
            })
            expect(event).to.not.be.undefined

            // Verify storage
            const pcr = await teeRegistry.approvedPCRs(pcrHash)
            expect(pcr.active).to.be.true
            expect(pcr.approvedAt).to.be.gt(0)
            expect(pcr.expiresAt).to.equal(0)
            expect(pcr.version).to.equal('v1.0.0')
        })

        it('should set expiry on previous PCR during upgrade', async function () {
            const oldPcrHash = await teeRegistry.computePCRHash(samplePCRs)
            const gracePeriod = 86400 // 1 day

            // Approve new PCR with previous PCR reference
            await teeRegistry.approvePCR(
                samplePCRs2,
                'v2.0.0',
                oldPcrHash,
                gracePeriod
            )

            // Old PCR should now have expiry
            const oldPcr = await teeRegistry.approvedPCRs(oldPcrHash)
            expect(oldPcr.expiresAt).to.be.gt(0)
        })

        it('should revert if not admin', async function () {
            await expect(
                teeRegistry.connect(user).approvePCR(
                    samplePCRs,
                    'v3.0.0',
                    hre.ethers.ZeroHash,
                    0
                )
            ).to.be.reverted
        })
    })

    describe('isPCRApproved', function () {
        it('should return true for approved PCR', async function () {
            const pcrHash = await teeRegistry.computePCRHash(samplePCRs2)
            const approved = await teeRegistry.isPCRApproved(pcrHash)
            expect(approved).to.be.true
        })

        it('should return false for non-existent PCR', async function () {
            const fakePCRs = {
                pcr0: '0x' + 'ff'.repeat(48),
                pcr1: '0x' + 'ee'.repeat(48),
                pcr2: '0x' + 'dd'.repeat(48)
            }
            const pcrHash = await teeRegistry.computePCRHash(fakePCRs)
            const approved = await teeRegistry.isPCRApproved(pcrHash)
            expect(approved).to.be.false
        })
    })

    describe('revokePCR', function () {
        it('should revoke a PCR', async function () {
            const pcrHash = await teeRegistry.computePCRHash(samplePCRs2)

            const tx = await teeRegistry.revokePCR(pcrHash)
            const receipt = await tx.wait()

            // Check event
            const event = receipt.logs.find(log => {
                try {
                    const parsed = teeRegistry.interface.parseLog(log)
                    return parsed.name === 'PCRRevoked'
                } catch {
                    return false
                }
            })
            expect(event).to.not.be.undefined

            // Verify PCR is no longer approved
            const approved = await teeRegistry.isPCRApproved(pcrHash)
            expect(approved).to.be.false
        })

        it('should revert if not admin', async function () {
            const pcrHash = await teeRegistry.computePCRHash(samplePCRs)
            await expect(
                teeRegistry.connect(user).revokePCR(pcrHash)
            ).to.be.reverted
        })
    })

    describe('getActivePCRs', function () {
        it('should return only active PCRs', async function () {
            // Approve a new PCR
            const newPCRs = {
                pcr0: '0x' + '44'.repeat(48),
                pcr1: '0x' + '55'.repeat(48),
                pcr2: '0x' + '66'.repeat(48)
            }
            await teeRegistry.approvePCR(newPCRs, 'v3.0.0', hre.ethers.ZeroHash, 0)

            const activePCRs = await teeRegistry.getActivePCRs()

            // Should include only non-revoked, non-expired PCRs
            expect(activePCRs.length).to.be.gte(1)

            // Verify returned PCRs are actually approved
            for (const pcrHash of activePCRs) {
                const approved = await teeRegistry.isPCRApproved(pcrHash)
                expect(approved).to.be.true
            }
        })
    })
})
