const hre = require("hardhat");
const { ethers, network } = hre;

/**
 * Deploys just the bridge-pegged wrapper pair: SynCoin (the ERC-20 itself)
 * plus SYNTHOSSynBridgeMinter (the only thing that can ever mint or burn
 * it). Intended for Base (mainnet: `base`, testnet: `baseSepolia`). Keep
 * this minimal deploy separate from the full governance/staking stack in
 * deploy-synthos.js.
 *
 * SynCoin never mints an independent supply here or anywhere else on the
 * EVM side. The native SYNTHOS chain is the sole sovereign ledger and the
 * only place SYN is ever created; this wrapper starts at zero supply and
 * can only be minted by SYNTHOSSynBridgeMinter, and only when its relayer
 * quorum has verified a matching amount of real SYN locked on the native
 * chain. Nothing minted here is a second, independent supply -- it is a
 * one-for-one claim on coins already locked at home.
 */
async function main() {
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);

  console.log("SYNTHOS bridge-pegged token deploy");
  console.log(`Network:  ${network.name} (chainId ${network.config.chainId})`);
  console.log(`Deployer: ${deployer.address}`);
  console.log(`Gas balance: ${ethers.formatEther(balance)} ETH`);

  if (balance === 0n) {
    throw new Error(
      "Deployer has 0 ETH for gas on this network. Fund " +
        deployer.address +
        " with a small amount of ETH before deploying."
    );
  }

  const treasuryWallet = process.env.TREASURY_WALLET || deployer.address;
  const relayers = (process.env.SYN_BRIDGE_RELAYERS || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  const bridgeRelayers = relayers.length > 0 ? relayers : [deployer.address];
  const bridgeThreshold = BigInt(
    process.env.SYN_BRIDGE_THRESHOLD || (bridgeRelayers.length >= 2 ? "2" : "1")
  );

  const SynCoin = await ethers.getContractFactory("SynCoin");
  const token = await SynCoin.deploy(treasuryWallet);
  await token.waitForDeployment();
  const address = await token.getAddress();

  const Minter = await ethers.getContractFactory("SYNTHOSSynBridgeMinter");
  const minter = await Minter.deploy(address, bridgeRelayers, bridgeThreshold);
  await minter.waitForDeployment();
  const minterAddress = await minter.getAddress();

  const tx = await token.initializeBridgeMinter(minterAddress);
  await tx.wait();

  const name = await token.name();
  const symbol = await token.symbol();
  const supply = await token.totalSupply();

  console.log("");
  console.log(`SynCoin deployed:        ${address}`);
  console.log(`SYNTHOSSynBridgeMinter:  ${minterAddress}`);
  console.log(`Name/Symbol:             ${name} (${symbol})`);
  console.log(`Total supply:            ${ethers.formatUnits(supply, 18)} ${symbol} (zero -- nothing mints until a real bridge lock is relayed)`);
  console.log(`Bridge relayers:         ${bridgeRelayers.join(",")}`);
  console.log(`Bridge threshold:        ${bridgeThreshold}`);
  console.log("");
  console.log("SYNTHOSSynBridgeMinter deploys paused. It stays paused, and no SYN can");
  console.log("be minted or burned through it, until you explicitly call unpause() once");
  console.log("the relayer set above is live and verified.");
  console.log("");
  console.log("Next: verify both contracts on the block explorer, confirm the relayer");
  console.log("set, then unpause the minter when ready.");
  console.log(
    network.name === "base"
      ? `Explorer: https://basescan.org/token/${address}`
      : network.name === "baseSepolia"
      ? `Explorer: https://sepolia.basescan.org/token/${address}`
      : ""
  );
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
