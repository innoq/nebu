-- Revert migration 000048: remove oidc_directory rows and restore the 000046 policy.

DELETE FROM server_config WHERE key IN ('oidc_directory_enabled', 'oidc_directory_endpoint');

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
