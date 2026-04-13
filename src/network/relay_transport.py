"""
Synthos Relay Transport — HTTP-based peer protocol for Python agents.

Connects Python agents to the same network as Cloudflare Workers validators,
Go nodes, and mobile PWA validators. Uses the shared peer registry for
discovery and HTTP gossip for message delivery.

Usage:
    transport = RelayTransport(
        registry_url="https://synthos-peer-registry.example.workers.dev",
        self_name="synthos-py-agent-1",
        self_url="https://my-agent.example.com",
    )
    await transport.start()

    # Send to a specific peer
    await transport.send_to_agent("synthos-validator-11", payload)

    # Broadcast to all peers
    await transport.broadcast("block_proposal", payload)

    # Receive inbound messages (call from your HTTP server handler)
    transport.deliver_inbound(from_agent, raw_bytes)
"""

import asyncio
import json
import logging
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional
from urllib.parse import urljoin

try:
    import aiohttp  # type: ignore[import-unresolved]
    _HAS_AIOHTTP = True
except ImportError:
    _HAS_AIOHTTP = False

try:
    from urllib import request as urllib_request
    from urllib.error import URLError
except ImportError:
    pass

logger = logging.getLogger("synthos.relay")


@dataclass
class RelayPeer:
    name: str
    url: str
    cloud: str = "unknown"
    last_seen: float = 0.0


