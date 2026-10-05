/**
 * NIDAW Token Deployment Script
 * Deploys the NIDAW ERC-20 token contract to the specified network
 * 
 * Usage:
 *   npx hardhat run scripts/deploy.js --network localhost
 *   npx hardhat run scripts/deploy.js --network goerli
 *   npx hardhat run scripts/deploy.js --network mainnet
 */

const hre = require("hardhat");
const { ethers } = require("hardhat");

// ============================================================================
// CONFIGURATION
// ============================================================================

const TOKEN_NAME = "NIDAW Token";
const TOKEN_SYMBOL = "NIDAW";
const INITIAL_SUPPLY = ethers.utils.parseUnits("100000000", 18); // 100M tokens
const DECIMALS = 18;

// ============================================================================
// DEPLOYMENT FUNCTION
// ============================================================================

async function main() {
  console.log("🚀 Starting NIDAW Token deployment...");
  console.log("📡 Network:", hre.network.name);
  console.log("👤 Deployer:", (await ethers.getSigners())[0].address);
  console.log("");

  // Get deployer account
  const [deployer] = await ethers.getSigners();
  
  console.log("💰 Deployer balance:", ethers.utils.formatEther(await deployer.getBalance()), "ETH");
  console.log("");

  // Check balance
  const balance = await deployer.getBalance();
  if (balance.eq(0)) {
    throw new Error("❌ Deployer has no ETH. Please fund the account.");
  }

  // Deploy contract
  console.log("📦 Deploying NIDAWToken contract...");
  console.log("   Name:", TOKEN_NAME);
  console.log("   Symbol:", TOKEN_SYMBOL);
  console.log("   Initial Supply:", ethers.utils.formatUnits(INITIAL_SUPPLY, DECIMALS), "NIDAW");
  console.log("");

  const NIDAWToken = await ethers.getContractFactory("NIDAWToken");
  const token = await NIDAWToken.deploy(
    TOKEN_NAME,
    TOKEN_SYMBOL,
    INITIAL_SUPPLY
  );

  console.log("⏳ Waiting for deployment confirmation...");
  await token.deployed();

  console.log("");
  console.log("✅ NIDAWToken deployed successfully!");
  console.log("📍 Contract Address:", token.address);
  console.log("🔗 Transaction Hash:", token.deployTransaction.hash);
  console.log("⛽ Gas Used:", token.deployTransaction.gasLimit.toString());
  console.log("");

  // Wait for additional confirmations (for production networks)
  if (hre.network.name !== "hardhat" && hre.network.name !== "localhost") {
    console.log("⏳ Waiting for 5 confirmations...");
    await token.deployTransaction.wait(5);
    console.log("✅ Contract confirmed!");
    console.log("");
  }

  // Verify contract on Etherscan (if not local network)
  if (hre.network.name !== "hardhat" && hre.network.name !== "localhost") {
    console.log("🔍 Verifying contract on Etherscan...");
    try {
      await hre.run("verify:verify", {
        address: token.address,
        constructorArguments: [
          TOKEN_NAME,
          TOKEN_SYMBOL,
          INITIAL_SUPPLY
        ],
      });
      console.log("✅ Contract verified!");
    } catch (error) {
      console.log("⚠️  Verification failed:", error.message);
      console.log("   You can verify manually with:");
      console.log(`   npx hardhat verify --network ${hre.network.name} ${token.address} "${TOKEN_NAME}" "${TOKEN_SYMBOL}" "${INITIAL_SUPPLY}"`);
    }
    console.log("");
  }

  // Log deployment summary
  console.log("═══════════════════════════════════════════════════════════");
  console.log("📊 DEPLOYMENT SUMMARY");
  console.log("═══════════════════════════════════════════════════════════");
  console.log("Network:", hre.network.name);
  console.log("Contract:", token.address);
  console.log("Deployer:", deployer.address);
  console.log("Token Name:", TOKEN_NAME);
  console.log("Token Symbol:", TOKEN_SYMBOL);
  console.log("Total Supply:", ethers.utils.formatUnits(INITIAL_SUPPLY, DECIMALS), "NIDAW");
  console.log("Transaction:", token.deployTransaction.hash);
  console.log("═══════════════════════════════════════════════════════════");
  console.log("");

  // Save deployment info to file
  const fs = require("fs");
  const deploymentInfo = {
    network: hre.network.name,
    contractAddress: token.address,
    deployer: deployer.address,
    transactionHash: token.deployTransaction.hash,
    blockNumber: token.deployTransaction.blockNumber,
    gasUsed: token.deployTransaction.gasLimit.toString(),
    timestamp: new Date().toISOString(),
    tokenName: TOKEN_NAME,
    tokenSymbol: TOKEN_SYMBOL,
    totalSupply: INITIAL_SUPPLY.toString(),
  };

  const deploymentPath = `deployments/${hre.network.name}.json`;
  fs.mkdirSync("deployments", { recursive: true });
  fs.writeFileSync(deploymentPath, JSON.stringify(deploymentInfo, null, 2));
  console.log(`💾 Deployment info saved to ${deploymentPath}`);

  return token;
}

// ============================================================================
// ERROR HANDLING
// ============================================================================

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("");
    console.error("❌ Deployment failed!");
    console.error("");
    console.error(error);
    process.exit(1);
  });