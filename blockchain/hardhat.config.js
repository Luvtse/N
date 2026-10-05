/**
 * NIDAW Hardhat Configuration
 * Production-ready configuration for smart contract development
 */

require("@nomicfoundation/hardhat-toolbox");
require("@nomicfoundation/hardhat-verify");
require("hardhat-gas-reporter");
require("solidity-coverage");
require("dotenv").config();

// ============================================================================
// ENVIRONMENT VARIABLES
// ============================================================================

const PRIVATE_KEY = process.env.PRIVATE_KEY || "0x0000000000000000000000000000000000000000000000000000000000000000";
const ETHERSCAN_API_KEY = process.env.ETHERSCAN_API_KEY || "";
const POLYGONSCAN_API_KEY = process.env.POLYGONSCAN_API_KEY || "";
const ARBISCAN_API_KEY = process.env.ARBISCAN_API_KEY || "";
const INFURA_API_KEY = process.env.INFURA_API_KEY || "";
const ALCHEMY_API_KEY = process.env.ALCHEMY_API_KEY || "";
const COINMARKETCAP_API_KEY = process.env.COINMARKETCAP_API_KEY || "";

// ============================================================================
// NETWORK CONFIGURATION
// ============================================================================

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  // ==========================================================================
  // SOLIDITY COMPILER
  // ==========================================================================
  solidity: {
    version: "0.8.20",
    settings: {
      optimizer: {
        enabled: true,
        runs: 200,
      },
      viaIR: true,
    },
  },

  // ==========================================================================
  // NETWORKS
  // ==========================================================================
  networks: {
    // Local development
    hardhat: {
      chainId: 31337,
      mining: {
        auto: true,
        interval: 0,
      },
    },

    localhost: {
      url: "http://127.0.0.1:8545",
      chainId: 31337,
    },

    // Ethereum Testnets
    goerli: {
      url: `https://eth-goerli.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 5,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
    },

    sepolia: {
      url: `https://eth-sepolia.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 11155111,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
    },

    // Polygon Testnets
    mumbai: {
      url: `https://polygon-mumbai.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 80001,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
    },

    // Arbitrum Testnets
    arbitrumGoerli: {
      url: `https://arb-goerli.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 421613,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
    },

    // Mainnets
    mainnet: {
      url: `https://eth-mainnet.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 1,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
      gasMultiplier: 1.2,
    },

    polygon: {
      url: `https://polygon-mainnet.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 137,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
      gasMultiplier: 1.2,
    },

    arbitrum: {
      url: `https://arb-mainnet.g.alchemy.com/v2/${ALCHEMY_API_KEY}`,
      chainId: 42161,
      accounts: [PRIVATE_KEY],
      gasPrice: "auto",
      gasMultiplier: 1.2,
    },
  },

  // ==========================================================================
  // ETHERSCAN VERIFICATION
  // ==========================================================================
  etherscan: {
    apiKey: {
      mainnet: ETHERSCAN_API_KEY,
      goerli: ETHERSCAN_API_KEY,
      sepolia: ETHERSCAN_API_KEY,
      polygon: POLYGONSCAN_API_KEY,
      polygonMumbai: POLYGONSCAN_API_KEY,
      arbitrumOne: ARBISCAN_API_KEY,
      arbitrumGoerli: ARBISCAN_API_KEY,
    },
  },

  // ==========================================================================
  // GAS REPORTER
  // ==========================================================================
  gasReporter: {
    enabled: process.env.REPORT_GAS === "true",
    currency: "USD",
    outputFile: "gas-report.txt",
    noColors: true,
    coinmarketcap: COINMARKETCAP_API_KEY,
    excludeContracts: ["MockToken"],
  },

  // ==========================================================================
  // MOCHA TEST CONFIGURATION
  // ==========================================================================
  mocha: {
    timeout: 100000,
  },

  // ==========================================================================
  // PATHS
  // ==========================================================================
  paths: {
    sources: "./contracts",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },

  // ==========================================================================
  // TYPECHAIN
  // ==========================================================================
  typechain: {
    outDir: "typechain-types",
    target: "ethers-v6",
  },

  // ==========================================================================
  // COVERAGE
  // ==========================================================================
  coverage: {
    skipFiles: ["mocks/", "interfaces/"],
  },
};