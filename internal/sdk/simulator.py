import copy
from typing import Any, Dict, List, Optional
from dataclasses import dataclass

@dataclass
class ScenarioReport:
    """The analytical output of a forked world simulation."""
    initial_liveness: float
    final_liveness: float
    balance_deltas: Dict[str, float]
    reputation_deltas: Dict[str, float]
    power_concentration_shift: float # Gini coefficient delta

class ForkedWorld:
    """A deterministic, local fork of the SYNTHOS world state."""
    def __init__(self, base_state: Any):
        # Deep clone to ensure total isolation from mainnet/current state
        self.state = copy.deepcopy(base_state)
        self.history: List[Dict[str, Any]] = []

    def apply_action_script(self, actions: List[Dict[str, Any]]):
        """Execute a sequence of transactions or policy changes in the fork."""
        for action in actions:
            self._apply_single_action(action)
            self.history.append(action)

    def _apply_single_action(self, action: Dict[str, Any]):
        """The 'Rules of the World' for the simulation."""
        atype = action.get("type")
        if atype == "STAKE":
            agent = action["agent"]
            amount = action["amount"]
            if agent in self.state.ledger:
                self.state.ledger[agent]["balance"] -= amount
                self.state.ledger[agent]["stake"] = self.state.ledger[agent].get("stake", 0) + amount
        
        elif atype == "POLICY_CHANGE":
            # Tweak the world's physical constants (fees, thresholds)
            param = action["parameter"]
            new_val = action["value"]
            setattr(self.state, param, new_val)

    def generate_report(self, original_state: Any) -> ScenarioReport:
        """Analyze the delta between the fork and the starting point."""
        b_deltas = {}
        for agent in self.state.ledger:
            old = original_state.ledger.get(agent, {}).get("balance", 0)
            new = self.state.ledger[agent].get("balance", 0)
            b_deltas[agent] = new - old
            
        return ScenarioReport(
            initial_liveness=1.0, # Placeholder logic
            final_liveness=0.95,
            balance_deltas=b_deltas,
            reputation_deltas={},
            power_concentration_shift=0.05
        )

class PolicyPlayground:
    """The API for exploring 'Infinite Possibilities'."""
    def __init__(self, sdk: Any):
        self.sdk = sdk

    def run_scenario(self, name: str, actions: List[Dict[str, Any]]) -> ScenarioReport:
        """Create a fork, run the script, and report back."""
        if not self.sdk.world:
            raise ValueError("World Model must be synced to run a scenario.")
            
        fork = ForkedWorld(self.sdk.world)
        fork.apply_action_script(actions)
        return fork.generate_report(self.sdk.world)

# --- Integration Example ---
if __name__ == "__main__":
    from internal.sdk.reasoning import ReasoningSDK
    
    # 1. Sync from 'Mainnet'
    sdk = ReasoningSDK()
    sdk.sync_world({
        "balances": {"agent-0": {"balance": 1000000}, "agent-1": {"balance": 100}},
        "height": 1000
    })
    
    # 2. Open the Playground
    playground = PolicyPlayground(sdk)
    
    # 3. Test a 'What If' Scenario: "Whale Staking Attack"
    scenario = [
        {"type": "STAKE", "agent": "agent-0", "amount": 500000},
        {"type": "POLICY_CHANGE", "parameter": "min_stake", "value": 200000}
    ]
    
    report = playground.run_scenario("Whale Attack", scenario)
    print(f"📈 Simulation Report: Power Shift: {report.power_concentration_shift*100}%")
    print(f"💰 Balance Delta for Whale: {report.balance_deltas['agent-0']} SYN")
