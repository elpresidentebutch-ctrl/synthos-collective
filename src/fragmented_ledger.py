"""
fragmented_ledger.py

Agent-Native Fragmented Ledger Protocol for The Collective

Features:
- Ledger is split into fragments, each managed by a subset of agents.
- Agents poll only relevant fragment holders for transaction validation.
- Handshake and state verification are performed per fragment.
- No global broadcast; privacy and scalability are maximized.

Redundancy/Recovery Strategies:
1. Multi-Agent Replication:
    - Each fragment is held by multiple agents (replica set).
    - If a fragment is missing, any replica can be used to recover it.
    - Simple, robust, and easy to implement.

2. Parity-Based (Erasure Coding) Recovery:
    - Each fragment can have parity data (e.g., Reed-Solomon codes) distributed among agents.
    - If a fragment is missing, it can be reconstructed from parity data held by other agents.
    - More storage-efficient and resilient for large-scale systems.

This module demonstrates both approaches with clear example usage.
"""

import uuid
from typing import Dict, List, Any, Optional


class LedgerFragment:
    """
    Represents a single ledger fragment.
    - state: The actual data for this fragment (e.g., balances, ownership).
    - holders: List of agent IDs responsible for this fragment (for replication).
    - parity: Simulated parity data for erasure coding recovery.
    """
    def __init__(self, fragment_id: str):
        self.fragment_id = fragment_id
        self.state: Dict[str, Any] = {}  # e.g., balances, ownership, etc.
        self.holders: List[str] = []     # agent IDs responsible for this fragment (replication)
        self.parity: Optional[Dict[str, Any]] = None  # Simulated parity data (for erasure coding)

    def verify_and_apply(self, tx: dict) -> bool:
        # Placeholder: implement fragment-specific state change logic
        # Return True if state change is valid and applied
        return True

class Agent:
    """
    Represents an agent in the network.
    - known_fragments: Fragments this agent is aware of.
    - submit_transaction: Submits a transaction, triggering polling and recovery if needed.
    - self_heal_fragment_replication: Recovers missing fragments using replicas.
    - self_heal_fragment_parity: Recovers missing fragments using parity data.
    """
    def __init__(self, agent_id: str):
        self.agent_id = agent_id
        self.known_fragments: Dict[str, LedgerFragment] = {}

    def poll_fragment_holder(self, fragment: LedgerFragment, tx: dict) -> bool:
        # Simulate handshake and state verification
        return fragment.verify_and_apply(tx)

    def submit_transaction(self, tx: dict, fragment_map: Dict[str, LedgerFragment], recovery_mode: str = "replication") -> bool:
        """
        Submit a transaction, polling all relevant fragments.
        If a fragment is missing, attempt self-healing using the specified recovery_mode:
        - 'replication': Use redundant copies (replicas).
        - 'parity': Use parity data (simulated erasure coding).
        """
        relevant_fragments = tx.get('fragments', [])
        for frag_id in relevant_fragments:
            fragment = fragment_map.get(frag_id)
            if not fragment:
                print(f"Fragment {frag_id} not found. Attempting self-healing recovery...")
                if recovery_mode == "replication":
                    fragment = self.self_heal_fragment_replication(frag_id, fragment_map)
                elif recovery_mode == "parity":
                    fragment = self.self_heal_fragment_parity(frag_id, fragment_map)
                else:
                    fragment = None
                if not fragment:
                    print(f"Self-healing failed: Fragment {frag_id} could not be reconstructed.")
                    return False
                else:
                    print(f"Fragment {frag_id} successfully reconstructed via self-healing.")
            # Poll each holder (simulate with direct call)
            if not self.poll_fragment_holder(fragment, tx):
                print(f"Fragment {frag_id} rejected the transaction.")
                return False
        print("Transaction accepted by all relevant fragments.")
        return True

    def self_heal_fragment_replication(self, frag_id: str, fragment_map: Dict[str, LedgerFragment]) -> Optional[LedgerFragment]:
        """
        Attempt to reconstruct a missing fragment using simple replication (redundant copies).
        Looks for any fragment that lists frag_id as a replica and copies its state.
        """
        for other_id, fragment in fragment_map.items():
            if other_id != frag_id and frag_id in getattr(fragment, 'replicas', []):
                recovered = LedgerFragment(frag_id)
                recovered.state = fragment.state.copy()
                recovered.holders = fragment.holders.copy()
                fragment_map[frag_id] = recovered
                return recovered
        return None

    def self_heal_fragment_parity(self, frag_id: str, fragment_map: Dict[str, LedgerFragment]) -> Optional[LedgerFragment]:
        """
        Attempt to reconstruct a missing fragment using simulated parity data.
        Looks for a fragment that holds parity for frag_id and reconstructs from it.
        In a real system, this would use erasure coding (e.g., Reed-Solomon) to combine multiple shards.
        """
        for fragment in fragment_map.values():
            if fragment.parity and fragment.parity.get('for') == frag_id:
                recovered = LedgerFragment(frag_id)
                recovered.state = fragment.parity['data'].copy()
                recovered.holders = fragment.holders.copy()
                fragment_map[frag_id] = recovered
                return recovered
        return None

######################################################################
# Example usage: Demonstrates both redundancy strategies in action
######################################################################
if __name__ == "__main__":
    print("--- Multi-Agent Replication Example ---")
    # Create fragments
    fragment_map = {f"frag_{i}": LedgerFragment(f"frag_{i}") for i in range(3)}
    # Simulate frag_1 as a replica for frag_99
    fragment_map["frag_1"].replicas = ["frag_99"]
    # Create agent
    agent = Agent(agent_id=str(uuid.uuid4()))
    # Example transaction affecting a missing fragment (frag_99) and an existing one (frag_2)
    tx = {"fragments": ["frag_99", "frag_2"], "amount": 100, "from": "A", "to": "B"}
    print("Submitting transaction with a missing fragment (frag_99) using replication...")
    agent.submit_transaction(tx, fragment_map, recovery_mode="replication")

    print("\n--- Parity-Based Recovery Example ---")
    # Create new fragments for parity demo
    fragment_map2 = {f"frag_{i}": LedgerFragment(f"frag_{i}") for i in range(3)}
    # Simulate frag_1 holding parity for frag_77
    fragment_map2["frag_1"].parity = {"for": "frag_77", "data": {"amount": 999, "from": "X", "to": "Y"}}
    # Create agent
    agent2 = Agent(agent_id=str(uuid.uuid4()))
    tx2 = {"fragments": ["frag_77", "frag_2"], "amount": 50, "from": "C", "to": "D"}
    print("Submitting transaction with a missing fragment (frag_77) using parity...")
    agent2.submit_transaction(tx2, fragment_map2, recovery_mode="parity")
