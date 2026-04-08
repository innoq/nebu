-- profiles: public-facing Matrix user profile (separate from encrypted PII in users table)
-- displayname and avatar_url are explicitly public per Matrix spec.
-- REFERENCES users(user_id): ensures a user record exists before a profile row can be created.
CREATE TABLE profiles (
    user_id      TEXT   PRIMARY KEY REFERENCES users(user_id),
    displayname  TEXT,
    avatar_url   TEXT,
    updated_at   BIGINT NOT NULL
);
