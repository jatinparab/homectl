# Web Login And Onboarding UI

Implemented React auth gating for onboarding, login, logout, and the existing app shell.

Acceptance:
- UI loads auth status from `/api/v1/auth/me`.
- No-user state shows onboarding form with temporary token, username, password, and confirmation.
- Created first user is not auto-logged-in; login form is shown afterward.
- Authenticated users see the existing dashboard shell and can log out.
