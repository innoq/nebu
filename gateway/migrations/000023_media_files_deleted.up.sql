-- Story 5.8: Operational PII Anonymization — add soft-delete flag to media_files.
-- deleted (BOOLEAN NOT NULL DEFAULT false) is set to true when the associated avatar
-- file is removed during user anonymization. The media download handler filters
-- rows WHERE NOT deleted to prevent serving deleted avatar files.
ALTER TABLE media_files ADD COLUMN deleted BOOLEAN NOT NULL DEFAULT false;
