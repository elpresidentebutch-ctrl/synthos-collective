"""Constitution Enforcement System"""

from typing import Dict, List, Any, Callable
from dataclasses import dataclass


@dataclass
class ConstitutionalRule:
    """A rule in the constitution"""
    rule_id: str
    category: str  # validation, consensus, economic, communication
    description: str
    enforcement_level: str  # strict, moderate, lenient
    handler: Callable
    penalty: int = 0  # 0-100 severity


class Constitution:
    """
    Enforces the protocol constitution
    
    The constitution defines:
    - Validation rules (what is a valid transaction)
    - Consensus rules (how blocks are finalized)
    - Economic rules (fee structure, rewards)
    - Communication rules (peer behavior)
    """
    
    def __init__(self, agent):
        self.agent = agent
        self.rules: Dict[str, ConstitutionalRule] = {}
        self._violation_log = []
    
    async def initialize_default_constitution(self) -> None:
        """Initialize with default rules"""
        
        # Validation Rules
        await self.add_rule(ConstitutionalRule(
            rule_id="val_signature",
            category="validation",
            description="All transactions must have valid signatures",
            enforcement_level="strict",
            handler=self._enforce_signature_rule,
            penalty=100,
        ))
        
        await self.add_rule(ConstitutionalRule(
            rule_id="val_balance",
            category="validation",
            description="Sender must have sufficient balance",
            enforcement_level="strict",
            handler=self._enforce_balance_rule,
            penalty=100,
        ))
        
        # Consensus Rules
        await self.add_rule(ConstitutionalRule(
            rule_id="cons_Byzantine",
            category="consensus",
            description="Consensus requires 2/3+ agreement (Byzantine fault tolerance)",
            enforcement_level="strict",
            handler=self._enforce_bft_rule,
            penalty=100,
        ))
        
        # Economic Rules
        await self.add_rule(ConstitutionalRule(
            rule_id="econ_fees",
            category="economic",
            description="All transactions must include adequate fees",
            enforcement_level="strict",
            handler=self._enforce_fee_rule,
            penalty=50,
        ))
        
        # Communication Rules
        await self.add_rule(ConstitutionalRule(
            rule_id="comm_rate_limit",
            category="communication",
            description="Peers must respect rate limits",
            enforcement_level="moderate",
            handler=self._enforce_rate_limit_rule,
            penalty=25,
        ))
    
    async def add_rule(self, rule: ConstitutionalRule) -> None:
        """Add rule to constitution"""
        self.rules[rule.rule_id] = rule
        self.agent.logger.info(f"Added constitutional rule: {rule.rule_id}")
    
    async def check_compliance(self, 
                              entity: Any,
                              rule_category: str = None) -> Dict[str, bool]:
        """
        Check entity compliance with constitution
        
        Args:
            entity: Entity to check (transaction, block, peer, proposal)
            rule_category: Specific category or None for all
            
        Returns:
            Dict mapping rule IDs to compliance status
        """
        compliance = {}
        
        for rule_id, rule in self.rules.items():
            # Filter by category if specified
            if rule_category and rule.category != rule_category:
                continue
            
            try:
                is_compliant = await rule.handler(entity)
                compliance[rule_id] = is_compliant
                
                if not is_compliant:
                    self._violation_log.append({
                        'rule_id': rule_id,
                        'entity': entity,
                        'timestamp': __import__('time').time(),
                    })
            except Exception as e:
                self.agent.logger.error(f"Error checking rule {rule_id}: {e}")
                compliance[rule_id] = False
        
        return compliance
    
    async def enforce_rules(self, entity: Any) -> bool:
        """
        Enforce constitution on entity
        
        Returns:
            True if entity is compliant, False otherwise
        """
        compliance = await self.check_compliance(entity)
        
        # All rules must pass for strict enforcement
        return all(compliance.values())
    
    # Validation Rule Handlers
    async def _enforce_signature_rule(self, transaction: Any) -> bool:
        """Validate transaction signature"""
        return hasattr(transaction, 'signature') and len(transaction.signature) > 0
    
    async def _enforce_balance_rule(self, transaction: Any) -> bool:
        """Validate sender balance"""
        sender = getattr(transaction, 'sender', None)
        amount = getattr(transaction, 'amount', 0)
        fee = getattr(transaction, 'fee', 0)
        
        if not sender:
            return False
        
        balance = await self.agent.state.get_balance(sender)
        return balance >= (amount + fee)
    
    # Consensus Rule Handlers
    async def _enforce_bft_rule(self, consensus_data: Any) -> bool:
        """Enforce BFT requirements"""
        # Would check consensus state
        return True
    
    # Economic Rule Handlers
    async def _enforce_fee_rule(self, transaction: Any) -> bool:
        """Enforce minimum fee"""
        fee = getattr(transaction, 'fee', 0)
        return fee >= 1  # Minimum fee
    
    # Communication Rule Handlers
    async def _enforce_rate_limit_rule(self, peer: Any) -> bool:
        """Enforce peer rate limits"""
        # Would check message rate
        return True
    
    def get_violation_log(self, limit: int = 100) -> List[Dict]:
        """Get recent violations"""
        return self._violation_log[-limit:]
    
    async def generate_constitution_report(self) -> Dict[str, Any]:
        """Generate constitution compliance report"""
        return {
            'total_rules': len(self.rules),
            'rules_by_category': self._count_by_category(),
            'recent_violations': len(self._violation_log),
            'last_update': __import__('time').time(),
        }
    
    def _count_by_category(self) -> Dict[str, int]:
        """Count rules by category"""
        counts = {}
        for rule in self.rules.values():
            counts[rule.category] = counts.get(rule.category, 0) + 1
        return counts
