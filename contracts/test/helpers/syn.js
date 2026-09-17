const { ethers } = require("hardhat");

/**
 * Deploys a real SynCoin + SYNTHOSSynBridgeMinter pair the same way
 * production deploy scripts do: zero initial supply, minter wired up via
 * the one-time initializeBridgeMinter() call, minter unpaused and ready.
 * `relayerSigners` defaults to a single signer with threshold 1, which is
 * enough for tests to mint deterministically while still exercising the
 * real signature/quorum-gated mint path (not a shortcut around it).
 */
async function deploySynWithBridge(treasury, relayerSigners, threshold = 1) {
  const relayers = (relayerSigners || [treasury]).map((s) =>
    typeof s === "string" ? s : s.address
  );

  const SynCoin = await ethers.getContractFactory("SynCoin");
  const syn = await SynCoin.deploy(typeof treasury === "string" ? treasury : treasury.address);
  await syn.waitForDeployment();

  const Minter = await ethers.getContractFactory("SYNTHOSSynBridgeMinter");
  const minter = await Minter.deploy(await syn.getAddress(), relayers, threshold);
  await minter.waitForDeployment();

  await (await syn.initializeBridgeMinter(await minter.getAddress())).wait();
  await (await minter.unpause()).wait();

  return { syn, minter };
}

let mintCounter = 0;

/**
 * Mints `amount` of wrapped SYN to `recipient` through the real
 * relayer-quorum mint path (approveMint), signing with each of
 * `relayerSigners` in turn until quorum is reached. This is how every test
 * that needs a SYN balance gets one -- there is no other way to create SYN
 * on this contract, on purpose.
 */
async function mintSyn(minter, relayerSigners, recipient, amount, label) {
  mintCounter += 1;
  const sourceEventId = ethers.keccak256(
    ethers.toUtf8Bytes(`test-native-lock-${label || "mint"}-${mintCounter}`)
  );
  const recipientAddress = typeof recipient === "string" ? recipient : recipient.address;
  const signers = Array.isArray(relayerSigners) ? relayerSigners : [relayerSigners];

  let receipt;
  for (const signer of signers) {
    const tx = await minter.connect(signer).approveMint(sourceEventId, recipientAddress, amount);
    receipt = await tx.wait();
  }
  return receipt;
}

module.exports = { deploySynWithBridge, mintSyn };
