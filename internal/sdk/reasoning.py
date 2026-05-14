import abc
import copy
import time
from typing import Any, Dict, List, Optional

# --- World Entities ---

class EconomyEntity:
    """Reasoning entity for the network's financial state."""
    def __init__(self, state: 'WorldState'):
        self.total_supply = sum(v.get('balance', 0) for v in state.ledger.values())
        self.avg_balance = self.total_supply / max(len(state.ledger), 1)
        
    def assess_inflation_risk(self, new_emission: float) -> str:
        ratio = new_emission / self.total_supply
        if ratio > 0.05: return "HIGH"
        return "STABLE"

class TrustEntity:
    """Reasoning entity for the network's social/security state."""
    def __init__(self, state: 'WorldState'):
        self.peer_reputation = state.reputation
        
    def detect_anomaly(self, peer_id: str) -> bool:
        # Detect sudden drops in reputation
        return self.peer_reputation.get(peer_id, 1.0) < 0.2

class WorldState:
    """The Semantic World Model (The State Adapter)."""
    def __init__(self, ledger: Dict[str, Any], reputation: Dict[str, float], height: int):
        self.ledger = ledger
        self.reputation = reputation
        self.height = height
        self.timestamp = time.time()
        
        # Semantic Entities
        self.economy = EconomyEntity(self)
        self.trust = TrustEntity(self)

    def get_agent_strength(self, agent_id: str) -> float:
        balance = self.ledger.get(agent_id, {}).get("balance", 0)
        rep = self.reputation.get(agent_id, 1.0)
        return (balance * 0.7) + (rep * 0.3)

# --- Reasoning Hooks ---

class PolicyHook(abc.ABC):
    @abc.abstractmethod
    def evaluate_action(self, state: WorldState, action: Any) -> bool:
        pass

class ReasoningSDK:
    """
    SYNTHOS Reasoning SDK v1.1
    Turns the blockchain into a queryable World Model for agents.
    """
    def __init__(self, local_node_url: str = "http://localhost:8080"):
        self.url = local_node_url
        self.world: Optional[WorldState] = None
        self.policies: List[PolicyHook] = []

    def sync_world(self, raw_rpc_data: Dict[str, Any]):
        """Adapter: Hydrates World Entities from raw JSON-RPC data."""
        self.world = WorldState(
            ledger=raw_rpc_data.get("balances", {}),
            reputation=raw_rpc_data.get("reputation", {}),
            height=raw_rpc_data.get("height", 0)
        )

    def think(self, action: Any) -> bool:
        """
        The Reasoning Loop:
        1. Create a Mental Sandbox (Simulation Hook)
        2. Test Future State against Policies (Policy Hook)
        """
        if not self.world: return False
        
        # Simulation: Predict the outcome of this action
        projected_world = self._simulate_future(action)
        
        # Policy: Evaluate the projected world
        for policy in self.policies:
            if not policy.evaluate_action(projected_world, action):
                return False
        return True

    def _simulate_future(self, action: Any) -> WorldState:
        """Simulation Hook: Mental Sandbox."""
        future = copy.deepcopy(self.world)
        
        # Apply hypothetical changes
        if action.get("type") == "TRANSACTION":
            amount = action.get("amount", 0)
            sender = action.get("from")
            if sender in future.ledger:
                future.ledger[sender]['balance'] -= amount
        
        return future

# --- Example Policy: 'Stability First' ---
class StabilityPolicy(PolicyHook):
    def evaluate_action(self, state: WorldState, action: Any) -> bool:
        # Reason about the economic state of the world
        if state.economy.assess_inflation_risk(1000) == "HIGH":
            print("🚫 Policy: Rejecting action due to inflation risk.")
            return False
        return True

if __name__ == "__main__":
    sdk = ReasoningSDK()
    # Mock node state
    sdk.sync_world({
        "height": 100, 
        "balances": {"agent-alpha": {"balance": 5000}, "agent-beta": {"balance": 10}},
        "reputation": {"agent-alpha": 0.9}
    })
    
    sdk.add_policy(StabilityPolicy())
    
    # The Agent 'Thinks' before acting
    should_act = sdk.think({"type": "TRANSACTION", "from": "agent-alpha", "amount": 100})
    print(f"Agent decision to act: {should_act}")
