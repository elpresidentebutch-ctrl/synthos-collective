"""
SYNTHOS Token Contract (SynthosERC20)
Native token for SYNTHOS Agent civilization with governance integration
"""

from dataclasses import dataclass, field
from typing import Dict, Optional, List, Tuple
from enum import Enum
from datetime import datetime, timedelta
import hashlib
import json
from uuid import uuid4


class TokenEventType(Enum):
    """Token event types"""
    TRANSFER = "TRANSFER"
    APPROVE = "APPROVE"
    MINT = "MINT"
    BURN = "BURN"
    REWARD = "REWARD"
    PENALTY = "PENALTY"
    SNAPSHOT = "SNAPSHOT"


@dataclass
class TokenTransfer:
    """Token transfer record"""
    from_address: str
    to_address: str
    amount: int
    timestamp: datetime
    tx_hash: str
    reason: Optional[str] = None
    metadata: Dict = field(default_factory=dict)


@dataclass
class TokenApproval:
    """Token approval for spending"""
    owner: str
    spender: str
    amount: int
    timestamp: datetime
    expiry: Optional[datetime] = None


@dataclass
class TokenSnapshot:
    """Balance snapshot for governance voting"""
    snapshot_id: str
    block_height: int
    timestamp: datetime
    balances: Dict[str, int]
    total_supply: int
    owner_addresses: List[str]


@dataclass
class TokenMintEvent:
    """Mint event record"""
    minter: str
    amount: int
    timestamp: datetime
    reason: str
    tx_hash: str


