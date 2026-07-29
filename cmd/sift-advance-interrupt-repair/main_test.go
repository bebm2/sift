package main

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRepairUsesAtomicMaintenancePathAndRestoresAppendOnlyTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sift.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE interrupt_command_effect_bindings (interrupt_id TEXT PRIMARY KEY, reason TEXT, binding_json TEXT, binding_digest TEXT);
		CREATE TRIGGER interrupt_command_effect_bindings_append_only_update BEFORE UPDATE ON interrupt_command_effect_bindings FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'append-only table'); END;
		INSERT INTO interrupt_command_effect_bindings VALUES ('i1','x','{"a":1}','duplicate'),('i2','x','{"a":2}','duplicate')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	backup := path + ".backup"
	if got := run([]string{"--db", path, "--repair", "--backup", backup}, &out, &stderr); got != 0 {
		t.Fatalf("run = %d stderr=%s", got, stderr.String())
	}
	if !strings.Contains(out.String(), "repaired interrupt=i1") {
		t.Fatalf("output = %s", out.String())
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT binding_digest) FROM interrupt_command_effect_bindings`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("distinct digests = %d, %v", count, err)
	}
	if _, err := db.Exec(`UPDATE interrupt_command_effect_bindings SET reason='changed' WHERE interrupt_id='i1'`); err == nil || !strings.Contains(err.Error(), "append-only table") {
		t.Fatalf("append-only trigger not restored: %v", err)
	}
}
