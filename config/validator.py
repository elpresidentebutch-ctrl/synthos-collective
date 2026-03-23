"""Configuration validation for SYNTHOS Collective"""

from src.core import AgentConfig
from typing import Dict, List, Tuple


class ConfigValidator:
    """Validates SYNTHOS Agent configurations"""
    
    # Valid networks
    VALID_NETWORKS = ["mainnet", "testnet", "devnet", "local"]
    
    # Valid log levels
    VALID_LOG_LEVELS = ["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"]
    
    # Configuration constraints
    CONSTRAINTS = {
        "id": {
            "min_length": 1,
            "max_length": 256,
            "pattern": r"^[a-zA-Z0-9\-_]+$"
        },
        "consensus_timeout_ms": {
            "min_value": 100,
            "max_value": 60000,
        },
        "max_peers": {
            "min_value": 1,
            "max_value": 10000,
        }
    }
    
    @staticmethod
    def validate_config(config: AgentConfig) -> Tuple[bool, List[str]]:
        """
        Validate agent configuration
        
        Args:
            config: AgentConfig to validate
            
        Returns:
            Tuple of (is_valid, error_messages)
        """
        errors = []
        
        # Validate ID
        if not config.id or len(config.id) == 0:
            errors.append("Agent ID cannot be empty")
        elif len(config.id) > ConfigValidator.CONSTRAINTS["id"]["max_length"]:
            errors.append(f"Agent ID exceeds max length of {ConfigValidator.CONSTRAINTS['id']['max_length']}")
        
        # Validate network
        if config.network not in ConfigValidator.VALID_NETWORKS:
            errors.append(f"Network must be one of: {', '.join(ConfigValidator.VALID_NETWORKS)}")
        
        # Validate log level
        if config.log_level not in ConfigValidator.VALID_LOG_LEVELS:
            errors.append(f"Log level must be one of: {', '.join(ConfigValidator.VALID_LOG_LEVELS)}")
        
        # Validate consensus timeout
        min_timeout = ConfigValidator.CONSTRAINTS["consensus_timeout_ms"]["min_value"]
        max_timeout = ConfigValidator.CONSTRAINTS["consensus_timeout_ms"]["max_value"]
        
        if config.consensus_timeout_ms < min_timeout:
            errors.append(f"Consensus timeout must be at least {min_timeout}ms")
        elif config.consensus_timeout_ms > max_timeout:
            errors.append(f"Consensus timeout cannot exceed {max_timeout}ms")
        
        # Validate max peers
        min_peers = ConfigValidator.CONSTRAINTS["max_peers"]["min_value"]
        max_peers = ConfigValidator.CONSTRAINTS["max_peers"]["max_value"]
        
        if config.max_peers < min_peers:
            errors.append(f"Max peers must be at least {min_peers}")
        elif config.max_peers > max_peers:
            errors.append(f"Max peers cannot exceed {max_peers}")
        
        # Validate storage path
        if not config.storage_path or len(config.storage_path) == 0:
            errors.append("Storage path cannot be empty")
        
        return len(errors) == 0, errors
    
    @staticmethod
    def get_recommended_config(network: str = "testnet") -> AgentConfig:
        """
        Get recommended configuration for network
        
        Args:
            network: Network type
            
        Returns:
            Recommended AgentConfig
        """
        configs = {
            "mainnet": AgentConfig(
                id="agent-mainnet-001",
                network="mainnet",
                log_level="INFO",
                consensus_timeout_ms=4000,
                max_peers=100,
                storage_path="./data/mainnet"
            ),
            "testnet": AgentConfig(
                id="agent-testnet-001",
                network="testnet",
                log_level="DEBUG",
                consensus_timeout_ms=5000,
                max_peers=50,
                storage_path="./data/testnet"
            ),
            "devnet": AgentConfig(
                id="agent-devnet-001",
                network="devnet",
                log_level="DEBUG",
                consensus_timeout_ms=2000,
                max_peers=10,
                storage_path="./data/devnet"
            ),
            "local": AgentConfig(
                id="agent-local-001",
                network="local",
                log_level="DEBUG",
                consensus_timeout_ms=1000,
                max_peers=5,
                storage_path="./data/local"
            ),
        }
        
        return configs.get(network, configs["testnet"])
    
    @staticmethod
    def print_validation_report(config: AgentConfig) -> None:
        """
        Print detailed validation report
        
        Args:
            config: AgentConfig to validate
        """
        is_valid, errors = ConfigValidator.validate_config(config)
        
        print("\n" + "=" * 60)
        print("SYNTHOS Agent Configuration Validation Report")
        print("=" * 60)
        
        print(f"\nConfiguration Details:")
        print(f"  Agent ID: {config.id}")
        print(f"  Network: {config.network}")
        print(f"  Log Level: {config.log_level}")
        print(f"  Consensus Timeout: {config.consensus_timeout_ms}ms")
        print(f"  Max Peers: {config.max_peers}")
        print(f"  Storage Path: {config.storage_path}")
        
        print(f"\nValidation Result: {'✓ VALID' if is_valid else '✗ INVALID'}")
        
        if errors:
            print(f"\nErrors ({len(errors)}):")
            for i, error in enumerate(errors, 1):
                print(f"  {i}. {error}")
        else:
            print("\nNo validation errors detected.")
        
        print("\n" + "=" * 60)


if __name__ == "__main__":
    # Test with various configurations
    print("Testing Configuration Validation System...\n")
    
    # Test 1: Valid mainnet config
    mainnet_config = AgentConfig(
        id="main-agent",
        network="mainnet",
        consensus_timeout_ms=4000,
    )
    ConfigValidator.print_validation_report(mainnet_config)
    
    # Test 2: Valid testnet config
    testnet_config = AgentConfig(
        id="test-agent",
        network="testnet",
        log_level="DEBUG",
    )
    ConfigValidator.print_validation_report(testnet_config)
    
    # Test 3: Invalid config (bad network)
    invalid_config = AgentConfig(
        id="bad-agent",
        network="badnet",
    )
    ConfigValidator.print_validation_report(invalid_config)
    
    # Test 4: Recommended configs
    print("\n" + "=" * 60)
    print("Recommended Configurations")
    print("=" * 60)
    
    for network in ["mainnet", "testnet", "devnet", "local"]:
        config = ConfigValidator.get_recommended_config(network)
        ConfigValidator.print_validation_report(config)
