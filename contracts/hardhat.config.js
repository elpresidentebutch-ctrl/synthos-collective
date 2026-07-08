require("@nomicfoundation/hardhat-toolbox");
require("dotenv").config();

const envChainId = (name, fallback) => Number(process.env[name] || fallback);

module.exports = {
  solidity: {
    version: "0.8.20",
    settings: {
      viaIR: true,
      optimizer: {
        enabled: true,
        runs: 200,
      },
    },
  },

  networks: {
    synthos: {
      url: process.env.SYNTHOS_RPC_URL || "http://localhost:8545",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: envChainId("SYNTHOS_CHAIN_ID", 1234),
      gasPrice: "auto",
    },

    gemini: {
      url: process.env.GEMINI_RPC_URL || "http://localhost:8546",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: envChainId("GEMINI_CHAIN_ID", 2048),
      gasPrice: "auto",
    },

    ethereum: {
      url: process.env.ETHEREUM_RPC_URL || "http://localhost:8547",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 1,
    },

    polygon: {
      url: process.env.POLYGON_RPC_URL || "http://localhost:8548",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 137,
    },

    base: {
      url: process.env.BASE_RPC_URL || "https://mainnet.base.org",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 8453,
      gasPrice: "auto",
    },

    baseSepolia: {
      url: process.env.BASE_SEPOLIA_RPC_URL || "https://sepolia.base.org",
      accounts: process.env.PRIVATE_KEY ? [process.env.PRIVATE_KEY] : [],
      chainId: 84532,
      gasPrice: "auto",
    },

    hardhat: {
      chainId: 31337,
      allowUnlimitedContractSize: true,
    },

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
    sources: "./src",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },

  mocha: {
    timeout: 40000,
  },
};
