-- gateway/migrations/000004_users.up.sql
-- users: core user identity, system roles, encrypted PII column placeholders, and key references.
-- user_keys: Ed25519 signing + X25519 encryption keypairs per user (two rows per user).

CREATE TABLE users (
    user_id                   TEXT    PRIMARY KEY,
    display_name_encrypted    BYTEA,
    display_name_nonce        BYTEA,
    avatar_url_encrypted      BYTEA,
    avatar_url_nonce          BYTEA,
    system_role               TEXT    NOT NULL DEFAULT 'user',
    is_active                 BOOLEAN NOT NULL DEFAULT true,
    signing_key_id            TEXT,
    encryption_key_id         TEXT,
    created_at                BIGINT  NOT NULL,
    last_seen_at              BIGINT
);

ALTER TABLE users
    ADD CONSTRAINT users_system_role_check
    CHECK (system_role IN ('user', 'instance_admin', 'compliance_officer'));

CREATE TABLE user_keys (
    key_id      TEXT   PRIMARY KEY,
    user_id     TEXT   NOT NULL REFERENCES users(user_id),
    key_type    TEXT   NOT NULL,
    algorithm   TEXT   NOT NULL,
    public_key  BYTEA  NOT NULL,
    private_key BYTEA,
    created_at  BIGINT NOT NULL,
    deleted_at  BIGINT
);

ALTER TABLE user_keys
    ADD CONSTRAINT user_keys_key_type_check
    CHECK (key_type IN ('signing', 'encryption'));
