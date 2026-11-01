"""
SYNTHOS Smart Contract System - Original Implementation
Deterministic VM for contract execution
"""

from typing import Dict, List, Optional, Any, Tuple
from dataclasses import dataclass
from enum import Enum
import json


class OpCode(Enum):
    """Original contract opcodes"""
    PUSH = "PUSH"          # Push value to stack
    POP = "POP"            # Pop from stack
    ADD = "ADD"            # Addition
    SUB = "SUB"            # Subtraction
    MUL = "MUL"            # Multiplication
    DIV = "DIV"            # Division
    MOD = "MOD"            # Modulo
    EQ = "EQ"              # Equality
    LT = "LT"              # Less than
    GT = "GT"              # Greater than
    AND = "AND"            # Bitwise AND
    OR = "OR"              # Bitwise OR
    NOT = "NOT"            # Bitwise NOT
    LOAD = "LOAD"          # Load from memory
    STORE = "STORE"        # Store to memory
    CALL = "CALL"          # Call function
    RETURN = "RETURN"      # Return from function
    REVERT = "REVERT"      # Revert state changes
    EMIT = "EMIT"          # Emit event
    HALT = "HALT"          # Halt execution


@dataclass
class ContractABI:
    """Contract Application Binary Interface"""
    version: str
    contract_name: str
    functions: List[Dict[str, Any]]
    events: List[Dict[str, Any]]
    state_variables: List[Dict[str, Any]]


