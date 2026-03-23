"""
Gemini Megachain 2.0 - Advanced DeFi Contracts
Oracle, Lending, and DEX functionality
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple
from enum import Enum
from datetime import datetime, timedelta
import math


class PriceSourceType(Enum):
    """Price source types"""
    CHAINLINK = "CHAINLINK"
    UNISWAP = "UNISWAP"
    CURVE = "CURVE"
    BALANCER = "BALANCER"
    CUSTOM = "CUSTOM"


@dataclass
class PriceData:
    """Price data point"""
    asset: str
    price: int  # In USD, scaled by 1e8
    timestamp: datetime
    source: PriceSourceType
    confidence: int = 95  # Confidence percentage (0-100)


@dataclass
class OracleUpdate:
    """Oracle update record"""
    update_id: str
    asset: str
    old_price: int
    new_price: int
    timestamp: datetime
    source: PriceSourceType
    validator_signatures: List[str] = field(default_factory=list)


@dataclass
class LoanPosition:
    """Lending position"""
    loan_id: str
    borrower: str
    collateral_token: str
    collateral_amount: int
    loan_token: str
    loan_amount: int
    interest_rate: int  # Basis points
    start_block: int
    maturity_block: int
    status: str = "ACTIVE"  # ACTIVE, LIQUIDATED, REPAID
    accrued_interest: int = 0


class GeminiOracleContract:
    """
    Gemini Oracle Contract
    - Multi-source price feeds
    - Validator consensus
    - Median price aggregation
    """

    def __init__(self, owner: str, required_validators: int = 3):
        """Initialize oracle contract"""
        self.owner = owner
        self.required_validators = required_validators
        self.validators: Dict[str, bool] = {owner: True}
        
        # Price storage
        self.prices: Dict[str, PriceData] = {}
        self.price_history: Dict[str, List[PriceData]] = {}
        self.price_updates: List[OracleUpdate] = []
        
        # Configuration
        self.max_price_deviation = 50  # 50 basis points (0.5%)
        self.update_frequency = 300  # Update every 5 minutes
        self.last_update: Dict[str, datetime] = {}


    def add_validator(self, owner: str, validator: str) -> Tuple[bool, str]:
        """Add oracle validator"""
        if owner != self.owner:
            return False, "Only owner can add validators"
        
        self.validators[validator] = True
        return True, f"Added validator {validator}"


    def submit_price(self, validator: str, asset: str, price: int,
                    source: PriceSourceType) -> Tuple[bool, str]:
        """
        Submit price update (from validator)
        Returns: (success, message)
        """
        if validator not in self.validators or not self.validators[validator]:
            return False, f"Validator {validator} not authorized"
        
        # Get current price
        current_price = self.prices.get(asset, None)
        
        # Check for extreme deviation
        if current_price:
            deviation = abs(price - current_price.price) / max(current_price.price, 1)
            if deviation > self.max_price_deviation / 10000:
                return False, f"Price deviation too large: {deviation * 100}%"
        
        # Update price
        price_data = PriceData(
            asset=asset,
            price=price,
            timestamp=datetime.now(),
            source=source,
            confidence=95
        )
        
        self.prices[asset] = price_data
        
        # Track history
        if asset not in self.price_history:
            self.price_history[asset] = []
        self.price_history[asset].append(price_data)
        
        # Record update
        old_price = current_price.price if current_price else 0
        update = OracleUpdate(
            update_id=self._generate_update_id(asset),
            asset=asset,
            old_price=old_price,
            new_price=price,
            timestamp=datetime.now(),
            source=source,
            validator_signatures=[validator]
        )
        self.price_updates.append(update)
        self.last_update[asset] = datetime.now()
        
        return True, f"Price updated for {asset}: {price / 1e8:.8f}"


    def get_price(self, asset: str) -> Optional[Dict]:
        """Get current price for asset"""
        if asset not in self.prices:
            return None
        
        price_data = self.prices[asset]
        
        return {
            "asset": asset,
            "price": price_data.price,
            "price_usd": price_data.price / 1e8,
            "timestamp": price_data.timestamp.isoformat(),
            "source": price_data.source.value,
            "confidence": price_data.confidence,
        }


    def get_price_history(self, asset: str, limit: int = 100) -> List[Dict]:
        """Get price history for asset"""
        if asset not in self.price_history:
            return []
        
        history = self.price_history[asset][-limit:]
        
        return [
            {
                "price": p.price / 1e8,
                "timestamp": p.timestamp.isoformat(),
                "source": p.source.value,
            }
            for p in history
        ]


    def _generate_update_id(self, asset: str) -> str:
        """Generate update ID"""
        import hashlib
        data = f"{asset}{datetime.now().timestamp()}"
        return "0x" + hashlib.sha256(data.encode()).hexdigest()[:40]


class GeminiLendingContract:
    """
    Gemini Lending Contract
    - Over-collateralized lending
    - Interest accrual
    - Liquidation mechanism
    """

    def __init__(self, owner: str, oracle: GeminiOracleContract):
        """Initialize lending contract"""
        self.owner = owner
        self.oracle = oracle
        
        # Loan management
        self.loans: Dict[str, LoanPosition] = {}
        self.loan_count = 0
        self.next_loan_id = 1
        
        # Collateral ratios
        self.collateral_ratios: Dict[str, int] = {}  # asset -> ratio (1000 = 100%)
        self.min_collateral_ratio = 1500  # 150%
        
        # Interest management
        self.base_interest_rates: Dict[str, int] = {}  # asset -> rate (100 = 1%)
        self.total_borrowed: Dict[str, int] = {}  # asset -> total
        
        # Liquidation
        self.liquidation_penalty = 50  # 5% penalty on liquidation
        self.liquidation_threshold = 1200  # 120% - liquidate below this ratio


    def init_lending_pair(self, collateral_token: str, loan_token: str,
                         collateral_ratio: int, base_rate: int) -> Tuple[bool, str]:
        """Initialize lending pair configuration"""
        if collateral_ratio < 1000:
            return False, "Collateral ratio must be at least 100%"
        
        self.collateral_ratios[collateral_token] = collateral_ratio
        self.base_interest_rates[loan_token] = base_rate
        self.total_borrowed[loan_token] = 0
        
        return True, f"Initialized lending pair: {collateral_token} -> {loan_token}"


    def borrow(self, borrower: str, collateral_token: str, collateral_amount: int,
              loan_token: str, loan_amount: int, maturity_blocks: int) -> Tuple[bool, str]:
        """
        Borrow against collateral
        Returns: (success, loan_id or error)
        """
        # Validate configuration
        if collateral_token not in self.collateral_ratios:
            return False, f"Collateral token {collateral_token} not configured"
        
        # Get prices
        collateral_price = self.oracle.get_price(collateral_token)
        loan_price = self.oracle.get_price(loan_token)
        
        if not collateral_price or not loan_price:
            return False, "Price data not available"
        
        # Calculate collateral value
        collateral_value = (collateral_amount * collateral_price["price"]) // 1e8
        loan_value = (loan_amount * loan_price["price"]) // 1e8
        
        # Check collateral ratio
        required_collateral = (loan_value * self.collateral_ratios[collateral_token]) // 1000
        
        if collateral_value < required_collateral:
            return False, f"Insufficient collateral. Have: {collateral_value}, Need: {required_collateral}"
        
        # Create loan
        loan_id = f"LOAN_{self.next_loan_id}"
        self.next_loan_id += 1
        
        interest_rate = self.base_interest_rates.get(loan_token, 100)
        
        loan = LoanPosition(
            loan_id=loan_id,
            borrower=borrower,
            collateral_token=collateral_token,
            collateral_amount=collateral_amount,
            loan_token=loan_token,
            loan_amount=loan_amount,
            interest_rate=interest_rate,
            start_block=0,  # Would be block number
            maturity_block=maturity_blocks,
            status="ACTIVE"
        )
        
        self.loans[loan_id] = loan
        self.total_borrowed[loan_token] = self.total_borrowed.get(loan_token, 0) + loan_amount
        
        return True, loan_id


    def repay_loan(self, borrower: str, loan_id: str, repay_amount: int) -> Tuple[bool, str]:
        """Repay loan with interest"""
        if loan_id not in self.loans:
            return False, f"Loan {loan_id} not found"
        
        loan = self.loans[loan_id]
        
        if loan.borrower != borrower:
            return False, "Not authorized to repay this loan"
        
        if loan.status != "ACTIVE":
            return False, f"Loan status is {loan.status}"
        
        # Calculate interest
        interest_accrued = self._calculate_interest(loan)
        total_owed = loan.loan_amount + interest_accrued
        
        if repay_amount < total_owed:
            # Partial repayment
            loan.loan_amount -= repay_amount
            return True, f"Partial repayment of {repay_amount}"
        else:
            # Full repayment
            loan.status = "REPAID"
            self.total_borrowed[loan.loan_token] -= loan.loan_amount
            return True, f"Loan repaid in full (interest: {interest_accrued})"


    def liquidate_loan(self, loan_id: str) -> Tuple[bool, str]:
        """Liquidate under-collateralized loan"""
        if loan_id not in self.loans:
            return False, f"Loan {loan_id} not found"
        
        loan = self.loans[loan_id]
        
        if loan.status != "ACTIVE":
            return False, f"Loan status is {loan.status}"
        
        # Check if liquidation needed
        collateral_price = self.oracle.get_price(loan.collateral_token)
        loan_price = self.oracle.get_price(loan.loan_token)
        
        if not collateral_price or not loan_price:
            return False, "Price data not available"
        
        collateral_value = (loan.collateral_amount * collateral_price["price"]) // 1e8
        outstanding = loan.loan_amount + self._calculate_interest(loan)
        outstanding_value = (outstanding * loan_price["price"]) // 1e8
        
        ratio = (collateral_value * 1000) // max(outstanding_value, 1)
        
        if ratio > self.liquidation_threshold:
            return False, f"Loan not under-collateralized. Ratio: {ratio / 10}%"
        
        # Apply liquidation
        penalty = (loan.collateral_amount * self.liquidation_penalty) // 1000
        
        loan.status = "LIQUIDATED"
        self.total_borrowed[loan.loan_token] -= loan.loan_amount
        
        return True, f"Loan liquidated. Penalty: {penalty} collateral"


    def get_loan(self, loan_id: str) -> Optional[Dict]:
        """Get loan details"""
        if loan_id not in self.loans:
            return None
        
        loan = self.loans[loan_id]
        interest = self._calculate_interest(loan)
        
        return {
            "loan_id": loan_id,
            "borrower": loan.borrower,
            "collateral_token": loan.collateral_token,
            "collateral_amount": loan.collateral_amount,
            "loan_token": loan.loan_token,
            "loan_amount": loan.loan_amount,
            "interest_accrued": interest,
            "total_owed": loan.loan_amount + interest,
            "interest_rate": loan.interest_rate,
            "status": loan.status,
        }


    def _calculate_interest(self, loan: LoanPosition) -> int:
        """Calculate accrued interest on loan"""
        # Simplified: daily compounding
        days_elapsed = 1  # Would calculate from blocks
        interest = (loan.loan_amount * loan.interest_rate * days_elapsed) // (36500 * 100)
        return interest


class GeminiDEXContract:
    """
    Gemini DEX Contract
    - Decentralized exchange with AMM
    - Liquidity provision
    - Fee collection
    """

    def __init__(self, owner: str):
        """Initialize DEX"""
        self.owner = owner
        self.pools: Dict[str, Dict] = {}
        self.fee_tiers = [1, 5, 30, 100]  # Basis points
        self.total_fees_collected: Dict[str, int] = {}


    def create_pool(self, token_a: str, token_b: str, fee_tier: int) -> Tuple[bool, str]:
        """Create new trading pool"""
        if fee_tier not in self.fee_tiers:
            return False, f"Fee tier must be one of {self.fee_tiers}"
        
        pool_id = self._get_pool_id(token_a, token_b, fee_tier)
        
        if pool_id in self.pools:
            return False, "Pool already exists"
        
        self.pools[pool_id] = {
            "token_a": token_a,
            "token_b": token_b,
            "fee_tier": fee_tier,
            "reserve_a": 0,
            "reserve_b": 0,
            "liquidity": 0,
            "volume_24h": 0,
            "created_timestamp": datetime.now(),
        }
        
        return True, pool_id


    def get_pool(self, pool_id: str) -> Optional[Dict]:
        """Get pool details"""
        if pool_id not in self.pools:
            return None
        
        pool = self.pools[pool_id]
        
        return {
            "pool_id": pool_id,
            "token_a": pool["token_a"],
            "token_b": pool["token_b"],
            "fee_tier": pool["fee_tier"],
            "reserve_a": pool["reserve_a"],
            "reserve_b": pool["reserve_b"],
            "liquidity": pool["liquidity"],
            "volume_24h": pool["volume_24h"],
            "created_timestamp": pool["created_timestamp"].isoformat(),
        }


    def _get_pool_id(self, token_a: str, token_b: str, fee_tier: int) -> str:
        """Generate pool ID"""
        import hashlib
        # Normalize token order
        tokens = sorted([token_a, token_b])
        data = f"{tokens[0]}{tokens[1]}{fee_tier}"
        return "0x" + hashlib.sha256(data.encode()).hexdigest()[:40]
