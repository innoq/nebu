-- gateway/migrations/000002_message_buffer.down.sql
DROP TABLE IF EXISTS message_dead_letter;
DROP TABLE IF EXISTS message_buffer;
