import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { network } from "hardhat";

const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";
const LOCAL_DOMAIN = 8453;
const REMOTE_DOMAIN = 10740;
const DISPATCH_FEE = 7n;
const REMOTE_ROUTER = "0x0000000000000000000000001111111111111111111111111111111111111111";

function addressToBytes32(address) {
  return `0x${address.toLowerCase().replace(/^0x/, "").padStart(64, "0")}`;
}

function encodeUint256(value) {
  return value.toString(16).padStart(64, "0");
}

function formatTransferBody(recipientBytes32, amount) {
  return `0x${recipientBytes32.slice(2)}${encodeUint256(amount)}`;
}

describe("Hyperlane HypERC20Collateral", async function () {
  const { viem, networkHelpers } = await network.connect();
  const [owner, sender, recipient] = await viem.getWalletClients();

  async function deployOfficialHypERC20CollateralFixture() {
    const token = await viem.deployContract("ERC20MinterBurnerDecimals", ["OPG", "OPG", 18]);
    const mailbox = await viem.deployContract("MockMailbox", [LOCAL_DOMAIN, DISPATCH_FEE]);
    const router = await viem.deployContract("OfficialHypERC20Collateral", [
      token.address,
      1n,
      1n,
      mailbox.address,
    ]);
    const routerAsSender = await viem.getContractAt("OfficialHypERC20Collateral", router.address, {
      client: { wallet: sender },
    });

    await router.write.initialize([ZERO_ADDRESS, ZERO_ADDRESS, owner.account.address]);
    await router.write.enrollRemoteRouter([REMOTE_DOMAIN, REMOTE_ROUTER]);
    await token.write.mint([sender.account.address, 5_000n]);

    const tokenAsSender = await viem.getContractAt("ERC20MinterBurnerDecimals", token.address, {
      client: { wallet: sender },
    });

    return { mailbox, router, routerAsSender, token, tokenAsSender };
  }

  it("requires ERC20 approval before locking collateral", async function () {
    const { routerAsSender } = await networkHelpers.loadFixture(deployOfficialHypERC20CollateralFixture);

    await viem.assertions.revertWith(
      routerAsSender.write.transferRemote([REMOTE_DOMAIN, addressToBytes32(recipient.account.address), 1_000n], {
        value: DISPATCH_FEE,
      }),
      "ERC20: insufficient allowance",
    );
  });

  it("locks collateral and dispatches on outbound transfer", async function () {
    const { mailbox, router, routerAsSender, token, tokenAsSender } =
      await networkHelpers.loadFixture(deployOfficialHypERC20CollateralFixture);
    const amount = 1_000n;
    const recipientBytes32 = addressToBytes32(recipient.account.address);
    const expectedBody = formatTransferBody(recipientBytes32, amount);

    await tokenAsSender.write.approve([router.address, amount]);
    await routerAsSender.write.transferRemote([REMOTE_DOMAIN, recipientBytes32, amount], {
      value: DISPATCH_FEE,
    });

    assert.equal(await token.read.balanceOf([sender.account.address]), 4_000n);
    assert.equal(await token.read.balanceOf([router.address]), amount);

    assert.equal((await mailbox.read.lastSender()).toLowerCase(), router.address.toLowerCase());
    assert.equal(await mailbox.read.lastDestination(), REMOTE_DOMAIN);
    assert.equal(await mailbox.read.lastRecipient(), REMOTE_ROUTER);
    assert.equal(await mailbox.read.lastBody(), expectedBody);
    assert.equal(await mailbox.read.lastValue(), DISPATCH_FEE);
  });

  it("releases collateral on inbound delivery", async function () {
    const { mailbox, router, token } = await networkHelpers.loadFixture(deployOfficialHypERC20CollateralFixture);
    const amount = 750n;
    const recipientBytes32 = addressToBytes32(recipient.account.address);
    const body = formatTransferBody(recipientBytes32, amount);

    await token.write.mint([router.address, amount]);
    await mailbox.write.deliver([router.address, REMOTE_DOMAIN, REMOTE_ROUTER, body]);

    assert.equal(await token.read.balanceOf([router.address]), 0n);
    assert.equal(await token.read.balanceOf([recipient.account.address]), amount);
  });
});
