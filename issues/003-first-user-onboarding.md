# First User Onboarding

Implemented first-user onboarding for managers with no local users.

Acceptance:
- Manager generates and logs a temporary onboarding token when no user exists.
- First-user creation requires token, username, password, and password confirmation.
- Onboarding token is memory-only and invalidated after first user creation.
- First-user creation does not create a logged-in session; the admin must log in again.
