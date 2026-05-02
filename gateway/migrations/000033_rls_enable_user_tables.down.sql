DROP POLICY IF EXISTS notifications_nebu_app_policy ON notifications;
ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS push_rules_nebu_app_policy ON push_rules;
ALTER TABLE push_rules DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS pushers_nebu_app_policy ON pushers;
ALTER TABLE pushers DISABLE ROW LEVEL SECURITY;
