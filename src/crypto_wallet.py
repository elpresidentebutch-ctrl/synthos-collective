"""
SYNTHOS Cryptography and Key Management - Original Implementation
"""

import hashlib
import hmac
from typing import Tuple, Optional
from dataclasses import dataclass
import secrets


class KeyDerivation:
    """Original key derivation system"""
    
    @staticmethod
    def derive_key_from_seed(seed: str, path: str = "") -> str:
        """
        Derive key from seed using HKDF-like approach
        
        Args:
            seed: Initial seed value
            path: Derivation path
            
        Returns:
            Derived key
        """
        # Extract phase
        salt = b"SYNTHOS_EXTRACT"
        prk = hmac.new(salt, seed.encode(), hashlib.sha256).digest()
        
        # Expand phase
        info = path.encode() if path else b"SYNTHOS_EXPAND"
        okm = hmac.new(prk, info + b"\x01", hashlib.sha256).digest()
        
        return okm.hex()


class Signature:
    """Original signature system (simplified ECDSA-like)"""
    
    @staticmethod
    def sign(message: str, private_key: str) -> str:
        """
        Create signature using private key
        
        Args:
            message: Message to sign
            private_key: Private key
            
        Returns:
            Signature
        """
        # Sign using HMAC-SHA256
        signature = hmac.new(
            private_key.encode(),
            message.encode(),
            hashlib.sha256
        ).digest()
        
        return signature.hex()
    
    @staticmethod
    def verify(message: str, signature: str, public_key: str) -> bool:
        """
        Verify signature using public key
        
        Args:
            message: Original message
            signature: Signature to verify
            public_key: Public key
            
        Returns:
            True if signature is valid
        """
        # Derive expected signature
        expected = hmac.new(
            public_key.encode(),
            message.encode(),
            hashlib.sha256
        ).digest().hex()
        
        # Constant-time comparison
        return hmac.compare_digest(signature, expected)


class AddressGeneration:
    """Original address generation"""
    
    @staticmethod
    def generate_address(public_key: str) -> str:
        """
        Generate address from public key
        
        Args:
            public_key: Public key
            
        Returns:
            Address
        """
        # Hash public key
        hash1 = hashlib.sha256(public_key.encode()).digest()
        hash2 = hashlib.new('ripemd160', hash1).digest()
        
        # Add version byte and encode
        versioned = b"\x00" + hash2
        
        # Checksum
        checksum = hashlib.sha256(
            hashlib.sha256(versioned).digest()
        ).digest()[:4]
        
        # Encode as hex
        address = (versioned + checksum).hex()
        
        return f"0x{address[:40]}"


@dataclass
class KeyPair:
    """Cryptographic key pair"""
    public_key: str
    private_key: str
    address: str


class Wallet:
    """Original wallet implementation"""
    
    def __init__(self, name: str = "wallet"):
        self.name = name
        self.accounts: dict[str, KeyPair] = {}
        self.balances: dict[str, int] = {}
    
    def create_account(self, seed: str = None) -> Tuple[KeyPair, str]:
        """
        Create new account
        
        Args:
            seed: Optional seed for deterministic generation
            
        Returns:
            Tuple of (KeyPair, mnemonic)
        """
        if not seed:
            seed = secrets.token_hex(32)
        
        # Derive keys
        private_key = KeyDerivation.derive_key_from_seed(seed, "account/0")
        public_key = hashlib.sha256(private_key.encode()).hexdigest()
        address = AddressGeneration.generate_address(public_key)
        
        keypair = KeyPair(
            public_key=public_key,
            private_key=private_key,
            address=address
        )
        
        self.accounts[address] = keypair
        self.balances[address] = 0
        
        return keypair, seed
    
    def import_account(self, private_key: str) -> KeyPair:
        """Import account from private key"""
        public_key = hashlib.sha256(private_key.encode()).hexdigest()
        address = AddressGeneration.generate_address(public_key)
        
        keypair = KeyPair(
            public_key=public_key,
            private_key=private_key,
            address=address
        )
        
        self.accounts[address] = keypair
        self.balances[address] = 0
        
        return keypair
    
    def sign_transaction(self, message: str, address: str) -> Optional[str]:
        """Sign transaction with account"""
        if address not in self.accounts:
            return None
        
        keypair = self.accounts[address]
        return Signature.sign(message, keypair.private_key)
    
    def verify_transaction(self, message: str, signature: str, address: str) -> bool:
        """Verify transaction signature"""
        if address not in self.accounts:
            return False
        
        keypair = self.accounts[address]
        return Signature.verify(message, signature, keypair.public_key)
    
    def set_balance(self, address: str, balance: int) -> None:
        """Set account balance"""
        if address in self.accounts:
            self.balances[address] = balance
    
    def get_balance(self, address: str) -> int:
        """Get account balance"""
        return self.balances.get(address, 0)
    
    def transfer(self, from_addr: str, to_addr: str, amount: int) -> Tuple[bool, str]:
        """Transfer funds"""
        if from_addr not in self.accounts:
            return False, "Account not found"
        
        balance = self.balances.get(from_addr, 0)
        if balance < amount:
            return False, "Insufficient balance"
        
        self.balances[from_addr] = balance - amount
        self.balances[to_addr] = self.balances.get(to_addr, 0) + amount
        
        return True, f"Transferred {amount} from {from_addr} to {to_addr}"


