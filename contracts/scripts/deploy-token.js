const hre = require("hardhat");
const { ethers, network } = hre;

/**
 * Deploys just the SynCoin ERC-20 token. The full 100B SYN supply mints to
 * the deployer wallet. Intended for Base (mainnet: `base`, testnet:
 * `baseSepolia`). Keep this minimal deploy separate from the full
 * governance/staking stack in deploy-synthos.js.
 */
async function main() {
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);

  console.log("SYNTHOS token deploy");
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

  const SynCoin = await ethers.getContractFactory("SynCoin");
  const token = await SynCoin.deploy();
  await token.waitForDeployment();
  const address = await token.getAddress();

  const name = await token.name();
  const symbol = await token.symbol();
  const supply = await token.totalSupply();
  const ownerBal = await token.balanceOf(deployer.address);

  console.log("");
  console.log(`SynCoin deployed: ${address}`);
  console.log(`Name/Symbol:      ${name} (${symbol})`);
  console.log(`Total supply:     ${ethers.formatUnits(supply, 18)} ${symbol}`);
  console.log(`Deployer holds:   ${ethers.formatUnits(ownerBal, 18)} ${symbol}`);
  console.log("");
  console.log("Next: verify on the block explorer and set up liquidity/sale.");
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
