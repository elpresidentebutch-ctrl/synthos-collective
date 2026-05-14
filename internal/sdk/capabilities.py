from typing import Any, Dict, List, Callable
import dataclasses

@dataclasses.dataclass
class Capability:
    """A first-class action an agent can perform on the SYNTHOS Collective."""
    id: str
    description: str
    params: Dict[str, str] # name -> type
    constraint_hook: Callable[[Any], bool] # Logic to check if this is possible

class Intent:
    """A high-level objective expressed by an agent."""
    def __init__(self, objective: str, constraints: Dict[str, Any]):
        self.objective = objective
        self.constraints = constraints
        self.status = "PENDING"

class IntentRouter:
    """Maps high-level Intents to concrete Capability execution plans."""
    def __init__(self, registry: 'CapabilityRegistry'):
        self.registry = registry
        self.active_intents: List[Intent] = []

    def route(self, intent: Intent) -> List[Dict[str, Any]]:
        """The Routing Logic: Maps intent to capabilities."""
        plan = []
        
        # Example routing logic: "Rebalance" -> ["Unstake", "Stake"]
        if "rebalance" in intent.objective.lower():
            plan.append({"capability": "unstake", "params": {"amount": intent.constraints.get("target_delta")}})
            plan.append({"capability": "stake", "params": {"target_peer": intent.constraints.get("new_anchor")}})
            
        elif "secure" in intent.objective.lower():
            plan.append({"capability": "increase_redundancy", "params": {"peers": 3}})
            
        intent.status = "ROUTED"
        return plan

class CapabilityRegistry:
    """The canonical registry of 'What is possible' on Synthos."""
    def __init__(self):
        self.capabilities: Dict[str, Capability] = {}
        self._load_standard_capabilities()

    def _load_standard_capabilities(self):
        # 1. Staking Capability
        self.register(Capability(
            id="stake",
            description="Lock SYN to increase validation weight",
            params={"amount": "uint64", "peer_id": "string"},
            constraint_hook=lambda state: state.get("balance", 0) > 1000
        ))
        
        # 2. Voting Capability
        self.register(Capability(
            id="vote",
            description="Cast a ballot on an active governance proposal",
            params={"proposal_id": "uint64", "vote": "bool"},
            constraint_hook=lambda state: state.get("is_governor", False)
        ))

    def register(self, cap: Capability):
        self.capabilities[cap.id] = cap
        print(f"📡 Registry: Capability '{cap.id}' registered and discoverable.")

    def discover(self) -> List[Capability]:
        return list(self.capabilities.values())

# --- Usage Example ---
if __name__ == "__main__":
    registry = CapabilityRegistry()
    router = IntentRouter(registry)
    
    # Agent expresses a high-level intent
    my_intent = Intent(
        objective="Rebalance stake for better liveness",
        constraints={"target_delta": 5000, "new_anchor": "agent-mesh-12"}
    )
    
    # Router generates the execution plan
    execution_plan = router.route(my_intent)
    print(f"🧩 Execution Plan for Intent: {execution_plan}")
