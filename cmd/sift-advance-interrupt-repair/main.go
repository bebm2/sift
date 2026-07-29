// Command sift-advance-interrupt-repair audits and repairs duplicate effect-binding
// digests before the uniqueness migration is applied.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type bindingRow struct{ interrupt, reason, body, digest string }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("sift-advance-interrupt-repair", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dbPath := fs.String("db", "", "SQLite database path")
	backup := fs.String("backup", "", "backup path (required with --repair)")
	repair := fs.Bool("repair", false, "backup and repair non-identical duplicate bindings")
	if e := fs.Parse(args); e != nil || *dbPath == "" || (*repair && *backup == "") {
		fmt.Fprintln(errOut, "usage: sift-advance-interrupt-repair --db PATH [--repair --backup PATH]")
		return 2
	}
	if *repair {
		if err := copyFile(*dbPath, *backup); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintf(out, "backup=%s\n", *backup)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Clean(*dbPath)+"?_pragma=foreign_keys(1)")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer db.Close()
	rows, err := db.Query(`SELECT interrupt_id,reason,binding_json,binding_digest FROM interrupt_command_effect_bindings ORDER BY binding_digest,interrupt_id`)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer rows.Close()
	groups := map[string][]bindingRow{}
	for rows.Next() {
		var r bindingRow
		if err := rows.Scan(&r.interrupt, &r.reason, &r.body, &r.digest); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		groups[r.digest] = append(groups[r.digest], r)
	}
	duplicates := 0
	for digest, group := range groups {
		if len(group) < 2 {
			continue
		}
		duplicates++
		fmt.Fprintf(out, "duplicate digest=%s rows=%d\n", digest, len(group))
		seen := map[string]string{}
		for _, r := range group {
			canonical, e := canonicalJSON(r.body)
			if e != nil {
				fmt.Fprintf(errOut, "interrupt=%s invalid JSON: %v\n", r.interrupt, e)
				return 1
			}
			if prior, ok := seen[string(canonical)]; ok {
				fmt.Fprintf(errOut, "interrupts %s and %s have identical immutable binding; cannot safely repair\n", prior, r.interrupt)
				return 1
			}
			seen[string(canonical)] = r.interrupt
		}
		if *repair {
			tx, e := db.Begin()
			if e != nil {
				fmt.Fprintln(errOut, e)
				return 1
			}
			for _, r := range group {
				canonical, _ := canonicalJSON(r.body)
				sum := sha256.Sum256(canonical)
				next := hex.EncodeToString(sum[:])
				if _, e = tx.Exec(`UPDATE interrupt_command_effect_bindings SET binding_json=?,binding_digest=? WHERE interrupt_id=? AND binding_digest=?`, string(canonical), next, r.interrupt, digest); e != nil {
					_ = tx.Rollback()
					fmt.Fprintln(errOut, e)
					return 1
				}
				fmt.Fprintf(out, "repaired interrupt=%s digest=%s\n", r.interrupt, next)
			}
			if e = tx.Commit(); e != nil {
				fmt.Fprintln(errOut, e)
				return 1
			}
		}
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if duplicates == 0 {
		fmt.Fprintln(out, "no duplicate binding digests")
	}
	if duplicates > 0 && !*repair {
		fmt.Fprintln(out, "check only; rerun with --repair --backup PATH")
	}
	return 0
}

func canonicalJSON(raw string) ([]byte, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func copyFile(src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return fmt.Errorf("backup must differ from database")
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	return out.Sync()
}
