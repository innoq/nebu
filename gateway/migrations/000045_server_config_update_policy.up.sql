-- Story 11-10 fix: Add UPDATE policy to server_config so nebu_app can use
-- ON CONFLICT DO UPDATE (upsert) for OIDC claim mapping and admin group claim.
-- The insert-only RLS design was an oversight: ClaimMappingHandler, ClaimSelectionHandler,
-- and SaveAdminGroupClaim all use upserts that perform an UPDATE on conflict.
-- Without this policy, updating any existing config key raises:
--   "new row violates row-level security policy (USING expression) for table server_config"
CREATE POLICY config_update_all ON server_config
    FOR UPDATE
    USING (true)
    WITH CHECK (true);
