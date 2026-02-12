require("@nomicfoundation/hardhat-toolbox");

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    compilers: [
      {
        version: "0.8.18",
      },
      // This version is required to compile the werc9 contract.
      {
        version: "0.4.22",
      },
    ],
  },
  networks: {
    cosmos: {
      url: "http://127.0.0.1:8545",
      chainId: 262144,
      accounts: [
        "0x88cbead91aee890d27bf06e003ade3d4e952427e88f88d31d61d3ef5e5d54305", // dev0
        "0x741de4f8988ea941d3ff0287911ca4074e62b7d45c991a51186455366f10b544", // dev1
        "0x3b7955d25189c99a7468192fcbc6429205c158834053ebe3f78f4512ab432db9", // dev2
      ],
    },
  },
};