class RelayTransport:
    """HTTP-based relay transport compatible with the Synthos peer protocol."""

    def __init__(
        self,
        registry_url: str = "",
        self_name: str = "",
        self_url: str = "",
        registry_secret: str = "",
        cloud: str = "python-agent",
        heartbeat_interval: float = 30.0,
    ):
        self.registry_url = registry_url.rstrip("/")
        self.self_name = self_name
        self.self_url = self_url
        self.registry_secret = registry_secret
        self.cloud = cloud
        self.heartbeat_interval = heartbeat_interval

        self._peers: List[RelayPeer] = []
        self._validator_order: List[str] = []
        self._agent_handler: Optional[Callable] = None
        self._topic_handlers: Dict[str, Callable] = {}
        self._started = False
        self._heartbeat_task: Optional[asyncio.Task] = None
        self._session: Optional[Any] = None  # aiohttp.ClientSession

    @property
    def peers(self) -> List[RelayPeer]:
        return list(self._peers)

    @property
    def validator_order(self) -> List[str]:
        return list(self._validator_order)

    async def start(self) -> None:
        """Register with peer registry and start heartbeat loop."""
        if self._started:
            return
        self._started = True

        if _HAS_AIOHTTP:
            self._session = aiohttp.ClientSession(
                timeout=aiohttp.ClientTimeout(total=10)
            )

        await self._register_self()
        await self._refresh_peers()

        self._heartbeat_task = asyncio.create_task(self._heartbeat_loop())

    async def close(self) -> None:
        """Stop heartbeat and clean up."""
        self._started = False
        if self._heartbeat_task:
            self._heartbeat_task.cancel()
            try:
                await self._heartbeat_task
            except asyncio.CancelledError:
                pass
        if self._session:
            await self._session.close()
            self._session = None

    def on_agent_message(self, handler: Callable) -> None:
        """Register handler for direct agent-to-agent messages."""
        self._agent_handler = handler

    def on_topic_message(self, topic: str, handler: Callable) -> None:
        """Register handler for messages on a topic."""
        self._topic_handlers[topic] = handler

    async def send_to_agent(self, agent_id: str, payload: bytes) -> bool:
        """Send a message to a specific agent by name."""
        target_url = None
        for p in self._peers:
            if p.name == agent_id:
                target_url = p.url
                break

        if not target_url:
            logger.warning("Peer %s not found in registry", agent_id)
            return False

        return await self._post_gossip(target_url, payload)

    async def broadcast(self, topic: str, payload: bytes) -> int:
        """Broadcast to all active peers. Returns count of successful sends."""
        sent = 0
        for p in self._peers:
            if p.name == self.self_name:
                continue
            if await self._post_gossip(p.url, payload):
                sent += 1
        return sent

    def deliver_inbound(self, from_agent: str, payload: bytes) -> None:
        """Dispatch an inbound message to registered handlers.
        Call this from your HTTP server when receiving POST /gossip/*."""
        try:
            data = json.loads(payload)
            topic = data.get("topic", "")
            from_id = data.get("from_agent", from_agent)
        except (json.JSONDecodeError, AttributeError):
            topic = ""
            from_id = from_agent

        if topic and topic in self._topic_handlers:
            self._topic_handlers[topic](from_id, payload)
            return

        if self._agent_handler:
            self._agent_handler(from_id, payload)

    # ─── Internal ────────────────────────────────────────────────────────

    async def _heartbeat_loop(self) -> None:
        while self._started:
            try:
                await asyncio.sleep(self.heartbeat_interval)
                await self._register_self()
                await self._refresh_peers()
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error("Heartbeat error: %s", e)

    async def _register_self(self) -> None:
        if not self.registry_url or not self.self_name:
            return

        body = json.dumps({
            "name": self.self_name,
            "url": self.self_url,
            "cloud": self.cloud,
        }).encode()

        headers = {"Content-Type": "application/json"}
        if self.registry_secret:
            headers["X-Registry-Secret"] = self.registry_secret

        try:
            if self._session:
                async with self._session.post(
                    f"{self.registry_url}/register",
                    data=body,
                    headers=headers,
                ) as resp:
                    if resp.status == 200:
                        logger.info("Registered as %s", self.self_name)
            else:
                await self._sync_post(
                    f"{self.registry_url}/register", body, headers
                )
        except Exception as e:
            logger.warning("Registration failed: %s", e)

    async def _refresh_peers(self) -> None:
        if not self.registry_url:
            return

        try:
            if self._session:
                async with self._session.get(
                    f"{self.registry_url}/peers/active"
                ) as resp:
                    if resp.status != 200:
                        return
                    data = await resp.json()
            else:
                data = await self._sync_get(
                    f"{self.registry_url}/peers/active"
                )
                if not data:
                    return

            peers_data = data.get("peers", [])
            self._peers = [
                RelayPeer(
                    name=p.get("name", ""),
                    url=p.get("url", ""),
                    cloud=p.get("cloud", "unknown"),
                    last_seen=p.get("last_seen", 0),
                )
                for p in peers_data
            ]
            self._validator_order = data.get("validator_order", [])
            logger.info(
                "Refreshed %d peers, order: %s",
                len(self._peers),
                self._validator_order,
            )
        except Exception as e:
            logger.warning("Peer refresh failed: %s", e)

    async def _post_gossip(self, peer_url: str, payload: bytes) -> bool:
        """POST a gossip message to a peer's /gossip/block endpoint."""
        # Determine endpoint from message type.
        endpoint = "/gossip/block"
        try:
            data = json.loads(payload)
            if data.get("message_type") == "transaction":
                endpoint = "/gossip/tx-batch"
        except (json.JSONDecodeError, AttributeError):
            pass

        url = peer_url.rstrip("/") + endpoint
        headers = {
            "Content-Type": "application/json",
            "X-Gossip": "true",
            "X-From-Agent": self.self_name,
        }

        try:
            if self._session:
                async with self._session.post(
                    url, data=payload, headers=headers
                ) as resp:
                    return resp.status < 400
            else:
                await self._sync_post(url, payload, headers)
                return True
        except Exception as e:
            logger.warning("Gossip to %s failed: %s", peer_url, e)
            return False

    # Fallback sync HTTP for environments without aiohttp.
    async def _sync_post(
        self, url: str, body: bytes, headers: Dict[str, str]
    ) -> None:
        """Blocking HTTP POST fallback using urllib."""
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(
            None, self._do_sync_post, url, body, headers
        )

    @staticmethod
    def _do_sync_post(
        url: str, body: bytes, headers: Dict[str, str]
    ) -> None:
        req = urllib_request.Request(
            url, data=body, headers=headers, method="POST"
        )
        try:
            with urllib_request.urlopen(req, timeout=8) as resp:
                resp.read()
        except URLError as e:
            logger.warning("sync POST to %s failed: %s", url, e)

    async def _sync_get(self, url: str) -> Optional[Dict]:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(None, self._do_sync_get, url)

    @staticmethod
    def _do_sync_get(url: str) -> Optional[Dict]:
        req = urllib_request.Request(url, method="GET")
        try:
            with urllib_request.urlopen(req, timeout=8) as resp:
                return json.loads(resp.read())
        except Exception:
            return None
