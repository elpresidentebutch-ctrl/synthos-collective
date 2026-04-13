// hardhat.config.js

require("@nomiclabs/hardhat-ethers");
require("@nomiclabs/hardhat-waffle");
require("hardhat-gas-reporter");
require("solidity-coverage");
require("@openzeppelin/hardhat-upgrades");
require("dotenv").config();

module.exports = {
  solidity: {
    version: "0.8.20",
    settings: {
      optimizer: {
        enabled: true,
        runs: 200,
      },
    },
  },

  networks: {
    // SYNTHOS Network
    synthos: {
      url: process.env.SYNTHOS_RPC_URL || "http://localhost:8545",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 1234,
      gasPrice: "auto",
    },

    // Gemini Megachain 2.0
    gemini: {
      url: process.env.GEMINI_RPC_URL || "http://localhost:8546",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 2048,
      gasPrice: "auto",
    },

    // Ethereum (for testing)
    ethereum: {
      url: process.env.ETHEREUM_RPC_URL || "http://localhost:8547",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 1,
    },

    // Polygon (for testing)
    polygon: {
      url: process.env.POLYGON_RPC_URL || "http://localhost:8548",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 137,
    },

    // Local hardhat
    hardhat: {
      chainId: 31337,
      allowUnlimitedContractSize: true,
    },

    // Localhost for local development
    localhost: {
      url: "http://127.0.0.1:8545",
    },
  },

  gasReporter: {
    enabled: process.env.REPORT_GAS === "true",
    currency: "USD",
    coinmarketcap: process.env.COINMARKETCAP_API_KEY,
  },

  paths: {
    sources: "./contracts",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },

  mocha: {
    timeout: 40000,
  },
};
