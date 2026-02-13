const HDWalletProvider = require('@truffle/hdwallet-provider')

// Test accounts private keys (dev0, dev1, dev2 from local_node.sh)
const privateKeys = [
  '0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305', // dev0
  '0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544', // dev1
  '0x3b7955d25189c99a7468192fcbc6429205c158834053ebe3f78f4512ab432db9', // dev2
  '0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a'  // dev3 (additional test account)
]

module.exports = {
  networks: {
    // Development network is just left as truffle's default settings
    cosmos: {
      provider: () =>
        new HDWalletProvider({
          privateKeys: privateKeys,
          providerOrUrl: 'http://127.0.0.1:8545',
          numberOfAddresses: 4
        }),
      network_id: '*', // Any network (default: none)
      gas: 5000000, // Gas sent with each transaction
      gasPrice: 1000000000 // 1 gwei (in wei)
    }
  },
  compilers: {
    solc: {
      version: '0.8.20',
      settings: {
        optimizer: {
          enabled: true,
          runs: 200
        }
      }
    }
  }
}
