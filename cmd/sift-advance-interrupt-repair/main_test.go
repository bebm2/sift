package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

const appendOnlyTrigger = `CREATE TRIGGER interrupt_command_effect_bindings_append_only_update BEFORE UPDATE ON interrupt_command_effect_bindings FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'append-only table'); END`

func repairFixture(t *testing.T, rows string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sift.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE interrupt_command_effect_bindings (interrupt_id TEXT PRIMARY KEY, reason TEXT, binding_json TEXT, binding_digest TEXT); ` + appendOnlyTrigger + `; ` + rows); err != nil {
		t.Fatal(err)
	}
	return path
}

func bindingRows(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT interrupt_id || ':' || binding_json || ':' || binding_digest FROM interrupt_command_effect_bindings ORDER BY interrupt_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRepairBacksUpAndRestoresPopulatedDatabase(t *testing.T) {
	path := repairFixture(t, `INSERT INTO interrupt_command_effect_bindings VALUES ('i1','x','{"a":1}','duplicate'),('i2','x','{"a":2}','duplicate')`)
	before := bindingRows(t, path)
	backup := path + ".backup"
	var out, stderr bytes.Buffer
	if got := run([]string{"--db", path, "--repair", "--backup", backup}, &out, &stderr); got != 0 {
		t.Fatalf("run = %d stderr=%s", got, stderr.String())
	}
	if !strings.Contains(out.String(), "repaired interrupt=i1") {
		t.Fatalf("output = %s", out.String())
	}
	if got := bindingRows(t, backup); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatalf("backup rows = %q, want %q", got, before)
	}
	if err := copyFile(backup, path); err != nil {
		t.Fatal(err)
	}
	if err := verifyBackup(path); err != nil {
		t.Fatal(err)
	}
	if got := bindingRows(t, path); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatalf("restored rows = %q, want %q", got, before)
	}
}

func TestRepairRollbackLeavesAllDuplicateGroupsUntouched(t *testing.T) {
	path := repairFixture(t, `INSERT INTO interrupt_command_effect_bindings VALUES
		('i1','x','{"a":1}','duplicate-a'),
		('i2','x','{"a":2}','duplicate-a'),
		('i3','x','{"a":1}','duplicate-b'),
		('i4','x','{"a":3}','duplicate-b')`)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER repair_failure BEFORE UPDATE ON interrupt_command_effect_bindings WHEN OLD.interrupt_id='i3' BEGIN SELECT RAISE(ABORT, 'injected repair failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before := bindingRows(t, path)
	var out, stderr bytes.Buffer
	if got := run([]string{"--db", path, "--repair", "--backup", path + ".backup"}, &out, &stderr); got != 1 {
		t.Fatalf("run = %d stderr=%s", got, stderr.String())
	}
	if got := bindingRows(t, path); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatalf("rollback rows = %q, want %q", got, before)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE interrupt_command_effect_bindings SET reason='changed' WHERE interrupt_id='i1'`); err == nil || !strings.Contains(err.Error(), "append-only table") {
		t.Fatalf("append-only trigger not restored: %v", err)
	}
	if _, err := os.Stat(path + ".backup"); err != nil {
		t.Fatalf("backup missing after failed repair: %v", err)
	}
}
