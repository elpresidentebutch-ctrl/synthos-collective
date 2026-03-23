"""
Agent → Blockchain Integration
Agents use the distributed ledger as their collective state machine
"""

from src.core.agent_blueprint import SynthosAgentInstance
from src.core.blockchain import SynthosBlockchain, Transaction
from datetime import datetime
import asyncio
from typing import List, Dict, Optional


class BlockchainAwareAgent(SynthosAgentInstance):
    """
    Agent that is aware of and participates in the blockchain
    Extends SynthosAgentInstance with blockchain coordination
    """
    
    def __init__(self, agent_id=None, initial_stake=0, blockchain=None):
        """Initialize blockchain-aware agent"""
        super().__init__(agent_id, initial_stake)
        
        self.blockchain = blockchain
        self.last_chain_height = 0
        self.last_synced_block = None


    async def create_transaction(self, receiver: str, amount: int) -> str:
        """Create transaction to be added to blockchain mempool"""
        
        nonce = self.local_transactions.get(self.identity.agent_id, 0)
        
        tx = Transaction(
            tx_id=f"tx_{self.identity.agent_id}_{nonce}_{int(datetime.now().timestamp() * 1000)}",
            sender=self.identity.agent_id,
            receiver=receiver,
            amount=amount,
            timestamp=datetime.now(),
            nonce=nonce,
            signature=f"0x{self.identity.agent_id}_sig_{nonce}"
        )
        
        # Add to local blockchain
        success, tx_id = self.blockchain.ledger.add_transaction(tx)
        
        if success:
            self.local_transactions[self.identity.agent_id] = nonce + 1
            print(f"[{self.identity.agent_id}] Created transaction {tx_id}")
            return tx_id
        else:
            print(f"[{self.identity.agent_id}] Failed to create transaction")
            return None


    async def submit_transaction(self, tx: Transaction) -> bool:
        """Submit transaction from another agent"""
        success, _ = self.blockchain.ledger.add_transaction(tx)
        return success


    async def participate_in_consensus(self) -> Optional[str]:
        """Participate in blockchain consensus round"""
        
        # Validators wait for consensus round signal
        # When consensus happens, all agents validate block
        
        return await super().participate_in_consensus()


    async def sync_blockchain_state(self) -> Dict:
        """Synchronize with current blockchain state"""
        
        chain_state = self.blockchain.ledger.get_chain_state()
        
        # Update local view
        self.last_chain_height = chain_state.chain_height
        self.state_root = chain_state.state_root
        
        sync_info = {
            "agent_id": self.identity.agent_id,
            "before_height": self.last_chain_height,
            "after_height": chain_state.chain_height,
            "blocks_synced": chain_state.chain_height - self.last_chain_height,
            "pending_transactions": len(chain_state.pending_transactions),
        }
        
        return sync_info


    async def validate_blockchain_block(self, block: Dict) -> Dict:
        """Validate block from blockchain"""
        
        # Use existing validation logic
        validation = await self.validate_block(block)
        
        return {
            "is_valid": validation.is_valid,
            "validator_id": self.identity.agent_id,
            "block_hash": block.get("block_hash"),
            "checks": validation.checks_passed,
        }


