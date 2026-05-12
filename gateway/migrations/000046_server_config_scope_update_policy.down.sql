-- Revert to the blanket update policy from migration 000045.
-- WARNING: This restores the overly permissive UPDATE policy that allows nebu_app
-- to update any server_config key, including immutable keys like server_name.
DROP POLICY IF EXISTS config_update_mutable ON server_config;

CREATE POLICY config_update_all ON server_config
    FOR UPDATE
    USING (true)
    WITH CHECK (true);
