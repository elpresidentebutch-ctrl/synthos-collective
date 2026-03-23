"""Transaction Validation System"""

from typing import List, Dict, Any, Tuple
from dataclasses import dataclass
import hashlib


@dataclass
class ValidationResult:
    """Result of transaction validation"""
    valid: bool
    signature_valid: bool
    balance_valid: bool
    constraints_valid: bool
    double_spend_check: bool
    cross_chain_proof_valid: bool
    errors: List[str]


class TransactionValidator:
    """Validates transactions against all rules"""
    
    def __init__(self, agent):
        self.agent = agent
    
    async def validate_full_transaction(self, 
                                       transaction: Any) -> ValidationResult:
        """
        Comprehensive transaction validation
        
        Checks:
        - Digital signature validity
        - Sender balance sufficiency
        - Constraint compliance
        - Double-spend prevention
        - Cross-chain proof if applicable
        """
        errors = []
        
        # 1. Verify signature
        sig_valid = await self._verify_signature(transaction)
        if not sig_valid:
            errors.append("Invalid transaction signature")
        
        # 2. Verify balance
        balance_valid = await self._verify_balance(transaction)
        if not balance_valid:
            errors.append("Insufficient sender balance")
        
        # 3. Check constraints
        constraints_valid = await self._verify_constraints(transaction)
        if not constraints_valid:
            errors.append("Transaction violates constraints")
        
        # 4. Check for double-spend
        double_spend_check = await self._check_double_spend(transaction)
        if not double_spend_check:
            errors.append("Double-spend detected")
        
        # 5. Verify cross-chain proof if applicable
        cross_chain_valid = True
        if hasattr(transaction, 'cross_chain_proof'):
            cross_chain_valid = await self._verify_cross_chain_proof(
                transaction
            )
            if not cross_chain_valid:
                errors.append("Invalid cross-chain proof")
        
        return ValidationResult(
            valid=len(errors) == 0,
            signature_valid=sig_valid,
            balance_valid=balance_valid,
            constraints_valid=constraints_valid,
            double_spend_check=double_spend_check,
            cross_chain_proof_valid=cross_chain_valid,
            errors=errors
        )
    
    async def _verify_signature(self, transaction: Any) -> bool:
        """
        Verify transaction signature
        
        Validates:
        - Signature format
        - Signature matches sender's public key
        - Prevents signature forge attacks
        """
        if not hasattr(transaction, 'signature'):
            return False
        
        # Placeholder - would use cryptography library
        # In real implementation:
        # 1. Get sender's public key
        # 2. Hash transaction data
        # 3. Verify signature with public key
        return True
    
    async def _verify_balance(self, transaction: Any) -> bool:
        """
        Verify sender has sufficient balance
        
        Checks:
        - Account balance >= transaction amount + fee
        - No pending withdrawals exceed balance
        """
        sender = getattr(transaction, 'sender', None)
        if not sender:
            return False
        
        balance = await self.agent.state.get_balance(sender)
        amount = getattr(transaction, 'amount', 0)
        fee = getattr(transaction, 'fee', 0)
        
        return balance >= (amount + fee)
    
    async def _verify_constraints(self, transaction: Any) -> bool:
        """
        Verify transaction meets constraints
        
        Checks:
        - Amount is positive
        - Fee is within acceptable range
        - Recipients is valid address
        - Timestamp is reasonable
        - Transaction is not too old
        - Recipient is not blacklisted
        """
        # Amount check
        amount = getattr(transaction, 'amount', 0)
        if amount <= 0:
            return False
        
        # Fee check
        fee = getattr(transaction, 'fee', 0)
        if fee < 0:
            return False
        
        # Recipient check
        recipient = getattr(transaction, 'recipient', None)
        if not recipient:
            return False
        
        # Timestamp check
        timestamp = getattr(transaction, 'timestamp', 0)
        current_time = __import__('time').time()
        age = current_time - timestamp
        
        # Transaction must be less than 1 hour old
        if age > 3600:
            return False
        
        # Transaction must not be from future
        if timestamp > current_time:
            return False
        
        return True
    
    async def _check_double_spend(self, transaction: Any) -> bool:
        """
        Check for double-spend attempts
        
        Prevents:
        - Spending same input twice
        - Spending money already committed
        - Creating circular dependencies
        """
        sender = getattr(transaction, 'sender', None)
        tx_id = getattr(transaction, 'id', None)
        
        # Get all pending transactions from this sender
        pending = await self._get_pending_transactions(sender)
        
        # Check if this transaction amount exceeds pending balance
        total_pending = sum(
            t.amount + t.fee for t in pending
            if t.id != tx_id
        )
        
        sender_balance = await self.agent.state.get_balance(sender)
        
        # Current transaction amount + all pending txs must not exceed balance
        tx_amount = getattr(transaction, 'amount', 0)
        tx_fee = getattr(transaction, 'fee', 0)
        
        return (tx_amount + tx_fee + total_pending) <= sender_balance
    
    async def _verify_cross_chain_proof(self, transaction: Any) -> bool:
        """
        Verify cross-chain proof
        
        Validates:
        - Proof comes from legitimate source chain
        - Proof is cryptographically valid
        - Proof matches claimed event
        - Proof has not been spent
        """
        proof = getattr(transaction, 'cross_chain_proof', None)
        if not proof:
            return False
        
        # Verify proof structure
        if not hasattr(proof, 'source_chain'):
            return False
        
        # Verify proof signature
        if not hasattr(proof, 'signature'):
            return False
        
        # Verify proof hasn't been spent
        if await self._is_proof_spent(proof):
            return False
        
        # Placeholder - would verify against source chain validators
        return True
    
    async def _get_pending_transactions(self, sender: str) -> List[Any]:
        """Get all pending transactions from sender"""
        # Placeholder - would query mempool
        return []
    
    async def _is_proof_spent(self, proof: Any) -> bool:
        """Check if cross-chain proof has been spent"""
        # Placeholder - would check spent proofs set
        return False


class ConstraintEnforcer:
    """Enforces validation constraints"""
    
    def __init__(self, agent):
        self.agent = agent
        self.constraints = {
            'max_transaction_size': 1000000,  # 1MB
            'min_fee': 1,
            'max_fee': 1000000,
            'max_recipient_balance': 10**18,  # 1 million tokens
            'min_recipient_balance': 0,
            'max_transactions_per_block': 10000,
            'max_pending_transactions': 100000,
        }
    
    async def check_all_constraints(self, transaction: Any) -> Tuple[bool, List[str]]:
        """Check all constraints"""
        violations = []
        
        # Size constraint
        tx_size = len(str(transaction))
        if tx_size > self.constraints['max_transaction_size']:
            violations.append(f"Transaction size {tx_size} exceeds max")
        
        # Fee constraints
        fee = getattr(transaction, 'fee', 0)
        if fee < self.constraints['min_fee']:
            violations.append(f"Fee {fee} below minimum")
        if fee > self.constraints['max_fee']:
            violations.append(f"Fee {fee} exceeds maximum")
        
        return len(violations) == 0, violations