class AgentNetworkBlockchain:
    """
    Agent Network that IS the Blockchain
    Multiple agents form distributed consensus ledger
    """
    
    def __init__(self, num_agents: int = 5):
        """Initialize agent network"""
        self.num_agents = num_agents
        self.agents: List[BlockchainAwareAgent] = []
        self.blockchain = SynthosBlockchain()
        
        # Create agents
        for i in range(num_agents):
            agent = BlockchainAwareAgent(
                agent_id=f"agent_{i:02d}",
                initial_stake=1000,
                blockchain=self.blockchain
            )
            self.agents.append(agent)
        
        # Register agents with blockchain
        self.blockchain.agents = self.agents


    async def initialize_network(self):
        """Initialize agent network"""
        print(f"Initializing agent network with {self.num_agents} agents...")
        
        for agent in self.agents:
            # Each agent joins network
            await agent.join_network([])
            
            # Add peer connections to other agents
            for peer_agent in self.agents:
                if peer_agent.identity.agent_id != agent.identity.agent_id:
                    agent.peers[peer_agent.identity.agent_id] = {
                        "endpoint": f"agent://{peer_agent.identity.agent_id}",
                        "latency": 1,
                        "is_active": True
                    }
        
        print(f"✓ Network initialized - {self.num_agents} agents connected\n")


    async def run_blockchain_network(self, rounds: int = 5, transactions_per_round: int = 3):
        """
        Run complete agent network as blockchain
        Agents coordinated through distributed consensus
        """
        
        print(f"\n{'='*80}")
        print(f"SYNTHOS AGENT NETWORK BLOCKCHAIN")
        print(f"Agents ARE the blockchain")
        print(f"{self.num_agents} agents forming distributed consensus")
        print(f"{'='*80}\n")
        
        # Initialize network
        await self.initialize_network()
        
        # Run consensus rounds
        for round_num in range(rounds):
            print(f"\n{'='*80}")
            print(f"ROUND {round_num + 1}/{rounds}")
            print(f"{'='*80}\n")
            
            # Step 1: Agents create transactions
            print(f"[Step 1] Agents creating transactions...")
            for i, agent in enumerate(self.agents):
                for j in range(transactions_per_round):
                    receiver = self.agents[(i + 1) % len(self.agents)].identity.agent_id
                    await agent.create_transaction(receiver, 10)
            
            print()
            
            # Step 2: Run consensus
            print(f"[Step 2] Running consensus round...")
            success, block_hash = await self.blockchain.run_consensus_round()
            
            print()
            
            # Step 3: Agents synchronize
            print(f"[Step 3] Agents synchronizing blockchain state...")
            for agent in self.agents:
                sync_info = await agent.sync_blockchain_state()
                print(f"  {agent.identity.agent_id}: {sync_info['after_height']} blocks")
            
            print()


    async def run_stress_test(self, duration_seconds: int = 10, agents_per_round: int = 2):
        """Stress test: multiple agents creating transactions"""
        
        print(f"\n{'='*80}")
        print(f"STRESS TEST - Blockchain under load")
        print(f"Duration: {duration_seconds}s, Transactions per agent per round: {agents_per_round}")
        print(f"{'='*80}\n")
        
        await self.initialize_network()
        
        start_time = datetime.now()
        round_num = 0
        
        while (datetime.now() - start_time).total_seconds() < duration_seconds:
            round_num += 1
            
            # Create transactions from random agents
            for i in range(min(agents_per_round, len(self.agents))):
                agent = self.agents[i % len(self.agents)]
                receiver = self.agents[(i + 1) % len(self.agents)].identity.agent_id
                await agent.create_transaction(receiver, 1)
            
            # Run consensus
            success, _ = await self.blockchain.run_consensus_round()
            
            # Sync
            for agent in self.agents:
                await agent.sync_blockchain_state()
            
            await asyncio.sleep(0.1)
        
        return self.blockchain.get_blockchain_stats()


    def print_blockchain_summary(self):
        """Print blockchain summary"""
        
        stats = self.blockchain.get_blockchain_stats()
        
        print(f"\n{'='*80}")
        print(f"BLOCKCHAIN SUMMARY")
        print(f"{'='*80}\n")
        
        print(f"Consensus Rounds: {stats['consensus_rounds']}")
        print(f"Blocks Created: {stats['blocks_created']}")
        print(f"Chain Height: {stats['chain_height']}")
        print(f"Total Transactions: {stats['total_transactions_confirmed']}")
        print(f"Avg Finality Time: {stats['avg_finality_time_ms']:.1f}ms")
        print(f"Agents in Network: {stats['agents_in_network']}")
        print(f"Agents Synchronized: {stats['agents_synchronized']}")
        print(f"Chain Valid: {stats['is_chain_valid']}")
        print()


def main():
    """Demo: Agents ARE the blockchain"""
    
    print("\n" + "="*80)
    print("SYNTHOS BLOCKCHAIN - Agents ARE the Blockchain")
    print("Demonstration of distributed consensus")
    print("="*80 + "\n")
    
    # Create agent network
    network = AgentNetworkBlockchain(num_agents=5)
    
    # Run blockchain for a few rounds
    asyncio.run(network.run_blockchain_network(rounds=3, transactions_per_round=2))
    
    # Print blockchain
    network.blockchain.print_chain()
    network.print_blockchain_summary()


if __name__ == "__main__":
    main()
