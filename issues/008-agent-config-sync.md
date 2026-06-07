# Agent Config Sync

Implemented periodic agent config sync over the existing manager-agent TCP link.

Acceptance:
- Manager answers `agent_config_sync` requests for the connected agent.
- Agent writes `/config/state/agent-config.json` only when the received version is newer.
- Empty config is a no-op.
- Sync failures are logged and do not block heartbeats or reconciliation.
