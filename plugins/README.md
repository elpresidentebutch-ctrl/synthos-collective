# SYNTHOS Agent Plugin System

This folder contains example plugins and documentation for extending agent capabilities off-chain.

## How Plugins Work

- Plugins are loaded by the agent runtime (JS/TS for mobile/web, Go for backend/server).
- Each plugin exposes a standard interface (see below) and registers itself at startup.
- Plugins can add new agent actions, APIs, or background tasks.
- The agent advertises all loaded plugin capabilities via a `/capabilities` endpoint or UI tab.

## Example Plugin Types

- `negotiation` — Automated deal-making, contract negotiation, and agreement protocols
- `logistics` — Supply chain, delivery, and resource management
- `research` — Web/API search, data gathering, and analysis
- `software_builder` — Code generation, CI/CD, and deployment
- `business_manager` — Invoicing, payments, and business process automation
- `coordination` — Multi-agent scheduling, task assignment, and collaboration
- `24_7_operator` — Persistent background jobs, monitoring, and alerting

## Adding a Plugin

- JS/TS: Place your plugin in `plugins/js/` and export a `register(agent)` function.
- Go: Place your plugin in `plugins/go/` and implement the `AgentPlugin` interface.

See the `js/` and `go/` subfolders for stubs and examples.
