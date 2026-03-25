import hardhatToolboxViemPlugin from "@nomicfoundation/hardhat-toolbox-viem";
import { defineConfig } from "hardhat/config";

const hyperlaneCompiler = {
  version: "0.8.22",
  settings: {
    optimizer: {
      enabled: true,
      runs: 25000,
    },
    evmVersion: "paris",
    viaIR: true,
  },
};

const hyperlaneSources = [
  "./solidity/bridge/OfficialHypERC20Collateral.sol",
  "./solidity/hyperlane/PackageVersioned.sol",
  "./solidity/hyperlane/client/GasRouter.sol",
  "./solidity/hyperlane/client/MailboxClient.sol",
  "./solidity/hyperlane/client/Router.sol",
  "./solidity/hyperlane/hooks/libs/StandardHookMetadata.sol",
  "./solidity/hyperlane/interfaces/IInterchainSecurityModule.sol",
  "./solidity/hyperlane/interfaces/IMailbox.sol",
  "./solidity/hyperlane/interfaces/IMessageRecipient.sol",
  "./solidity/hyperlane/interfaces/ITokenBridge.sol",
  "./solidity/hyperlane/interfaces/hooks/IPostDispatchHook.sol",
  "./solidity/hyperlane/libs/EnumerableMapExtended.sol",
  "./solidity/hyperlane/libs/Message.sol",
  "./solidity/hyperlane/libs/TypeCasts.sol",
  "./solidity/hyperlane/token/HypERC20Collateral.sol",
  "./solidity/hyperlane/token/interfaces/IWETH.sol",
  "./solidity/hyperlane/token/libs/LpCollateralRouter.sol",
  "./solidity/hyperlane/token/libs/MovableCollateralRouter.sol",
  "./solidity/hyperlane/token/libs/Quotes.sol",
  "./solidity/hyperlane/token/libs/TokenCollateral.sol",
  "./solidity/hyperlane/token/libs/TokenMessage.sol",
  "./solidity/hyperlane/token/libs/TokenRouter.sol",
];

export default defineConfig({
  plugins: [hardhatToolboxViemPlugin],
  solidity: {
    compilers: [
      hyperlaneCompiler,
      {
        version: "0.8.20",
        settings: {
          optimizer: {
            enabled: true,
            runs: 100,
          },
          viaIR: true,
        },
      },
      // This version is required to compile the werc9 contract.
      {
        version: "0.4.22",
      },
    ],
    overrides: Object.fromEntries(
      hyperlaneSources.map((source) => [source, hyperlaneCompiler]),
    ),
  },
  paths: {
    sources: "./solidity",
    exclude: ["**/lib/**"]
  },
});
