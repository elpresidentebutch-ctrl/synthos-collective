"""Utilities for SYNTHOS Agents"""

import logging
from typing import Dict, Any


def setup_logger(name: str, level: str = "INFO") -> logging.Logger:
    """Setup logger for SYNTHOS components"""
    logger = logging.getLogger(name)
    logger.setLevel(logging.getLevelName(level))
    
    handler = logging.StreamHandler()
    formatter = logging.Formatter(
        '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )
    handler.setFormatter(formatter)
    logger.addHandler(handler)
    
    return logger


def merge_configs(base: Dict[str, Any], overrides: Dict[str, Any]) -> Dict[str, Any]:
    """Merge configuration dictionaries"""
    merged = base.copy()
    merged.update(overrides)
    return merged