class ContractVM:
    """Original Deterministic Contract Virtual Machine"""
    
    def __init__(self, code: List[int], initial_state: Dict[str, Any] = None):
        """
        Initialize VM
        
        Args:
            code: Contract bytecode
            initial_state: Initial state values
        """
        self.code = code
        self.pc = 0  # Program counter
        self.stack = []
        self.memory: Dict[int, int] = {}
        self.state = initial_state or {}
        self.gas_used = 0
        self.execution_trace = []
        self.events = []
        self.halted = False
    
    def execute(self, gas_limit: int = 1000000) -> Tuple[bool, Any]:
        """
        Execute contract code
        
        Returns:
            Tuple of (success, result)
        """
        while self.pc < len(self.code) and self.gas_used < gas_limit and not self.halted:
            opcode_val = self.code[self.pc]
            
            # Gas meter
            self.gas_used += 1
            
            try:
                self._execute_opcode(opcode_val)
            except Exception as e:
                return False, str(e)
            
            self.pc += 1
        
        if self.gas_used >= gas_limit:
            return False, "Out of gas"
        
        result = self.stack[-1] if self.stack else None
        return True, result
    
    def _execute_opcode(self, opcode: int) -> None:
        """Execute single opcode"""
        # Map opcode to operation
        if opcode == OpCode.PUSH.value:
            self.pc += 1
            value = self.code[self.pc] if self.pc < len(self.code) else 0
            self.stack.append(value)
        
        elif opcode == OpCode.POP.value:
            if self.stack:
                self.stack.pop()
        
        elif opcode == OpCode.ADD.value:
            if len(self.stack) >= 2:
                b = self.stack.pop()
                a = self.stack.pop()
                self.stack.append(a + b)
        
        elif opcode == OpCode.SUB.value:
            if len(self.stack) >= 2:
                b = self.stack.pop()
                a = self.stack.pop()
                self.stack.append(a - b)
        
        elif opcode == OpCode.MUL.value:
            if len(self.stack) >= 2:
                b = self.stack.pop()
                a = self.stack.pop()
                self.stack.append(a * b)
        
        elif opcode == OpCode.DIV.value:
            if len(self.stack) >= 2:
                b = self.stack.pop()
                a = self.stack.pop()
                if b != 0:
                    self.stack.append(a // b)
                else:
                    raise Exception("Division by zero")
        
        elif opcode == OpCode.HALT.value:
            self.halted = True
    
    def write_state(self, key: str, value: Any) -> None:
        """Write to contract state"""
        self.state[key] = value
    
    def read_state(self, key: str) -> Any:
        """Read from contract state"""
        return self.state.get(key)
    
    def emit_event(self, event_name: str, data: Dict[str, Any]) -> None:
        """Emit contract event"""
        self.events.append({
            'event': event_name,
            'data': data,
            'gas_used': self.gas_used,
        })


@dataclass
class DeployedContract:
    """Deployed smart contract"""
    contract_id: str
    address: str
    deployer: str
    code: List[int]
    abi: ContractABI
    state: Dict[str, Any]
    block_height: int
    creation_time: float
    compiled_hash: str


class ContractRegistry:
    """Registry of all deployed contracts"""
    
    def __init__(self):
        self.contracts: Dict[str, DeployedContract] = {}
        self.address_map: Dict[str, str] = {}  # address -> contract_id
    
    def deploy_contract(self, contract: DeployedContract) -> Tuple[bool, str]:
        """Deploy new contract"""
        if contract.contract_id in self.contracts:
            return False, "Contract already deployed"
        
        self.contracts[contract.contract_id] = contract
        self.address_map[contract.address] = contract.contract_id
        
        return True, f"Contract deployed: {contract.address}"
    
    def get_contract(self, contract_id: str) -> Optional[DeployedContract]:
        """Get contract by ID"""
        return self.contracts.get(contract_id)
    
    def get_contract_by_address(self, address: str) -> Optional[DeployedContract]:
        """Get contract by address"""
        contract_id = self.address_map.get(address)
        return self.contracts.get(contract_id) if contract_id else None
    
    def call_contract(self, address: str, function: str, args: List[Any]) -> Tuple[bool, Any]:
        """Call contract function"""
        contract = self.get_contract_by_address(address)
        if not contract:
            return False, "Contract not found"
        
        # Execute contract
        vm = ContractVM(contract.code, contract.state)
        success, result = vm.execute()
        
        if success:
            contract.state = vm.state
        
        return success, result


class SmartContractSystem:
    """Complete smart contract system"""
    
    def __init__(self):
        self.registry = ContractRegistry()
        self.call_history: Dict[str, List[Dict]] = {}
    
    def compile_contract(self, source_code: str) -> Tuple[bool, List[int], str]:
        """
        Compile contract source to bytecode
        
        Args:
            source_code: Contract source code
            
        Returns:
            Tuple of (success, bytecode, error_message)
        """
        try:
            # Parse source to opcodes
            lines = source_code.strip().split('\n')
            bytecode = []
            
            for line in lines:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                
                parts = line.split()
                if not parts:
                    continue
                
                opcode_name = parts[0]
                if opcode_name == "PUSH":
                    bytecode.append(OpCode.PUSH.value)
                    if len(parts) > 1:
                        bytecode.append(int(parts[1]))
                elif opcode_name == "ADD":
                    bytecode.append(OpCode.ADD.value)
                elif opcode_name == "SUB":
                    bytecode.append(OpCode.SUB.value)
                elif opcode_name == "MUL":
                    bytecode.append(OpCode.MUL.value)
                elif opcode_name == "HALT":
                    bytecode.append(OpCode.HALT.value)
            
            return True, bytecode, ""
        
        except Exception as e:
            return False, [], str(e)
    
    def deploy_contract(self, source_code: str, contract_name: str, address: str, 
                       deployer: str, block_height: int, creation_time: float) -> Tuple[bool, str]:
        """Deploy contract"""
        # Compile
        success, bytecode, error = self.compile_contract(source_code)
        if not success:
            return False, f"Compilation failed: {error}"
        
        # Create contract object
        abi = ContractABI(
            version="1.0",
            contract_name=contract_name,
            functions=[],
            events=[],
            state_variables=[]
        )
        
        import hashlib
        compiled_hash = hashlib.sha256(str(bytecode).encode()).hexdigest()
        
        contract = DeployedContract(
            contract_id=f"contract_{address}",
            address=address,
            deployer=deployer,
            code=bytecode,
            abi=abi,
            state={},
            block_height=block_height,
            creation_time=creation_time,
            compiled_hash=compiled_hash,
        )
        
        # Deploy
        return self.registry.deploy_contract(contract)
    
    def call_contract(self, address: str, function: str, args: List[Any],
                     caller: str) -> Tuple[bool, Any]:
        """Call contract function"""
        success, result = self.registry.call_contract(address, function, args)
        
        # Record call
        if address not in self.call_history:
            self.call_history[address] = []
        
        self.call_history[address].append({
            'caller': caller,
            'function': function,
            'success': success,
            'result': result,
        })
        
        return success, result
    
    def get_contract_state(self, address: str) -> Optional[Dict]:
        """Get contract state"""
        contract = self.registry.get_contract_by_address(address)
        return contract.state if contract else None
    
    def get_call_history(self, address: str, limit: int = 100) -> List[Dict]:
        """Get contract call history"""
        history = self.call_history.get(address, [])
        return history[-limit:]


if __name__ == "__main__":
    print("SYNTHOS Smart Contract System Test")
    print("=" * 50)
    
    system = SmartContractSystem()
    
    # Simple contract
    contract_code = """
    PUSH 10
    PUSH 5
    ADD
    HALT
    """
    
    # Compile
    success, bytecode, error = system.compile_contract(contract_code)
    print(f"✓ Contract compiled: {success}")
    
    # Deploy
    success, msg = system.deploy_contract(
        contract_code,
        "SimpleContract",
        "0xabcd",
        "alice",
        100,
        1234567.0
    )
    print(f"✓ Contract deployed: {msg}")
    
    # Call
    success, result = system.call_contract("0xabcd", "add", [], "bob")
    print(f"✓ Contract called: result={result}")
