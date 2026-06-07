# Runtime Startup Gate

Refactored manager startup so runtime services wait until a first user exists.

Acceptance:
- HTTP server starts first so onboarding UI/API are available.
- Before onboarding, WireGuard setup, agent TCP server, app discovery, reconcile loop, CrowdSec bootstrap, public-route loop, WireGuard cleanup, and lease cleanup do not start.
- After onboarding completes, runtime services start exactly once in-process.
- Existing-user restarts start runtime normally.
