-- gateway/migrations/000004_users.down.sql
-- Drop order: user_keys first (FK user_id → users.user_id), then users.

DROP TABLE IF EXISTS user_keys;
DROP TABLE IF EXISTS users;
