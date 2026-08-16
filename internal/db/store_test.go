package db

import (
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_open.db")
	sqlDB, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer sqlDB.Close()

	var fk int
	if err := sqlDB.QueryRow("PRAGMA foreign_keys;").Scan(&fk); err != nil {
		t.Fatalf("query PRAGMA foreign_keys error: %v", err)
	}
	if fk != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", fk)
	}

	var jm string
	if err := sqlDB.QueryRow("PRAGMA journal_mode;").Scan(&jm); err != nil {
		t.Fatalf("query PRAGMA journal_mode error: %v", err)
	}
	if jm != "wal" {
		t.Errorf("PRAGMA journal_mode = %q, want 'wal'", jm)
	}
}

func TestMigrateIdempotency(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_migrate.db")
	sqlDB, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("first Migrate() error: %v", err)
	}

	// Second run should be a no-op / idempotent
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("second Migrate() error: %v", err)
	}
}
