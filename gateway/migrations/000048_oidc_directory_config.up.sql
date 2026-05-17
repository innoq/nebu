-- Story 14-2a — OIDC Directory Integration Config
-- Adds two server_config rows for OIDC directory integration (Protocol A):
--   oidc_directory_enabled  — feature flag, default 'false'
--   oidc_directory_endpoint — URL of the OIDC provider's user-search endpoint, default ''
--
-- The server_config table is a key-value store (key TEXT PRIMARY KEY, value TEXT NOT NULL).
-- New config values are inserted as rows (not columns).
-- ON CONFLICT DO NOTHING: safe to run on an instance where these rows already exist.
--
-- The existing RLS policy `config_update_mutable` from migration 000046 restricts UPDATE
-- to an explicit allowlist. This migration extends that list to include the two new keys
-- so that PATCH /api/v1/admin/config can upsert them as the nebu_app role.

INSERT INTO server_config (key, value, set_at) VALUES
    ('oidc_directory_enabled',  'false', (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint),
    ('oidc_directory_endpoint', '',      (EXTRACT(EPOCH FROM NOW()) * 1000)::bigint)
ON CONFLICT (key) DO NOTHING;

-- Drop the existing mutable-key policy from migration 000046 and recreate it with the
-- two new keys added to the allowlist.
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
        'oidc_directory_endpoint'
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
        'oidc_directory_endpoint'
    ));
