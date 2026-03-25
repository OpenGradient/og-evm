import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { network } from "hardhat";

const PRECOMPILE_ADDRESS = "0x0000000000000000000000000000000000000A00";
const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";
const LOCAL_DOMAIN = 10740;
const REMOTE_DOMAIN = 8453;
const DISPATCH_FEE = 7n;
const REMOTE_ROUTER = "0x0000000000000000000000001111111111111111111111111111111111111111";
const REMOTE_RECIPIENT = "0x0000000000000000000000002222222222222222222222222222222222222222";
const WRONG_REMOTE_ROUTER = "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const LOCAL_RECIPIENT = "0x3333333333333333333333333333333333333333";

function addressToBytes32(address) {
  return `0x${address.toLowerCase().replace(/^0x/, "").padStart(64, "0")}`;
}

function encodeUint256(value) {
  return value.toString(16).padStart(64, "0");
}

function formatTransferBody(recipientBytes32, amount) {
  return `0x${recipientBytes32.slice(2)}${encodeUint256(amount)}`;
}

describe("HypOGNative", async function () {
  const { viem, networkHelpers } = await network.connect();
  const publicClient = await viem.getPublicClient();
  const [owner] = await viem.getWalletClients();

  async function deployHypOGNativeFixture() {
    const mailbox = await viem.deployContract("MockMailbox", [LOCAL_DOMAIN, DISPATCH_FEE]);
    const precompileSource = await viem.deployContract("MockBridgeMint");
    const runtimeCode = await publicClient.getCode({ address: precompileSource.address });

    assert.ok(runtimeCode !== undefined && runtimeCode !== "0x");

    await networkHelpers.setCode(PRECOMPILE_ADDRESS, runtimeCode);

    const bridgeMint = await viem.getContractAt("MockBridgeMint", PRECOMPILE_ADDRESS);
    await bridgeMint.write.initialize();

    const router = await viem.deployContract("HypOGNative", [
      mailbox.address,
      ZERO_ADDRESS,
      owner.account.address,
    ]);
    await router.write.enrollRemoteRouter([REMOTE_DOMAIN, REMOTE_ROUTER]);

    return { bridgeMint, mailbox, router };
  }

  it("mints on inbound handle", async function () {
    const { bridgeMint, mailbox, router } =
      await networkHelpers.loadFixture(deployHypOGNativeFixture);
    const recipientBytes32 = addressToBytes32(LOCAL_RECIPIENT);
    const amount = 12345n;
    const body = formatTransferBody(recipientBytes32, amount);

    await viem.assertions.emitWithArgs(
      mailbox.write.deliver([router.address, REMOTE_DOMAIN, REMOTE_ROUTER, body]),
      router,
      "ReceivedTransferRemote",
      [REMOTE_DOMAIN, recipientBytes32, amount],
    );

    assert.equal(await bridgeMint.read.lastMintRecipient(), LOCAL_RECIPIENT.toLowerCase());
    assert.equal(await bridgeMint.read.lastMintAmount(), amount);
    assert.equal((await bridgeMint.read.lastMintCaller()).toLowerCase(), router.address.toLowerCase());
    assert.equal(await bridgeMint.read.mintCalls(), 1n);
  });

  it("burns and dispatches on outbound transfer", async function () {
    const { bridgeMint, mailbox, router } =
      await networkHelpers.loadFixture(deployHypOGNativeFixture);
    const amount = 1000n;
    const msgValue = amount + DISPATCH_FEE;
    const expectedBody = formatTransferBody(REMOTE_RECIPIENT, amount);

    await viem.assertions.emitWithArgs(
      router.write.transferRemote([REMOTE_DOMAIN, REMOTE_RECIPIENT, amount], {
        value: msgValue,
      }),
      router,
      "SentTransferRemote",
      [REMOTE_DOMAIN, REMOTE_RECIPIENT, amount],
    );

    assert.equal(await bridgeMint.read.lastBurnAmount(), amount);
    assert.equal((await bridgeMint.read.lastBurnCaller()).toLowerCase(), router.address.toLowerCase());
    assert.equal(await bridgeMint.read.burnCalls(), 1n);

    assert.equal(await mailbox.read.lastDestination(), REMOTE_DOMAIN);
    assert.equal(await mailbox.read.lastRecipient(), REMOTE_ROUTER);
    assert.equal(await mailbox.read.lastBody(), expectedBody);
    assert.equal(await mailbox.read.lastValue(), DISPATCH_FEE);
  });

  it("rejects inbound messages from an unknown router", async function () {
    const { mailbox, router } = await networkHelpers.loadFixture(deployHypOGNativeFixture);
    const body = formatTransferBody(addressToBytes32(LOCAL_RECIPIENT), 1n);

    await viem.assertions.revertWithCustomErrorWithArgs(
      mailbox.write.deliver([router.address, REMOTE_DOMAIN, WRONG_REMOTE_ROUTER, body]),
      router,
      "UnknownRouter",
      [REMOTE_DOMAIN],
    );
  });

  it("rejects outbound transfers when the bridge is disabled", async function () {
    const { bridgeMint, router } = await networkHelpers.loadFixture(deployHypOGNativeFixture);
    await bridgeMint.write.setEnabled([false]);

    await viem.assertions.revertWithCustomError(
      router.write.transferRemote([REMOTE_DOMAIN, REMOTE_RECIPIENT, 1000n], {
        value: 1007n,
      }),
      router,
      "BridgeDisabled",
    );
  });
});
