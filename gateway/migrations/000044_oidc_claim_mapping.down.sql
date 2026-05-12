-- Story 11-10: Remove OIDC claim mapping defaults from server_config.
DELETE FROM server_config WHERE key IN ('oidc_user_id_claim', 'oidc_displayname_claim', 'oidc_email_claim');
