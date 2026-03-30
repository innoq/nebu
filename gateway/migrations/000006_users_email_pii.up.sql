-- gateway/migrations/000006_users_email_pii.up.sql
-- Add Sensitive PII (Tier 2) columns for email storage to users table.
-- email is encrypted with the user's X25519 public key via ephemeral ECDH (Story 2.13).
-- Three columns needed: ciphertext+tag, nonce, ephemeral public key (required for ECDH decrypt).

ALTER TABLE users ADD COLUMN email_encrypted     BYTEA;
ALTER TABLE users ADD COLUMN email_nonce         BYTEA;
ALTER TABLE users ADD COLUMN email_ephemeral_pub BYTEA;
