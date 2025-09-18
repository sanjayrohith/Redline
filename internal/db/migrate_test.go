package db

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrations_SortsByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"002_add_orgs.sql":  {Data: []byte("CREATE TABLE orgs ();")},
		"001_add_users.sql": {Data: []byte("CREATE TABLE users ();")},
	}

	migrations, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("len(migrations) = %d, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Errorf("versions = [%d %d], want [1 2]", migrations[0].Version, migrations[1].Version)
	}
	if migrations[0].Name != "add_users" {
		t.Errorf("Name = %q, want add_users", migrations[0].Name)
	}
}

func TestLoadMigrations_IgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"001_add_users.sql": {Data: []byte("CREATE TABLE users ();")},
		"README.md":         {Data: []byte("not a migration")},
	}

	migrations, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("len(migrations) = %d, want 1", len(migrations))
	}
}

func TestLoadMigrations_RejectsMalformedFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"not-a-migration.sql": {Data: []byte("SELECT 1;")},
	}

	if _, err := LoadMigrations(fsys); err == nil {
		t.Fatal("LoadMigrations() error = nil, want error for malformed filename")
	}
}

func TestLoadMigrations_RejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"001_add_users.sql": {Data: []byte("CREATE TABLE users ();")},
		"001_add_orgs.sql":  {Data: []byte("CREATE TABLE orgs ();")},
	}

	if _, err := LoadMigrations(fsys); err == nil {
		t.Fatal("LoadMigrations() error = nil, want error for duplicate version")
	}
}
