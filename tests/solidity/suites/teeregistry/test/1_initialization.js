const { expect } = require('chai')
const hre = require('hardhat')

describe('TEERegistry - Initialization', function () {
    let teeRegistry
    let owner, operator, user

    before(async () => {
        [owner, operator, user] = await hre.ethers.getSigners()

        const TEERegistry = await hre.ethers.getContractFactory('TEERegistry')
        teeRegistry = await TEERegistry.deploy()
        await teeRegistry.waitForDeployment()

        console.log('TEERegistry deployed at:', await teeRegistry.getAddress())
    })

    it('should deploy successfully', async function () {
        const address = await teeRegistry.getAddress()
        expect(address).to.be.properAddress
    })

    it('should grant DEFAULT_ADMIN_ROLE to deployer', async function () {
        const DEFAULT_ADMIN_ROLE = await teeRegistry.DEFAULT_ADMIN_ROLE()
        const hasRole = await teeRegistry.hasRole(DEFAULT_ADMIN_ROLE, owner.address)
        expect(hasRole).to.be.true
    })

    it('should grant TEE_OPERATOR role to deployer', async function () {
        const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()
        const hasRole = await teeRegistry.hasRole(TEE_OPERATOR, owner.address)
        expect(hasRole).to.be.true
    })

    it('should have correct role admin setup', async function () {
        const DEFAULT_ADMIN_ROLE = await teeRegistry.DEFAULT_ADMIN_ROLE()
        const TEE_OPERATOR = await teeRegistry.TEE_OPERATOR()
        const roleAdmin = await teeRegistry.getRoleAdmin(TEE_OPERATOR)
        expect(roleAdmin).to.equal(DEFAULT_ADMIN_ROLE)
    })

    it('should have correct constants', async function () {
        const MAX_SETTLEMENT_AGE = await teeRegistry.MAX_SETTLEMENT_AGE()
        const FUTURE_TOLERANCE = await teeRegistry.FUTURE_TOLERANCE()
        const VERIFIER_ADDRESS = await teeRegistry.VERIFIER()

        expect(MAX_SETTLEMENT_AGE).to.equal(3600) // 1 hour
        expect(FUTURE_TOLERANCE).to.equal(300) // 5 minutes
        expect(VERIFIER_ADDRESS).to.equal('0x0000000000000000000000000000000000000900')
    })
})
