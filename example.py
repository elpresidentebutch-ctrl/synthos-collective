"""SYNTHOS Agent Framework - Example Usage"""

import asyncio
from src.core import SyntHOSAgent, AgentConfig
from src.roles import (
    ValidatorRole,
    EconomistRole,
    GovernorRole,
    CommunicatorRole,
    SimulatorRole,
    EnforcerRole,
    CitizenRole,
)
from src.models import Transaction, Block, Proposal


async def create_agent(agent_id: str, network: str = "testnet") -> SyntHOSAgent:
    """
    Create and initialize a SYNTHOS Agent
    
    Args:
        agent_id: Unique agent identifier
        network: Network to participate in
        
    Returns:
        Initialized SyntHOSAgent instance
    """
    # Create configuration
    config = AgentConfig(
        id=agent_id,
        network=network,
        log_level="INFO",
        consensus_timeout_ms=4000,
        max_peers=50,
    )
    
    # Create agent
    agent = SyntHOSAgent(config)
    
    # Register all roles
    agent.register_role(ValidatorRole(agent))
    agent.register_role(EconomistRole(agent))
    agent.register_role(GovernorRole(agent))
    agent.register_role(CommunicatorRole(agent))
    agent.register_role(SimulatorRole(agent))
    agent.register_role(EnforcerRole(agent))
    agent.register_role(CitizenRole(agent))
    
    # Initialize agent
    await agent.initialize()
    
    return agent


async def example_transaction_flow(agent: SyntHOSAgent) -> None:
    """
    Example: Submit and validate transaction
    
    Demonstrates the flow:
    1. Citizen submits transaction
    2. Validator validates it
    3. Economist calculates fees
    4. Communicator broadcasts
    """
    # Create transaction
    tx = Transaction(
        sender="alice",
        recipient="bob",
        amount=100,
        fee=1,
        nonce=0,
    )
    
    print(f"\n--- Transaction Flow Example ---")
    print(f"Submitting transaction: {tx.id}")
    
    # Submit via Citizen role
    citizen = agent.get_role("Citizen")
    await citizen.submit_transaction(tx)
    
    # Let events process
    await asyncio.sleep(0.5)
    
    print(f"Transaction submitted and validated")


async def example_governance_flow(agent: SyntHOSAgent) -> None:
    """
    Example: Propose and vote on governance change
    
    Demonstrates:
    1. Governor proposes change
    2. Simulator models impact
    3. Citizens vote
    4. Propose is executed if passed
    """
    # Create proposal
    proposal = Proposal(
        id="proposal-001",
        proposer=agent.id,
        change_type="FEE_ADJUSTMENT",
        parameters={'new_base_fee': 2},
        description="Increase base fee to reduce spam",
        vote_deadline=3600,
    )
    
    print(f"\n--- Governance Flow Example ---")
    print(f"Proposing: {proposal.change_type}")
    
    # Submit proposal via Governor role
    governor = agent.get_role("Governor")
    proposal_id = await governor.propose_change(proposal)
    
    # Simulate impact with Simulator role
    simulator = agent.get_role("Simulator")
    simulation_result = await simulator.simulate_protocol_change(proposal)
    print(f"Simulated impact: {simulation_result['predicted_impact']}")
    
    # Vote as Citizen
    citizen = agent.get_role("Citizen")
    await citizen.participate_in_voting(proposal_id, vote=True)
    print(f"Citizen voted YES on proposal")
    
    # Finalize vote
    passed = await governor.finalize_vote(proposal_id)
    print(f"Proposal result: {'PASSED' if passed else 'REJECTED'}")


async def example_monitoring(agent: SyntHOSAgent) -> None:
    """
    Example: Monitor agent status and metrics
    """
    print(f"\n--- Agent Status Example ---")
    status = agent.get_status()
    
    print(f"Agent: {status['id']}")
    print(f"Network: {status['network']}")
    print(f"Started: {status['started']}")
    print(f"Running: {status['running']}")
    
    print(f"\nRole Status:")
    for role_name, role_data in status['roles'].items():
        print(f"  {role_name}: {role_data['status']}")
    
    print(f"\nState:")
    state = status['state']
    print(f"  Version: {state['version']}")
    print(f"  Ledger keys: {len(state['ledger'])}")


async def example_event_history(agent: SyntHOSAgent) -> None:
    """
    Example: View agent event history
    """
    print(f"\n--- Event History Example ---")
    
    events = await agent.get_event_history(limit=10)
    
    print(f"Recent events ({len(events)} total):")
    for event in events[-5:]:
        print(f"  - {event['type']} (source: {event['source']})")


async def run_example() -> None:
    """
    Run example demonstrations
    """
    print("=" * 60)
    print("SYNTHOS Agent Framework - Example Usage")
    print("=" * 60)
    
    # Create agent
    print("\nCreating SYNTHOS Agent...")
    agent = await create_agent("agent-001", "testnet")
    
    # Get initial status
    await example_monitoring(agent)
    
    # Run transaction flow example
    await example_transaction_flow(agent)
    
    # Run governance flow example
    await example_governance_flow(agent)
    
    # Show event history
    await example_event_history(agent)
    
    # Stop agent
    print(f"\nStopping agent...")
    await agent.stop()
    
    print(f"\nExample completed successfully!")
    print("=" * 60)


if __name__ == "__main__":
    asyncio.run(run_example())
