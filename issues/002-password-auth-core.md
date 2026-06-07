# Password Auth Core

Implemented local username/password auth using Argon2id password hashes and opaque server-side sessions.

Acceptance:
- Password hashes include Argon2id parameters and per-user salt.
- Login creates a server-side session and secure cookie.
- Logout deletes the session and expires the cookie.
- Invalid credentials and logged-out sessions fail authentication.
