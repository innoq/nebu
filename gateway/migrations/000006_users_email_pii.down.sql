-- gateway/migrations/000006_users_email_pii.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS email_ephemeral_pub;
ALTER TABLE users DROP COLUMN IF EXISTS email_nonce;
ALTER TABLE users DROP COLUMN IF EXISTS email_encrypted;
