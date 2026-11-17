# Contributing to homectl

Open an issue for bugs and feature proposals. Include the homectl version,
platform, and a minimal reproduction. Redact credentials and private host details.

For pull requests, explain the behavior change and how you verified it. Run
`go test ./...` for Go changes and `npm ci && npm run build` in `web/` for
dashboard changes. Test changes to manager or agent behavior on a disposable
cluster using the [quickstart](docs/quickstart.md).

Report security issues privately as described in [SECURITY.md](SECURITY.md).
