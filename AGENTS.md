# Working on homectl

Homectl manages Docker Compose applications with a Go manager, Go agents, and a
React dashboard. Keep public examples portable and use disposable hosts for
integration testing.

- Run `go test ./...` for Go changes.
- Run `npm ci && npm run build` in `web/` for dashboard changes.
- Follow the [quickstart](docs/quickstart.md) on a disposable manager and agent
  when changing manager or agent behavior.
- Never commit credentials, live inventory, or production configuration.
