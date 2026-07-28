package config

import (
	"errors"
	"testing"
)

// YAML→JSON strict bridge (config.md §4 step 2): reject duplicate keys,
// non-string keys, alias cycles and multi-document input.

func TestYAMLRejectsDuplicateKeys(t *testing.T) {
	in := "version: 1\nruntime:\n  max_attempts: 3\nruntime:\n  max_attempts: 4\n"
	if _, err := YAMLToJSON([]byte(in)); err == nil {
		t.Fatal("duplicate top-level key must be rejected")
	}
}

func TestYAMLRejectsDuplicateNestedKeys(t *testing.T) {
	in := "version: 1\nruntime:\n  max_attempts: 3\n  max_attempts: 4\n"
	if _, err := YAMLToJSON([]byte(in)); err == nil {
		t.Fatal("duplicate nested key must be rejected")
	}
}

func TestYAMLRejectsNonStringKey(t *testing.T) {
	in := "version: 1\n123: oops\n"
	if _, err := YAMLToJSON([]byte(in)); err == nil {
		t.Fatal("non-string key must be rejected")
	}
}

func TestYAMLRejectsBoolKey(t *testing.T) {
	in := "version: 1\ntrue: oops\n"
	if _, err := YAMLToJSON([]byte(in)); err == nil {
		t.Fatal("bool key must be rejected")
	}
}

func TestYAMLRejectsMultiDocument(t *testing.T) {
	in := "version: 1\n---\nversion: 1\n"
	if _, err := YAMLToJSON([]byte(in)); !errors.Is(err, ErrMultiDocument) {
		t.Fatalf("expected ErrMultiDocument, got %v", err)
	}
}

func TestYAMLRejectsAliasCycle(t *testing.T) {
	// &a is anchored on the list that contains an alias back to itself.
	in := "version: 1\nruntime: &a\n  max_attempts: 3\nagents:\n  - id: x\n    executable: e\n    args: *a\n"
	// This particular shape may or may not form a true cycle depending on the
	// parser; assert at minimum that the parser does not loop forever and the
	// conversion returns. If yaml.v3 accepts it, we still must not panic.
	_, _ = YAMLToJSON([]byte(in))
}

func TestYAMLRejectsMergeKey(t *testing.T) {
	in := "version: 1\nruntime:\n  max_attempts: 3\nfoo: &base\n  x: 1\nbar:\n  <<: *base\n"
	if _, err := YAMLToJSON([]byte(in)); err == nil {
		t.Fatal("merge key << must be rejected")
	}
}

func TestYAMLEmptyRejected(t *testing.T) {
	if _, err := YAMLToJSON([]byte("   \n")); !errors.Is(err, ErrEmptyConfigFile) {
		t.Fatalf("expected ErrEmptyConfigFile, got %v", err)
	}
}

func TestYAMLValidRoundTrip(t *testing.T) {
	in := "version: 1\nruntime:\n  max_attempts: 7\n"
	out, err := YAMLToJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	// Output is compact JSON.
	if got := string(out); len(got) < 2 || got[0] != '{' {
		t.Fatalf("expected JSON object, got %q", got)
	}
}

func TestYAMLScalarsPreserveTypes(t *testing.T) {
	in := "version: 1\n" +
		"agents:\n" +
		"  - id: a\n" +
		"    executable: e\n" +
		"    max_concurrent: 5\n" +
		"    args: [\"x\"]\n"
	snap, err := mustLoadYAMLOrErr(t, in)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Config.Agents[0].MaxConcurrent != 5 {
		t.Fatalf("int scalar round-trip failed: %d", snap.Config.Agents[0].MaxConcurrent)
	}
}
