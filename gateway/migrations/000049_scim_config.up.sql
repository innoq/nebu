-- Story 14-3c — SCIM 2.0 User Fetch + Progress Tracking
-- Adds three server_config rows for SCIM 2.0 integration (Protocol B per ADR-015):
--   scim_enabled        — feature flag, default 'false'
--   scim_base_url       — HTTPS URL of the SCIM /Users endpoint, default ''
--   scim_bearer_token   — AES-256-GCM encrypted bearer token, default ''
--
-- The server_config table is a key-value store (key TEXT PRIMARY KEY, value TEXT NOT NULL).
-- New config values are inserted as rows (not columns).
-- ON CONFLICT DO NOTHING: safe to run on an instance where these rows already exist.
--
-- Security (CR-1): scim_bearer_token is stored AES-256-GCM encrypted using the
-- gateway internal secret. The raw token is NEVER stored in plaintext.
-- The gateway returns only scim_bearer_token_set: bool in API responses — never the value.
--
-- The existing RLS policy `config_update_mutable` from migration 000048 restricts UPDATE
-- to an explicit allowlist. This migration extends that list to include the three new keys
-- so that PATCH /api/v1/admin/config can upsert them as the nebu_app role.

INSERT INTO server_config (key, value, set_at) VALUES
    ('scim_enabled',       'false', (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
    ('scim_base_url',      '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
    ('scim_bearer_token',  '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint)
ON CONFLICT (key) DO NOTHING;

-- Drop the existing mutable-key policy from migration 000048 and recreate it with the
-- three new SCIM keys added to the allowlist.
DROP POLICY IF EXISTS config_update_mutable ON server_config;

CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret',
        'oidc_directory_enabled',
        'oidc_directory_endpoint',
        'scim_enabled',
        'scim_base_url',
        'scim_bearer_token'
    ))
    WITH CHECK (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret',
        'oidc_directory_enabled',
        'oidc_directory_endpoint',
        'scim_enabled',
        'scim_base_url',
        'scim_bearer_token'
    ));