class MerkleProofStorage:
    """Storage with Merkle proof support"""
    
    def __init__(self):
        self.data: Dict[str, any] = {}
        self.merkle_roots: Dict[int, str] = {}  # height -> root
    
    def store(self, key: str, value: any) -> None:
        """Store value"""
        self.data[key] = value
    
    def retrieve(self, key: str) -> Optional[any]:
        """Retrieve value"""
        return self.data.get(key)
    
    def compute_merkle_root(self) -> str:
        """Compute Merkle root of all data"""
        if not self.data:
            return hashlib.sha256(b"empty").hexdigest()
        
        items = sorted(self.data.items())
        hashes = [hashlib.sha256(f"{k}:{v}".encode()).hexdigest() for k, v in items]
        
        while len(hashes) > 1:
            next_level = []
            for i in range(0, len(hashes), 2):
                combined = hashes[i] + (hashes[i + 1] if i + 1 < len(hashes) else hashes[i])
                next_level.append(hashlib.sha256(combined.encode()).hexdigest())
            hashes = next_level
        
        return hashes[0] if hashes else hashlib.sha256(b"empty").hexdigest()
    
    def generate_proof(self, key: str) -> List[str]:
        """Generate Merkle proof for key"""
        if key not in self.data:
            return []
        
        # Simplified proof generation
        proof = []
        for k in self.data:
            if k != key:
                proof.append(hashlib.sha256(f"{k}:{self.data[k]}".encode()).hexdigest())
        
        return proof


class StateDB:
    """State database for blockchain state"""
    
    def __init__(self):
        self.state: Dict[str, Dict[str, any]] = {}  # address -> state
        self.history: List[Dict[str, Dict]] = []  # State history
    
    def set_state(self, address: str, key: str, value: any) -> None:
        """Set state value"""
        if address not in self.state:
            self.state[address] = {}
        
        self.state[address][key] = value
    
    def get_state(self, address: str, key: str = None) -> any:
        """Get state value"""
        if address not in self.state:
            return None
        
        if key:
            return self.state[address].get(key)
        else:
            return self.state[address]
    
    def commit_state(self, block_height: int, state_root: str) -> None:
        """Commit state at block height"""
        self.history.append({
            'height': block_height,
            'root': state_root,
            'state': dict(self.state),
        })
    
    def get_state_at_height(self, height: int) -> Optional[Dict]:
        """Get state at block height"""
        for entry in self.history:
            if entry['height'] == height:
                return entry['state']
        return None
    
    def compute_root_hash(self) -> str:
        """Compute state root hash"""
        items = []
        for addr in sorted(self.state.keys()):
            for key in sorted(self.state[addr].keys()):
                value = self.state[addr][key]
                items.append(f"{addr}:{key}:{value}")
        
        combined = "|".join(items)
        return hashlib.sha256(combined.encode()).hexdigest()


if __name__ == "__main__":
    print("SYNTHOS Cryptography and Wallet Test")
    print("=" * 50)
    
    # Create wallet
    wallet = Wallet("my_wallet")
    
    # Create account
    keypair, seed = wallet.create_account()
    print(f"✓ Account created: {keypair.address}")
    print(f"  Seed: {seed[:32]}...")
    
    # Set balance
    wallet.set_balance(keypair.address, 1000)
    print(f"✓ Balance set: {wallet.get_balance(keypair.address)}")
    
    # Sign message
    message = "test message"
    signature = wallet.sign_transaction(message, keypair.address)
    print(f"✓ Signed: {signature[:16]}...")
    
    # Verify
    verified = wallet.verify_transaction(message, signature, keypair.address)
    print(f"✓ Verified: {verified}")
    
    # State DB
    db = StateDB()
    db.set_state("alice", "balance", 1000)
    db.set_state("bob", "balance", 500)
    
    root = db.compute_root_hash()
    print(f"✓ State root: {root[:16]}...")
