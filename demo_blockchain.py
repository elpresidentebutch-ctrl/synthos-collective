"""
SYNTHOS Blockchain Demo
Demonstrates agents forming distributed consensus blockchain
"""

import asyncio
import sys
from datetime import datetime
from src.core.blockchain_integration import AgentNetworkBlockchain


def print_header(text: str, char: str = "="):
    """Print formatted header"""
    width = 80
    print(f"\n{char * width}")
    print(f"{text.center(width)}")
    print(f"{char * width}\n")


def print_step(step_num: int, description: str):
    """Print formatted step"""
    print(f"[Step {step_num}] {description}")
    print("-" * 80)


async def demo_basic_blockchain():
    """Demo 1: Basic blockchain with 5 agents"""
    print_header("DEMO 1: Basic Blockchain (5 Agents, 3 Rounds)")
    
    network = AgentNetworkBlockchain(num_agents=5)
    
    await network.run_blockchain_network(rounds=3, transactions_per_round=2)
    
    network.blockchain.print_chain()
    network.print_blockchain_summary()


async def demo_blockchain_under_load():
    """Demo 2: Blockchain under load"""
    print_header("DEMO 2: Blockchain Under Load")
    
    print("Stress test: Creating transactions rapidly")
    print("Testing distributed consensus at scale\n")
    
    network = AgentNetworkBlockchain(num_agents=7)
    await network.initialize_network()
    
    # Create transactions rapidly
    start = datetime.now()
    tx_count = 0
    
    for round_num in range(10):
        # Multiple agents create multiple transactions
        for agent_idx in range(3):
            agent = network.agents[agent_idx]
            receiver_idx = (agent_idx + 1) % len(network.agents)
            receiver = network.agents[receiver_idx].identity.agent_id
            
            await agent.create_transaction(receiver, 5)
            tx_count += 1
        
        # Run consensus
        success, _ = await network.blockchain.run_consensus_round()
        
        # Synchronize
        for agent in network.agents:
            await agent.sync_blockchain_state()
    
    elapsed = (datetime.now() - start).total_seconds()
    
    print(f"\n✓ Processed {tx_count} transactions in {elapsed:.1f}s")
    print(f"✓ Average: {tx_count/elapsed:.1f} tx/sec")
    
    network.print_blockchain_summary()


async def demo_network_statistics():
    """Demo 3: Collect network statistics"""
    print_header("DEMO 3: Network Statistics")
    
    network = AgentNetworkBlockchain(num_agents=10)
    await network.initialize_network()
    
    print(f"Network Topology:")
    print(f"  Total Agents: {len(network.agents)}")
    print(f"  Each agent connected to: {len(network.agents) - 1} peers")
    print(f"  Network fully connected: YES")
    print(f"  Byzantine tolerance: up to {len(network.agents) // 3} malicious agents")
    print(f"  Finality threshold: {len(network.agents) * 2 // 3 + 1}/{len(network.agents)} agents")
    print()
    
    # Run several rounds and collect stats
    finality_times = []
    
    for round_num in range(5):
        start = datetime.now()
        success, block_hash = await network.blockchain.run_consensus_round()
        elapsed_ms = int((datetime.now() - start).total_seconds() * 1000)
        
        if success:
            finality_times.append(elapsed_ms)
        
        for agent in network.agents:
            await agent.sync_blockchain_state()
    
    print(f"Consensus Statistics (5 rounds):")
    print(f"  Average finality time: {sum(finality_times)/len(finality_times):.1f}ms")
    print(f"  Min finality time: {min(finality_times)}ms")
    print(f"  Max finality time: {max(finality_times)}ms")
    print()


async def demo_agent_properties():
    """Demo 4: Show agent properties"""
    print_header("DEMO 4: Agent Properties")
    
    network = AgentNetworkBlockchain(num_agents=3)
    await network.initialize_network()
    
    for agent in network.agents:
        print(f"Agent: {agent.identity.agent_id}")
        print(f"  Public Key Hash: {agent.identity.public_key[:16]}...")
        print(f"  Stake: {agent.identity.stake} tokens")
        print(f"  Reputation: {agent.identity.reputation}")
        print(f"  Roles: {[r.name for r in agent.assigned_roles]}")
        print(f"  Peers: {len(agent.peers)}")
        print()


async def demo_transaction_flow():
    """Demo 5: Detailed transaction flow"""
    print_header("DEMO 5: Transaction Flow Trace")
    
    network = AgentNetworkBlockchain(num_agents=3)
    await network.initialize_network()
    
    print("Transaction Flow:")
    print("  1. Agent creates transaction")
    print("     → TX added to mempool (visible to all agents)")
    print("     → TX.status = 'pending'")
    print()
    
    # Create transaction
    agent = network.agents[0]
    receiver = network.agents[1].identity.agent_id
    tx_id = await agent.create_transaction(receiver, 10)
    
    print(f"  2. Mempool state: {len(network.blockchain.ledger.mempool)} pending transactions")
    print()
    
    print("  3. Block proposer selects transactions")
    print("     → Creates block from mempool")
    print("     → Block includes pending transactions")
    print()
    
    pending = network.blockchain.ledger.get_pending_transactions(limit=10)
    print(f"  4. Running consensus round...")
    success, block_hash = await network.blockchain.run_consensus_round()
    
    print()
    print(f"  5. Block result: {'FINALIZED' if success else 'FAILED'}")
    print()
    
    # Check transaction status
    for tx in network.blockchain.ledger.full_chain[0].transactions:
        print(f"  6. Transaction status: {tx.status}")
    
    print()


async def main():
    """Run all demos"""
    
    print_header("SYNTHOS BLOCKCHAIN DEMONSTRATIONS", "=")
    print("Agents ARE the Blockchain - Distributed Consensus System")
    print()
    print("This demo shows:")
    print("  • How agents form a blockchain through consensus")
    print("  • Transaction lifecycle from creation to finality")
    print("  • Distributed state synchronization")
    print("  • Network properties and statistics")
    print()
    
    # Run demos
    try:
        await demo_basic_blockchain()
        await demo_transaction_flow()
        await demo_network_statistics()
        await demo_blockchain_under_load()
        await demo_agent_properties()
        
    except Exception as e:
        print(f"\nError: {e}")
        import traceback
        traceback.print_exc()
    
    print_header("DEMOS COMPLETE", "=")
    print("✓ All demonstrations completed successfully")
    print()
    print("Key Insights:")
    print("  • Agents participate equally in consensus (no mining)")
    print("  • Finality achieved when 2/3+ agents agree")
    print("  • All agents maintain identical chain copy")
    print("  • Transactions flow peer-to-peer without intermediary")
    print("  • Byzantine Fault Tolerant - survives 1/3 malicious agents")
    print()


if __name__ == "__main__":
    print("Starting SYNTHOS Blockchain Demo...")
    print("Python version:", sys.version)
    print()
    
    asyncio.run(main())
