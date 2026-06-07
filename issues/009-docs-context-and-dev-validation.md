# Docs, Context, And Dev Validation

Updated domain language and README auth documentation.

Acceptance:
- `CONTEXT.md` defines User, Session, Onboarding Token, and Agent Config.
- README documents first-user onboarding and the agent config encryption key.
- Validation commands run: `make build-web` and `go test ./...`.
- Dev deploy validation should run after review: `make dev-deploy-manager` and `make dev-deploy`.
