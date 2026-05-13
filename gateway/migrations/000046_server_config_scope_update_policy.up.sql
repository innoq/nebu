-- Story 12.7 — SEC Gate 2 fix [MEDIUM-5]:
-- Replace the blanket config_update_all policy (USING true) introduced in migration 000045
-- with a key-scoped policy restricted to known mutable keys.
--
-- The original intent of migration 000003 was to make server_name and bootstrap_completed
-- immutable (no UPDATE policy → DB refuses updates by construction). Migration 000045
-- accidentally reversed this for ALL keys to support OIDC claim upserts.
--
-- This migration restores defence-in-depth: only the following keys can be updated
-- by nebu_app. All other keys (including server_name, bootstrap_completed) are
-- immutable at the DB level again.
--
-- Mutable keys: oidc_user_id_claim, oidc_displayname_claim, oidc_email_claim,
--               admin_group_claim, oidc_issuer, oidc_client_id, oidc_client_secret
-- Immutable keys (protected): server_name, bootstrap_completed, and any future
--               keys not explicitly added to the mutable list.

DROP POLICY IF EXISTS config_update_all ON server_config;

CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret'
    ))
    WITH CHECK (key IN (
        'oidc_user_id_claim',
        'oidc_displayname_claim',
        'oidc_email_claim',
        'admin_group_claim',
        'oidc_issuer',
        'oidc_client_id',
        'oidc_client_secret'
    ));
