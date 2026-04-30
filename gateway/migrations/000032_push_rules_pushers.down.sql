-- Migration 000032 rollback: drop push_rules and pushers tables
-- Story 7-30: Push Rules API

DROP TABLE IF EXISTS pushers;
DROP TABLE IF EXISTS push_rules;