class SynthosTokenContract:
    """
    SYNTHOS ERC20-compatible token contract
    - 1 billion total supply
    - 18 decimals
    - Governance voting integration
    - Snapshot mechanism for voting
    """

    def __init__(self, owner: str, name: str = "SYNTHOS Token", symbol: str = "SYN"):
        """Initialize token contract"""
        self.name = name
        self.symbol = symbol
        self.decimals = 18
        
        # Supply management
        self.initial_supply = 10**9 * (10 ** self.decimals)  # 1 billion tokens
        self.total_supply = self.initial_supply
        self.max_supply = 10**9 * (10 ** self.decimals)  # No inflation beyond initial
        
        # Balances and allowances
        self.balances: Dict[str, int] = {owner: self.initial_supply}
        self.allowances: Dict[str, Dict[str, int]] = {}
        
        # Access control
        self.owner = owner
        self.minters: Dict[str, bool] = {owner: True}
        self.burners: Dict[str, bool] = {owner: True}
        
        # History and snapshots
        self.transfer_history: List[TokenTransfer] = []
        self.approval_history: List[TokenApproval] = []
        self.mint_history: List[TokenMintEvent] = []
        self.snapshots: Dict[str, TokenSnapshot] = {}
        
        # Governance integration
        self.voting_snapshots: Dict[int, TokenSnapshot] = {}  # block_height -> snapshot
        self.delegation_enabled = True
        self.delegation: Dict[str, str] = {}  # delegator -> delegatee
        
        # Pause mechanism
        self.paused = False
        self.transfer_whitelist: Dict[str, bool] = {}


    def balance_of(self, address: str) -> int:
        """Get token balance for address"""
        return self.balances.get(address, 0)


    def allowance(self, owner: str, spender: str) -> int:
        """Get allowance from owner to spender"""
        if owner not in self.allowances:
            return 0
        return self.allowances[owner].get(spender, 0)


    def transfer(self, from_addr: str, to_addr: str, amount: int, 
                reason: Optional[str] = None, metadata: Optional[Dict] = None) -> Tuple[bool, str]:
        """
        Transfer tokens from one address to another
        Returns: (success, message)
        """
        if self.paused:
            return False, "Token transfers are paused"
        
        if amount <= 0:
            return False, "Transfer amount must be positive"
        
        if from_addr not in self.balances or self.balances[from_addr] < amount:
            return False, f"Insufficient balance. Have: {self.balances.get(from_addr, 0)}, Need: {amount}"
        
        # Check whitelist if enabled
        if self.transfer_whitelist and from_addr not in self.transfer_whitelist:
            return False, "From address not in whitelist"
        
        # Execute transfer
        self.balances[from_addr] -= amount
        self.balances[to_addr] = self.balances.get(to_addr, 0) + amount
        
        # Record transfer
        tx_hash = self._generate_tx_hash(from_addr, to_addr, amount)
        transfer = TokenTransfer(
            from_address=from_addr,
            to_address=to_addr,
            amount=amount,
            timestamp=datetime.now(),
            tx_hash=tx_hash,
            reason=reason,
            metadata=metadata or {}
        )
        self.transfer_history.append(transfer)
        
        return True, tx_hash


    def transfer_from(self, spender: str, from_addr: str, to_addr: str, amount: int) -> Tuple[bool, str]:
        """
        Transfer tokens on behalf of owner (requires approval)
        Returns: (success, message)
        """
        if self.paused:
            return False, "Token transfers are paused"
        
        # Check allowance
        allowance = self.allowance(from_addr, spender)
        if allowance < amount:
            return False, f"Insufficient allowance. Have: {allowance}, Need: {amount}"
        
        # Reduce allowance
        if from_addr not in self.allowances:
            self.allowances[from_addr] = {}
        self.allowances[from_addr][spender] -= amount
        
        # Execute transfer
        return self.transfer(from_addr, to_addr, amount, reason="transfer_from")


    def approve(self, owner: str, spender: str, amount: int, 
               expiry: Optional[datetime] = None) -> Tuple[bool, str]:
        """
        Approve spender to transfer amount on behalf of owner
        Returns: (success, message)
        """
        if amount < 0:
            return False, "Approval amount cannot be negative"
        
        if owner not in self.allowances:
            self.allowances[owner] = {}
        
        self.allowances[owner][spender] = amount
        
        # Record approval
        approval = TokenApproval(
            owner=owner,
            spender=spender,
            amount=amount,
            timestamp=datetime.now(),
            expiry=expiry
        )
        self.approval_history.append(approval)
        
        return True, f"Approved {spender} to spend {amount} tokens"


    def increase_allowance(self, owner: str, spender: str, add_amount: int) -> Tuple[bool, str]:
        """Increase allowance safely"""
        current = self.allowance(owner, spender)
        new_amount = current + add_amount
        return self.approve(owner, spender, new_amount)


    def decrease_allowance(self, owner: str, spender: str, sub_amount: int) -> Tuple[bool, str]:
        """Decrease allowance safely"""
        current = self.allowance(owner, spender)
        if sub_amount > current:
            return False, "Subtraction would result in negative allowance"
        new_amount = current - sub_amount
        return self.approve(owner, spender, new_amount)


    def mint(self, minter: str, to_addr: str, amount: int, reason: str) -> Tuple[bool, str]:
        """
        Mint new tokens (only by authorized minters)
        Returns: (success, message)
        """
        if minter not in self.minters or not self.minters[minter]:
            return False, f"Address {minter} is not authorized to mint"
        
        if self.total_supply + amount > self.max_supply:
            return False, f"Mint would exceed max supply. Current: {self.total_supply}, Max: {self.max_supply}"
        
        # Execute mint
        self.balances[to_addr] = self.balances.get(to_addr, 0) + amount
        self.total_supply += amount
        
        # Record mint
        tx_hash = self._generate_tx_hash(minter, to_addr, amount)
        mint_event = TokenMintEvent(
            minter=minter,
            amount=amount,
            timestamp=datetime.now(),
            reason=reason,
            tx_hash=tx_hash
        )
        self.mint_history.append(mint_event)
        
        return True, f"Minted {amount} tokens to {to_addr}. Reason: {reason}"


    def burn(self, burner: str, from_addr: str, amount: int, reason: str) -> Tuple[bool, str]:
        """
        Burn tokens (permanently remove from supply)
        Returns: (success, message)
        """
        if burner not in self.burners or not self.burners[burner]:
            return False, f"Address {burner} is not authorized to burn"
        
        if from_addr not in self.balances or self.balances[from_addr] < amount:
            return False, f"Insufficient balance to burn. Have: {self.balances.get(from_addr, 0)}"
        
        # Execute burn
        self.balances[from_addr] -= amount
        self.total_supply -= amount
        
        # Record transfer (special burn address)
        tx_hash = self._generate_tx_hash(from_addr, "0x0000000000000000", amount)
        transfer = TokenTransfer(
            from_address=from_addr,
            to_address="0x0000000000000000",  # Burn address
            amount=amount,
            timestamp=datetime.now(),
            tx_hash=tx_hash,
            reason=f"BURN: {reason}",
            metadata={"burned": True}
        )
        self.transfer_history.append(transfer)
        
        return True, f"Burned {amount} tokens from {from_addr}. Reason: {reason}"


    def create_snapshot(self, block_height: int) -> Tuple[bool, str]:
        """
        Create snapshot for governance voting
        Returns: (success, snapshot_id or error)
        """
        snapshot_id = str(uuid4())
        
        snapshot = TokenSnapshot(
            snapshot_id=snapshot_id,
            block_height=block_height,
            timestamp=datetime.now(),
            balances=dict(self.balances),  # Deep copy
            total_supply=self.total_supply,
            owner_addresses=list(self.balances.keys())
        )
        
        self.snapshots[snapshot_id] = snapshot
        self.voting_snapshots[block_height] = snapshot
        
        return True, snapshot_id


    def get_snapshot_balance(self, snapshot_id: str, address: str) -> int:
        """Get balance at specific snapshot"""
        if snapshot_id not in self.snapshots:
            return 0
        snapshot = self.snapshots[snapshot_id]
        return snapshot.balances.get(address, 0)


    def delegate(self, delegator: str, delegatee: str) -> Tuple[bool, str]:
        """
        Delegate voting power to another address
        Returns: (success, message)
        """
        if not self.delegation_enabled:
            return False, "Delegation is disabled"
        
        if delegator == delegatee:
            return False, "Cannot delegate to yourself"
        
        self.delegation[delegator] = delegatee
        return True, f"Delegated voting power from {delegator} to {delegatee}"


    def get_voting_power(self, address: str, snapshot_id: Optional[str] = None) -> int:
        """
        Get voting power for address (including delegated power)
        """
        # Get own balance
        if snapshot_id:
            power = self.get_snapshot_balance(snapshot_id, address)
        else:
            power = self.balance_of(address)
        
        # Add delegated power (from those who delegated to this address)
        for delegator, delegatee in self.delegation.items():
            if delegatee == address:
                if snapshot_id:
                    power += self.get_snapshot_balance(snapshot_id, delegator)
                else:
                    power += self.balance_of(delegator)
        
        return power


    def add_minter(self, owner: str, minter: str) -> Tuple[bool, str]:
        """Add minter role"""
        if owner != self.owner:
            return False, "Only owner can add minters"
        
        self.minters[minter] = True
        return True, f"Added {minter} as minter"


    def remove_minter(self, owner: str, minter: str) -> Tuple[bool, str]:
        """Remove minter role"""
        if owner != self.owner:
            return False, "Only owner can remove minters"
        
        self.minters[minter] = False
        return True, f"Removed {minter} as minter"


    def pause_transfers(self, owner: str) -> Tuple[bool, str]:
        """Pause all token transfers"""
        if owner != self.owner:
            return False, "Only owner can pause transfers"
        
        self.paused = True
        return True, "Token transfers paused"


    def resume_transfers(self, owner: str) -> Tuple[bool, str]:
        """Resume token transfers"""
        if owner != self.owner:
            return False, "Only owner can resume transfers"
        
        self.paused = False
        return True, "Token transfers resumed"


    def add_to_whitelist(self, owner: str, address: str) -> Tuple[bool, str]:
        """Add address to transfer whitelist"""
        if owner != self.owner:
            return False, "Only owner can manage whitelist"
        
        self.transfer_whitelist[address] = True
        return True, f"Added {address} to whitelist"


    def get_transfer_history(self, address: Optional[str] = None, limit: int = 100) -> List[TokenTransfer]:
        """Get transfer history for address"""
        history = self.transfer_history
        
        if address:
            history = [t for t in history if t.from_address == address or t.to_address == address]
        
        return history[-limit:]


    def get_contract_state(self) -> Dict:
        """Get full contract state"""
        return {
            "name": self.name,
            "symbol": self.symbol,
            "decimals": self.decimals,
            "total_supply": self.total_supply,
            "max_supply": self.max_supply,
            "owner": self.owner,
            "paused": self.paused,
            "balance_count": len(self.balances),
            "total_holders": sum(1 for b in self.balances.values() if b > 0),
            "transfer_history_size": len(self.transfer_history),
            "approval_history_size": len(self.approval_history),
            "mint_history_size": len(self.mint_history),
            "snapshot_count": len(self.snapshots),
            "delegation_enabled": self.delegation_enabled,
            "active_delegations": sum(1 for d in self.delegation.values() if d != ""),
        }


    def _generate_tx_hash(self, from_addr: str, to_addr: str, amount: int) -> str:
        """Generate transaction hash"""
        tx_data = f"{from_addr}{to_addr}{amount}{datetime.now().timestamp()}"
        return "0x" + hashlib.sha256(tx_data.encode()).hexdigest()[:40]
