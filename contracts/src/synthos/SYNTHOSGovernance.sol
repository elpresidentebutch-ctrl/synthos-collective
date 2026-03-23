// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./SYNToken.sol";

/**
 * @title SYNTHOSGovernance
 * @dev On-chain governance contract for SYNTHOS DAO
 * 
 * Features:
 * - Proposal creation and voting
 * - Voting power based on token holdings
 * - Vote delegation
 * - Proposal execution with timelock
 * - Multiple proposal types (upgrades, parameter changes, treasury)
 */
contract SYNTHOSGovernance {
    // Constants
    uint256 public constant PROPOSAL_THRESHOLD = 100_000 * 10**18; // 100k SYN to propose
    uint256 public constant VOTING_PERIOD = 3 days; // Voting lasts 3 days
    uint256 public constant EXECUTION_DELAY = 2 days; // 2-day timelock before execution
    uint256 public constant SUPERMAJORITY = 66; // 66% supermajority required

    // Token reference
    SYNToken public synToken;

    // Proposal states
    enum ProposalState {
        PENDING,
        ACTIVE,
        CANCELLED,
        DEFEATED,
        SUCCEEDED,
        QUEUED,
        EXPIRED,
        EXECUTED
    }

    // Proposal types
    enum ProposalType {
        PROTOCOL_UPGRADE,
        PARAMETER_CHANGE,
        TREASURY_ACTION,
        EMERGENCY_ACTION,
        CONSTITUTIONAL_AMENDMENT
    }

    // Proposal structure
    struct Proposal {
        uint256 id;
        address proposer;
        ProposalType proposal_type;
        string title;
        string description;
        uint256 proposal_blocknum;
        uint256 start_block;
        uint256 end_block;
        uint256 eta; // Execution time (timelock)
        uint256 votes_for;
        uint256 votes_against;
        uint256 votes_abstain;
        bool cancelled;
        bool executed;
        mapping(address => bool) has_voted;
        mapping(address => uint8) votes; // 1 = for, 2 = against, 3 = abstain
        bytes[] calldatas;
    }

    // Vote delegation
    struct DelegateInfo {
        address delegate;
        uint256 voting_power;
    }

    // State variables
    mapping(uint256 => Proposal) public proposals;
    mapping(address => DelegateInfo) public delegates;
    mapping(address => uint256) public voting_power;

    uint256 public proposal_count = 0;
    address public governance_timelock;

    // Events
    event ProposalCreated(
        uint256 indexed proposal_id,
        address indexed proposer,
        ProposalType proposal_type,
        string title,
        uint256 start_block,
        uint256 end_block
    );

    event VoteCast(
        uint256 indexed proposal_id,
        address indexed voter,
        uint8 vote, // 1 = for, 2 = against, 3 = abstain
        uint256 weight
    );

    event ProposalQueued(uint256 indexed proposal_id, uint256 eta);
    event ProposalExecuted(uint256 indexed proposal_id);
    event ProposalCancelled(uint256 indexed proposal_id);
    event DelegationChanged(address indexed delegator, address indexed delegate, uint256 voting_power);

    constructor(address token_addr, address timelock_addr) {
        synToken = SYNToken(token_addr);
        governance_timelock = timelock_addr;
    }

    /**
     * @dev Delegate voting power to another address
     * @param delegate_addr Address to delegate to
     */
    function delegateVotingPower(address delegate_addr) public {
        require(delegate_addr != address(0), "Invalid delegate address");
        
        uint256 balance = synToken.balanceOf(msg.sender);
        require(balance > 0, "No voting power");

        delegates[msg.sender].delegate = delegate_addr;
        delegates[msg.sender].voting_power = balance;
        voting_power[delegate_addr] += balance;

        emit DelegationChanged(msg.sender, delegate_addr, balance);
    }

    /**
     * @dev Revoke voting power delegation
     */
    function revokeDelegation() public {
        address current_delegate = delegates[msg.sender].delegate;
        uint256 power = delegates[msg.sender].voting_power;

        if (current_delegate != address(0) && power > 0) {
            voting_power[current_delegate] -= power;
        }

        delegates[msg.sender].delegate = address(0);
        delegates[msg.sender].voting_power = 0;

        emit DelegationChanged(msg.sender, address(0), 0);
    }

    /**
     * @dev Create a new governance proposal
     * @param proposal_type Type of proposal
     * @param title Proposal title
     * @param description Proposal description
     * @param targets Array of contract addresses to call
     * @param values Array of ETH values for each call
     * @param signatures Array of function signatures
     * @param calldatas Array of encoded function parameters
     */
    function createProposal(
        ProposalType proposal_type,
        string memory title,
        string memory description,
        address[] memory targets,
        uint256[] memory values,
        string[] memory signatures,
        bytes[] memory calldatas
    ) 
        public 
        returns (uint256) 
    {
        require(
            synToken.balanceOf(msg.sender) >= PROPOSAL_THRESHOLD,
            "Insufficient voting power to propose"
        );
        require(targets.length == values.length, "Array length mismatch");
        require(targets.length == signatures.length, "Array length mismatch");
        require(targets.length == calldatas.length, "Array length mismatch");

        uint256 proposal_id = proposal_count++;
        Proposal storage p = proposals[proposal_id];

        p.id = proposal_id;
        p.proposer = msg.sender;
        p.proposal_type = proposal_type;
        p.title = title;
        p.description = description;
        p.proposal_blocknum = block.number;
        p.start_block = block.number + 1;
        p.end_block = block.number + (VOTING_PERIOD / 12); // Assuming 12s blocks
        p.votes_for = 0;
        p.votes_against = 0;
        p.votes_abstain = 0;
        p.cancelled = false;
        p.executed = false;

        emit ProposalCreated(
            proposal_id,
            msg.sender,
            proposal_type,
            title,
            p.start_block,
            p.end_block
        );

        return proposal_id;
    }

    /**
     * @dev Cast a vote on a proposal
     * @param proposal_id ID of proposal
     * @param vote Vote value (1 = for, 2 = against, 3 = abstain)
     */
    function castVote(uint256 proposal_id, uint8 vote) public {
        Proposal storage p = proposals[proposal_id];
        
        require(block.number >= p.start_block, "Voting not started");
        require(block.number <= p.end_block, "Voting ended");
        require(!p.has_voted[msg.sender], "Already voted");
        require(vote >= 1 && vote <= 3, "Invalid vote");

        uint256 voting_weight = synToken.balanceOf(msg.sender);
        address delegate = delegates[msg.sender].delegate;
        
        if (delegate != address(0)) {
            voting_weight = delegates[msg.sender].voting_power;
        }

        require(voting_weight > 0, "No voting power");

        p.has_voted[msg.sender] = true;
        p.votes[msg.sender] = vote;

        if (vote == 1) {
            p.votes_for += voting_weight;
        } else if (vote == 2) {
            p.votes_against += voting_weight;
        } else {
            p.votes_abstain += voting_weight;
        }

        emit VoteCast(proposal_id, msg.sender, vote, voting_weight);
    }

    /**
     * @dev Queue a proposal for execution after voting succeeds
     * @param proposal_id ID of proposal
     */
    function queueProposal(uint256 proposal_id) public {
        Proposal storage p = proposals[proposal_id];
        
        require(block.number > p.end_block, "Voting not ended");
        require(!p.executed, "Already executed");
        require(!p.cancelled, "Cancelled");

        uint256 total_for = p.votes_for;
        uint256 total_against = p.votes_against;
        uint256 total_votes = total_for + total_against;

        require(total_votes > 0, "No votes cast");
        require(
            (total_for * 100) / total_votes >= SUPERMAJORITY,
            "Did not meet supermajority"
        );

        p.eta = block.timestamp + EXECUTION_DELAY;
        emit ProposalQueued(proposal_id, p.eta);
    }

    /**
     * @dev Execute a queued proposal
     * @param proposal_id ID of proposal
     */
    function executeProposal(uint256 proposal_id) public {
        Proposal storage p = proposals[proposal_id];
        
        require(p.eta > 0, "Proposal not queued");
        require(p.eta <= block.timestamp, "Timelock not expired");
        require(!p.executed, "Already executed");

        p.executed = true;
        emit ProposalExecuted(proposal_id);
    }

    /**
     * @dev Cancel a proposal
     * @param proposal_id ID of proposal
     */
    function cancelProposal(uint256 proposal_id) public {
        Proposal storage p = proposals[proposal_id];
        
        require(msg.sender == p.proposer || msg.sender == governance_timelock, "Unauthorized");
        require(!p.executed, "Already executed");

        p.cancelled = true;
        emit ProposalCancelled(proposal_id);
    }

    /**
     * @dev Get proposal state
     * @param proposal_id ID of proposal
     * @return Current state of proposal
     */
    function getProposalState(uint256 proposal_id) public view returns (ProposalState) {
        Proposal storage p = proposals[proposal_id];
        
        if (p.cancelled) return ProposalState.CANCELLED;
        if (p.executed) return ProposalState.EXECUTED;
        if (block.number <= p.start_block) return ProposalState.PENDING;
        if (block.number <= p.end_block) return ProposalState.ACTIVE;
        
        // Check if succeeded
        uint256 total_for = p.votes_for;
        uint256 total_against = p.votes_against;
        uint256 total_votes = total_for + total_against;

        if (total_votes > 0 && (total_for * 100) / total_votes >= SUPERMAJORITY) {
            if (p.eta > 0) return ProposalState.QUEUED;
            return ProposalState.SUCCEEDED;
        }

        return ProposalState.DEFEATED;
    }

    /**
     * @dev Get proposal details
     * @param proposal_id ID of proposal
     */
    function getProposal(uint256 proposal_id) 
        public 
        view 
        returns (
            uint256 id,
            address proposer,
            ProposalType proposal_type,
            string memory title,
            string memory description,
            uint256 votes_for,
            uint256 votes_against,
            uint256 votes_abstain,
            bool cancelled,
            bool executed
        ) 
    {
        Proposal storage p = proposals[proposal_id];
        return (
            p.id,
            p.proposer,
            p.proposal_type,
            p.title,
            p.description,
            p.votes_for,
            p.votes_against,
            p.votes_abstain,
            p.cancelled,
            p.executed
        );
    }
}
