package agentfamily

import "testing"

func TestMatchByBaseName(t *testing.T) {
	f, ok := Match(Builtin(), "/usr/local/bin/claude")
	if !ok || f.ID != "claude" {
		t.Fatalf("Match(/usr/local/bin/claude) = %v, %v", f, ok)
	}
}

func TestMatchBareName(t *testing.T) {
	f, ok := Match(Builtin(), "codex")
	if !ok || f.ID != "codex" {
		t.Fatalf("Match(codex) = %v, %v", f, ok)
	}
}

func TestMatchCursorAliases(t *testing.T) {
	for _, name := range []string{"cursor-agent", "agent", "cursor"} {
		if f, ok := Match(Builtin(), name); !ok || f.ID != "cursor" {
			t.Errorf("Match(%s) = %v, %v, want cursor", name, f, ok)
		}
	}
}

func TestMatchUnknownReturnsFalse(t *testing.T) {
	if _, ok := Match(Builtin(), "no-such-agent"); ok {
		t.Fatal("Match(no-such-agent): ok = true, want false")
	}
}

func TestMatchDeterministicOnConflict(t *testing.T) {
	families := map[string]*Family{
		"b": {ID: "b", Match: []string{"shared"}},
		"a": {ID: "a", Match: []string{"shared"}},
	}
	f, ok := Match(families, "shared")
	if !ok || f.ID != "a" {
		t.Fatalf("Match(shared) = %v, %v, want a (lexicographically first)", f, ok)
	}
}
