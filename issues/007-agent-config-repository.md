# Agent Config Repository

Implemented encrypted per-agent config storage.

Acceptance:
- Each agent has one opaque JSON config document.
- Config is encrypted at rest with AES-GCM and `HOMECTL_CONFIG_ENCRYPTION_KEY`.
- Stored ciphertext differs from plaintext.
- Reads decrypt to the original JSON and include version metadata.
