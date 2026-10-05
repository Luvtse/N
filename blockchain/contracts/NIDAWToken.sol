// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Permit.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/access/AccessControl.sol";

contract NIDAWToken is ERC20, ERC20Burnable, ERC20Permit, Ownable, AccessControl {
    bytes32 public constant MINTER_ROLE = keccak256("MINTER_ROLE");
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");
    
    uint256 public constant MAX_SUPPLY = 1_000_000_000 * 10**18; // 1 billion tokens
    
    // Staking
    struct Stake {
        uint256 amount;
        uint256 startTime;
        uint256 lockPeriod;
        uint256 rewardRate; // APY in basis points
    }
    
    mapping(address => Stake) public stakes;
    mapping(address => uint256) public stakingRewards;
    
    // Loyalty tiers
    enum Tier { Bronze, Silver, Gold, Platinum, Diamond }
    mapping(address => Tier) public userTiers;
    mapping(address => uint256) public loyaltyPoints;
    
    // Events
    event TokensEarned(address indexed user, uint256 amount, string reason);
    event TokensStaked(address indexed user, uint256 amount, uint256 lockPeriod);
    event TokensUnstaked(address indexed user, uint256 amount, uint256 reward);
    event TierUpgraded(address indexed user, Tier newTier);
    
    constructor() ERC20("NIDAW Token", "NIDAW") Ownable(msg.sender) {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(MINTER_ROLE, msg.sender);
        _grantRole(PAUSER_ROLE, msg.sender);
        
        // Mint initial supply to owner
        _mint(msg.sender, 100_000_000 * 10**18);
    }
    
    // Mint tokens (only minters)
    function mint(address to, uint256 amount) external onlyRole(MINTER_ROLE) {
        require(totalSupply() + amount <= MAX_SUPPLY, "Exceeds max supply");
        _mint(to, amount);
    }
    
    // Reward users for activities
    function rewardUser(address user, uint256 amount, string calldata reason) 
        external onlyRole(MINTER_ROLE) 
    {
        require(totalSupply() + amount <= MAX_SUPPLY, "Exceeds max supply");
        _mint(user, amount);
        loyaltyPoints[user] += amount;
        
        // Update tier
        _updateTier(user);
        
        emit TokensEarned(user, amount, reason);
    }
    
    // Stake tokens for rewards
    function stake(uint256 amount, uint256 lockPeriodDays) external {
        require(amount > 0, "Amount must be > 0");
        require(lockPeriodDays >= 30 && lockPeriodDays <= 365, "Invalid lock period");
        require(balanceOf(msg.sender) >= amount, "Insufficient balance");
        
        // Claim existing rewards first
        if (stakes[msg.sender].amount > 0) {
            _claimStakingRewards();
        }
        
        _transfer(msg.sender, address(this), amount);
        
        // Calculate reward rate based on lock period
        uint256 rewardRate = _calculateRewardRate(lockPeriodDays);
        
        stakes[msg.sender] = Stake({
            amount: amount,
            startTime: block.timestamp,
            lockPeriod: lockPeriodDays * 1 days,
            rewardRate: rewardRate
        });
        
        emit TokensStaked(msg.sender, amount, lockPeriodDays);
    }
    
    // Unstake tokens
    function unstake() external {
        Stake memory userStake = stakes[msg.sender];
        require(userStake.amount > 0, "No stake found");
        require(block.timestamp >= userStake.startTime + userStake.lockPeriod, "Still locked");
        
        uint256 reward = _calculateReward(userStake);
        
        // Return staked tokens + rewards
        _transfer(address(this), msg.sender, userStake.amount + reward);
        
        delete stakes[msg.sender];
        
        emit TokensUnstaked(msg.sender, userStake.amount, reward);
    }
    
    // Claim staking rewards without unstaking
    function claimRewards() external {
        _claimStakingRewards();
    }
    
    function _claimStakingRewards() internal {
        Stake memory userStake = stakes[msg.sender];
        require(userStake.amount > 0, "No stake found");
        
        uint256 reward = _calculateReward(userStake);
        require(reward > 0, "No rewards to claim");
        
        // Mint rewards
        _mint(msg.sender, reward);
        
        // Reset stake start time
        stakes[msg.sender].startTime = block.timestamp;
    }
    
    function _calculateReward(Stake memory stake) internal view returns (uint256) {
        uint256 timeStaked = block.timestamp - stake.startTime;
        if (timeStaked > stake.lockPeriod) {
            timeStaked = stake.lockPeriod;
        }
        
        // reward = amount * rate * time / 365 days / 10000 (basis points)
        return (stake.amount * stake.rewardRate * timeStaked) / (365 days * 10000);
    }
    
    function _calculateRewardRate(uint256 lockPeriodDays) internal pure returns (uint256) {
        // APY based on lock period
        if (lockPeriodDays >= 365) return 1500; // 15% APY
        if (lockPeriodDays >= 180) return 1200; // 12% APY
        if (lockPeriodDays >= 90) return 1000;  // 10% APY
        if (lockPeriodDays >= 60) return 800;   // 8% APY
        return 500; // 5% APY
    }
    
    function _updateTier(address user) internal {
        uint256 points = loyaltyPoints[user];
        Tier newTier;
        
        if (points >= 1_000_000 * 10**18) newTier = Tier.Diamond;
        else if (points >= 500_000 * 10**18) newTier = Tier.Platinum;
        else if (points >= 100_000 * 10**18) newTier = Tier.Gold;
        else if (points >= 10_000 * 10**18) newTier = Tier.Silver;
        else newTier = Tier.Bronze;
        
        if (newTier != userTiers[user]) {
            userTiers[user] = newTier;
            emit TierUpgraded(user, newTier);
        }
    }
    
    // Get user info
    function getUserInfo(address user) external view returns (
        uint256 balance,
        uint256 points,
        Tier tier,
        uint256 stakedAmount,
        uint256 pendingRewards
    ) {
        Stake memory userStake = stakes[user];
        return (
            balanceOf(user),
            loyaltyPoints[user],
            userTiers[user],
            userStake.amount,
            userStake.amount > 0 ? _calculateReward(userStake) : 0
        );
    }
}