-- Migration 000032: push_rules and pushers tables
-- Story 7-30: Push Rules API — GET/PUT/DELETE /pushrules + Pushers
--
-- NOTE: No RLS policies — the gateway queries with WHERE user_id=$1 directly.
-- The GUC current_setting('app.user_id') is never set at runtime (Story 7-30 notes).

CREATE TABLE IF NOT EXISTS push_rules (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT    NOT NULL,
    scope        TEXT    NOT NULL DEFAULT 'global',
    kind         TEXT    NOT NULL,   -- override|content|room|sender|underride
    rule_id      TEXT    NOT NULL,
    priority     INT     NOT NULL DEFAULT 0,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    conditions   JSONB   NOT NULL DEFAULT '[]',
    actions      JSONB   NOT NULL DEFAULT '["notify"]',
    default_rule BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (user_id, scope, kind, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_push_rules_user_scope
    ON push_rules (user_id, scope);

CREATE TABLE IF NOT EXISTS pushers (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             TEXT NOT NULL,
    pushkey             TEXT NOT NULL,
    kind                TEXT NOT NULL,
    app_id              TEXT NOT NULL,
    app_display_name    TEXT NOT NULL,
    device_display_name TEXT NOT NULL,
    lang                TEXT NOT NULL DEFAULT 'en',
    data                JSONB NOT NULL DEFAULT '{}',
    UNIQUE (user_id, app_id, pushkey)
);

CREATE INDEX IF NOT EXISTS idx_pushers_user_id
    ON pushers (user_id);

-- Explicit grants for nebu_app (belt-and-suspenders alongside ALTER DEFAULT PRIVILEGES).
GRANT SELECT, INSERT, UPDATE, DELETE ON push_rules TO nebu_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON pushers TO nebu_app;
GRANT USAGE, SELECT ON SEQUENCE push_rules_id_seq TO nebu_app;
GRANT USAGE, SELECT ON SEQUENCE pushers_id_seq TO nebu_app;
