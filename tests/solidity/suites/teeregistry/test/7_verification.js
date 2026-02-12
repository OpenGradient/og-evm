const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - Verification', function () {
    let teeRegistry
    let owner

    const mockTeeId = hre.ethers.keccak256(hre.ethers.toUtf8Bytes('mock-tee-1'))
    const inputHash = hre.ethers.randomBytes(32)
    const outputHash = hre.ethers.randomBytes(32)
    const timestamp = Math.floor(Date.now() / 1000)
    const mockSignature = '0x' + 'aa'.repeat(256)

    before(async () => {
        [owner] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()
    })

    describe('computeMessageHash', function () {
        it('should compute message hash correctly', async function () {
            const hash = await teeRegistry.computeMessageHash(
                inputHash,
                outputHash,
                timestamp
            )

            // Should be keccak256 of inputHash + outputHash + timestamp
            const expectedHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'uint256'],
                    [inputHash, outputHash, timestamp]
                )
            )
            expect(hash).to.equal(expectedHash)
        })

        it('should produce different hashes for different inputs', async function () {
            const hash1 = await teeRegistry.computeMessageHash(inputHash, outputHash, timestamp)
            const hash2 = await teeRegistry.computeMessageHash(
                hre.ethers.randomBytes(32),
                outputHash,
                timestamp
            )
            expect(hash1).to.not.equal(hash2)
        })
    })

    describe('verifySignature', function () {
        it('should return false for non-existent TEE', async function () {
            const valid = await teeRegistry.verifySignature(
                mockTeeId,
                inputHash,
                outputHash,
                timestamp,
                mockSignature
            )
            expect(valid).to.be.false
        })

        // Note: Full signature verification testing requires:
        // 1. A registered TEE with valid public key
        // 2. The precompile to be available
        // These tests will work when running against cosmos network
    })

    describe('verifySettlement', function () {
        it('should revert with timestamp too old', async function () {
            const oldTimestamp = Math.floor(Date.now() / 1000) - 7200 // 2 hours ago

            await expect(
                teeRegistry.verifySettlement(
                    mockTeeId,
                    inputHash,
                    outputHash,
                    oldTimestamp,
                    mockSignature
                )
            ).to.be.revertedWithCustomError(teeRegistry, 'TimestampTooOld')
        })

        it('should revert with timestamp in future', async function () {
            const futureTimestamp = Math.floor(Date.now() / 1000) + 600 // 10 minutes in future

            await expect(
                teeRegistry.verifySettlement(
                    mockTeeId,
                    inputHash,
                    outputHash,
                    futureTimestamp,
                    mockSignature
                )
            ).to.be.revertedWithCustomError(teeRegistry, 'TimestampInFuture')
        })

        // Note: Full settlement verification requires:
        // 1. Valid timestamp within acceptable range
        // 2. Active TEE with valid signature
        // 3. Precompile available for signature verification
    })

    describe('settlement replay protection', function () {
        it('should track settlement usage in mapping', async function () {
            const settlementHash = hre.ethers.keccak256(
                hre.ethers.solidityPacked(
                    ['bytes32', 'bytes32', 'bytes32', 'uint256'],
                    [mockTeeId, inputHash, outputHash, timestamp]
                )
            )

            const used = await teeRegistry.settlementUsed(settlementHash)
            expect(used).to.be.false
        })
    })
})
