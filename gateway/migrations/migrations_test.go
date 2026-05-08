package migrations_test

import (
	"testing"

	"github.com/nebu/nebu/migrations"
)

func TestFS_ContainsExpectedMigrationFiles(t *testing.T) {
	// Verify the embedded FS contains the required migration files.
	// golang-migrate requires both .up.sql and .down.sql for each version.
	files := []string{
		"000001_init.up.sql",
		"000001_init.down.sql",
		"000002_message_buffer.up.sql",
		"000002_message_buffer.down.sql",
		"000003_server_config.up.sql",
		"000003_server_config.down.sql",
		"000004_users.up.sql",
		"000004_users.down.sql",
		"000005_sessions.up.sql",
		"000005_sessions.down.sql",
		"000006_users_email_pii.up.sql",
		"000006_users_email_pii.down.sql",
		"000016_media_files.up.sql",
		"000016_media_files.down.sql",
		// Story 5.1: audit_log schema + RLS (000018 — 000017 is admin_sessions)
		"000018_audit_log.up.sql",
		"000018_audit_log.down.sql",
		// Story 5.3: compliance_requests table + RLS
		"000019_compliance_requests.up.sql",
		"000019_compliance_requests.down.sql",
		// Story 5.5: compliance_sessions table + partial unique index + RLS
		"000020_compliance_sessions.up.sql",
		"000020_compliance_sessions.down.sql",
		// Story 5.7: users.deletion_status + users.keys_deleted_at for DSGVO key deletion
		"000021_users_deletion_status.up.sql",
		"000021_users_deletion_status.down.sql",
		// Story 5.8: users.anonymized_at for operational PII anonymization
		"000022_users_anonymized.up.sql",
		"000022_users_anonymized.down.sql",
		// Story 5.8: media_files.deleted soft-delete flag for avatar cleanup
		"000023_media_files_deleted.up.sql",
		"000023_media_files_deleted.down.sql",
		// Story 11.1: search_vector tsvector column + GIN index + trigger on events
		"000042_search_vector.up.sql",
		"000042_search_vector.down.sql",
	}

	for _, name := range files {
		_, err := migrations.FS.Open(name)
		if err != nil {
			t.Errorf("embedded FS missing required file %q: %v", name, err)
		}
	}
}

func TestFS_UpMigrationIsNotEmpty(t *testing.T) {
	// The initial up migration must not be empty — it must create extensions.
	f, err := migrations.FS.Open("000001_init.up.sql")
	if err != nil {
		t.Fatalf("cannot open 000001_init.up.sql: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("cannot stat 000001_init.up.sql: %v", err)
	}

	if stat.Size() == 0 {
		t.Error("000001_init.up.sql must not be empty")
	}
}
