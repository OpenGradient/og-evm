# OpenGradient Blockchain Node

[![CI Tests](https://github.com/OpenGradient/og-evm/actions/workflows/ci.yml/badge.svg)](https://github.com/OpenGradient/og-evm/actions/workflows/ci.yml)

`og-evm` is the official node software for participating in the OpenGradient blockchain network.

## What is OpenGradient?

[OpenGradient](https://docs.opengradient.ai/about/) is the world's first EVM-compatible blockchain network that extends standard blockchain capabilities with native AI inference. It enables developers to execute machine learning and large language models atomically as part of blockchain transactions, bridging the gap between artificial intelligence and decentralized applications.

### Key Features

- **Native AI Inference** - Execute ML/LLM models directly within smart contracts as atomic transactions
- **EVM Compatibility** - Full Ethereum compatibility with Solidity smart contracts, JSON-RPC, and EVM wallet support
- **SolidML Framework** - Solidity framework for building AI-enabled on-chain applications
- **Model Hub** - Decentralized repository for uploading, browsing, and using AI models
- **Heterogeneous Compute** - Network architecture with Full Nodes, Inference Nodes, Storage Nodes, and Data Nodes
- **Multi-layer Verification** - Security through ZKML (zero-knowledge machine learning), TEE (trusted execution environments), and cryptoeconomic guarantees

For more information, visit the [OpenGradient documentation](https://docs.opengradient.ai/).

## Built on Cosmos EVM

OpenGradient is built on [Cosmos EVM](https://evm.cosmos.network/), a battle-tested framework that brings full Ethereum compatibility to the Cosmos ecosystem. This foundation provides OpenGradient with:

- **Full Ethereum Compatibility** - Solidity smart contracts, Ethereum JSON-RPC, and native support for EVM wallets like [MetaMask](https://metamask.io/) and [Rabby](https://rabby.io/)
- **Cosmos SDK Integration** - Access to the Cosmos ecosystem including [IBC](https://github.com/cosmos/ibc-go) for cross-chain communication
- **EVM Extensions** - Custom precompiles that expose Cosmos SDK functionality to Solidity contracts
- **Native ERC-20 Support** - Unified token representation across IBC and ERC-20 standards
- **EIP-1559 Fee Market** - Modern fee mechanism based on [EIP-1559](https://eips.ethereum.org/EIPS/eip-1559)
- **EIP-712 Signing** - Structured data signing for seamless wallet integration

OpenGradient extends this foundation with AI-native capabilities, enabling a new class of intelligent on-chain applications.

## Ethereum Compatibility

OpenGradient inherits Cosmos EVM's **forward-compatible** approach to Ethereum. This means it can run any valid smart contract from Ethereum while also supporting additional features not yet available on the standard Ethereum VM—including native AI inference capabilities.

## Getting Started

To run a local OpenGradient node, execute the following from the root of the repository:

```bash
./local_node.sh
```

For detailed instructions on joining the OpenGradient network, see the [official documentation](https://docs.opengradient.ai/).

## Development

### Testing

All test scripts are available in the `Makefile`:

```bash
# Unit tests
make test-unit

# Coverage report (generates filtered_coverage.txt)
make test-unit-cover

# Fuzz testing
make test-fuzz

# Solidity tests
make test-solidity

# Benchmarks
make benchmark
```

## Documentation

- [OpenGradient Documentation](https://docs.opengradient.ai/) - Official docs for the OpenGradient network
- [Cosmos EVM Documentation](https://evm.cosmos.network/) - Documentation for the underlying EVM framework

### Cosmos Stack

OpenGradient leverages the following core Cosmos technologies:

- [Cosmos SDK](https://github.com/cosmos/cosmos-sdk) - Framework for building blockchain applications in Go
- [IBC Protocol](https://github.com/cosmos/ibc-go/) - Inter-Blockchain Communication for cross-chain interoperability
- [CometBFT](https://github.com/cometbft/cometbft) - High-performance BFT consensus engine

## License & Acknowledgments

This project is open-source under the Apache 2.0 license.

OpenGradient is built on [Cosmos EVM](https://github.com/cosmos/evm), which is itself a fork of [evmOS](https://github.com/evmos/OS). We acknowledge the foundational work by Tharsis and the evmOS team in bringing EVM compatibility to the Cosmos ecosystem, as well as the continued development by Cosmos Labs and contributors including [B-Harvest](https://bharvest.io/) and [Mantra](https://www.mantrachain.io/).

## Contributing

We welcome contributions! Please see our [contributing guide](./CONTRIBUTING.md) for more information.
