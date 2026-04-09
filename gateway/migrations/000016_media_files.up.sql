-- media_files: stores metadata and AES-256-GCM keys for uploaded media files.
-- Encrypted file lives on disk at NEBU_MEDIA_STORAGE_PATH/<server_name>/<media_id>.
-- aes_key_hex: 32-byte key as lowercase hex (64 chars).
-- nonce_hex: 12-byte GCM nonce as lowercase hex (24 chars).
-- DSGVO: DELETE FROM media_files WHERE uploader_user_id = $1 → file irrecoverably encrypted.
CREATE TABLE media_files (
    media_id          TEXT    PRIMARY KEY,
    server_name       TEXT    NOT NULL,
    content_type      TEXT    NOT NULL,
    file_size         BIGINT  NOT NULL,
    aes_key_hex       TEXT    NOT NULL,   -- 64 hex chars (32 bytes)
    nonce_hex         TEXT    NOT NULL,   -- 24 hex chars (12 bytes)
    uploader_user_id  TEXT    NOT NULL,
    uploaded_at       BIGINT  NOT NULL    -- Unix ms
);
CREATE INDEX media_files_uploader_idx ON media_files (uploader_user_id);
