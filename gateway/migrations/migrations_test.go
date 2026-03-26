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
