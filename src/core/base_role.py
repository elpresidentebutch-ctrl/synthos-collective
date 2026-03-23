"""Base Role Interface for SYNTHOS Agents"""

from abc import ABC, abstractmethod
from typing import Any, Dict
from enum import Enum


class RoleStatus(Enum):
    """Role operational status"""
    INITIALIZED = "initialized"
    RUNNING = "running"
    PAUSED = "paused"
    STOPPED = "stopped"
    ERROR = "error"


class Role(ABC):
    """Abstract base class for all SYNTHOS Agent roles"""

    def __init__(self, agent):
        """
        Initialize a role.
        
        Args:
            agent: Reference to parent SyntHOSAgent instance
        """
        self.agent = agent
        self.name = self.__class__.__name__
        self.version = "1.0.0"
        self.enabled = True
        self.status = RoleStatus.INITIALIZED
        self._state: Dict[str, Any] = {}

    async def initialize(self) -> None:
        """Initialize role resources and setup"""
        self.status = RoleStatus.RUNNING

    async def execute(self) -> None:
        """Main role execution loop"""
        pass

    async def finalize(self) -> None:
        """Cleanup role resources"""
        self.status = RoleStatus.STOPPED

    async def on_event(self, event) -> None:
        """
        Handle incoming events
        
        Args:
            event: Event object to process
        """
        if self.enabled:
            await self.process_event(event)

    async def process_event(self, event) -> None:
        """
        Process specific event types
        
        Args:
            event: Event object to process
        """
        pass

    def get_state(self) -> Dict[str, Any]:
        """
        Get role state snapshot
        
        Returns:
            Dictionary containing role state
        """
        return {
            'name': self.name,
            'version': self.version,
            'enabled': self.enabled,
            'status': self.status.value,
            'state': self._state.copy(),
        }

    def set_enabled(self, enabled: bool) -> None:
        """Enable or disable role"""
        self.enabled = enabled

    async def handle_error(self, error: Exception) -> None:
        """
        Handle errors that occur during role execution
        
        Args:
            error: The exception that occurred
        """
        self.status = RoleStatus.ERROR
        self.agent.logger.error(
            f"Error in {self.name}: {error}",
            extra={'role': self.name, 'error_type': type(error).__name__}
        )
