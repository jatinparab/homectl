# DB Repositories

Implemented repository interfaces for users, sessions, and agent config, backed by SQLite at `/config/state/homectl.db`.

Acceptance:
- Migrations are idempotent.
- SQLite details stay behind repository interfaces.
- User/session/agent-config repositories are wired into `Manager`.
- `go test ./...` covers repository-backed auth and encrypted config behavior.
