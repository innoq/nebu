-- Story 7-35: enable Row-Level Security on user-scoped tables.
-- Companion: each query in notifications_store, push_rules_store, pushers_store
-- now runs inside withUserDB which issues SET LOCAL app.user_id = $1 before the query.

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_nebu_app_policy ON notifications
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));

ALTER TABLE push_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY push_rules_nebu_app_policy ON push_rules
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));

ALTER TABLE pushers ENABLE ROW LEVEL SECURITY;
ALTER TABLE pushers FORCE ROW LEVEL SECURITY;
CREATE POLICY pushers_nebu_app_policy ON pushers
    FOR ALL
    TO nebu_app
    USING (user_id = current_setting('app.user_id', true))
    WITH CHECK (user_id = current_setting('app.user_id', true));
