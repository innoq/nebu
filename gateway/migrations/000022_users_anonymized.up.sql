-- Story 5.8: Operational PII Anonymization — add anonymized_at timestamp to users table.
-- anonymized_at (BIGINT, Unix-ms epoch) is set when an instance admin anonymizes a user account.
-- NULL means the user has not been anonymized. No DEFAULT — intentionally nullable.
ALTER TABLE users ADD COLUMN anonymized_at BIGINT;
