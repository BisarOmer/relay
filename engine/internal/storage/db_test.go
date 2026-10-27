package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenUsesEmbeddedMigrations(t *testing.T) {
	db, err := Open(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := db.SchemaVersion(); v < 3 {
		t.Fatalf("schema version = %d", v)
	}
}

func TestReadOnlyPoolRejectsWrites(t *testing.T) {
	db, err := Open(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ReadOnly.Exec(`DELETE FROM connections`); err == nil {
		t.Fatal("read-only pool accepted a write")
	}
}

func TestCorruptDatabaseIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relay.db"), []byte("this is not a sqlite database at all, just junk bytes......"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dir, "")
	if err != nil {
		t.Fatalf("open after corruption: %v", err)
	}
	defer db.Close()
	if !db.Recovered || db.RecoveredPath == "" {
		t.Fatal("recovery not reported")
	}
	if _, err := os.Stat(db.RecoveredPath); err != nil {
		t.Fatalf("corrupt file not preserved: %v", err)
	}
}

func TestPreferencesValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPreferencesRepo(db)
	p, err := repo.Get()
	if err != nil || p != DefaultPreferences() {
		t.Fatalf("defaults: %+v %v", p, err)
	}
	if _, err := repo.Patch(json.RawMessage(`{"theme":"neon"}`)); err == nil {
		t.Fatal("invalid theme accepted")
	}
	if _, err := repo.Patch(json.RawMessage(`{"maxDisplayedMessages":5}`)); err == nil {
		t.Fatal("out of range accepted")
	}
	p, err = repo.Patch(json.RawMessage(`{"theme":"light","sidebarCollapsed":true}`))
	if err != nil || p.Theme != "light" || !p.SidebarCollapsed || p.Timezone != "local" {
		t.Fatalf("patch: %+v %v", p, err)
	}
	_ = db.Close()
	db2, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	p2, _ := NewPreferencesRepo(db2).Get()
	if p2.Theme != "light" || !p2.SidebarCollapsed {
		t.Fatalf("not persisted: %+v", p2)
	}
}
