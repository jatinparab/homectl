# Auth Middleware

Implemented session auth middleware for manager HTTP routes.

Acceptance:
- `/health` remains public.
- `/api/join` remains session-exempt and continues using `HOMECTL_JOIN_TOKEN`.
- `/api/v1/*`, `/agents/*`, `/docker/sync`, and `/sync` require a valid user session when accessed over HTTP.
- Internal manager-agent TCP handler dispatch bypasses HTTP middleware without weakening join-token auth.
