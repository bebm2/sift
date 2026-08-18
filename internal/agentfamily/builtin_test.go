package agentfamily

import "testing"

// wantBuiltinIDs is the closed set of families Sift ships with. Adding a new
// builtin/*.yaml file must extend this list in the same change (TestBuiltinIDs
// guards against a file silently failing to load or a stray extra file).
var wantBuiltinIDs = []string{"claude", "codex", "cursor", "opencode", "pi"}

func TestBuiltinFamiliesValid(t *testing.T) {
	// Builtin panics on decode/validation failure; reaching this line at all
	// is the assertion that every embedded file parses and validates.
	families := Builtin()
	if len(families) == 0 {
		t.Fatal("Builtin: got no families")
	}
}

func TestBuiltinIDs(t *testing.T) {
	got := BuiltinIDs()
	if len(got) != len(wantBuiltinIDs) {
		t.Fatalf("BuiltinIDs = %v, want %v", got, wantBuiltinIDs)
	}
	for i, id := range wantBuiltinIDs {
		if got[i] != id {
			t.Fatalf("BuiltinIDs = %v, want %v", got, wantBuiltinIDs)
		}
	}
}

func TestBuiltinClaudeShape(t *testing.T) {
	f, ok := Builtin()["claude"]
	if !ok {
		t.Fatal("Builtin: missing claude")
	}
	if args, ok := f.Flag("model", "opus"); !ok || args[0] != "--model" || args[1] != "opus" {
		t.Errorf("claude Flag(model) = %v, %v", args, ok)
	}
	if _, ok := f.Flag("thinking", "high"); ok {
		t.Error("claude Flag(thinking) = ok, want unsupported (not confirmed by Anthropic docs)")
	}
}

func TestBuiltinCodexShape(t *testing.T) {
	f, ok := Builtin()["codex"]
	if !ok {
		t.Fatal("Builtin: missing codex")
	}
	args, ok := f.Flag("thinking", "high")
	if !ok {
		t.Fatal("codex Flag(thinking) = false, want true")
	}
	want := []string{"--config", "model_reasoning_effort=high"}
	if len(args) != len(want) || args[0] != want[0] || args[1] != want[1] {
		t.Errorf("codex Flag(thinking) = %v, want %v", args, want)
	}
}
