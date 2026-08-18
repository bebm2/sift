package agentfamily

import "testing"

func TestParseValidFamily(t *testing.T) {
	data := []byte(`
id: claude
match: [claude]
run:
  args: ["-p"]
  version_args: ["--version"]
  flags:
    model: ["--model", "{value}"]
auth:
  env: [ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN]
config:
  env: [ANTHROPIC_BASE_URL]
  dirs: ["~/.claude"]
`)
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.ID != "claude" {
		t.Errorf("ID = %q, want claude", f.ID)
	}
	if len(f.Match) != 1 || f.Match[0] != "claude" {
		t.Errorf("Match = %v", f.Match)
	}
	if len(f.Auth.Env) != 2 {
		t.Errorf("Auth.Env = %v", f.Auth.Env)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	data := []byte(`
id: claude
match: [claude]
run:
  args: ["-p"]
bogus: true
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse: want error for unknown top-level field, got nil")
	}
}

func TestParseRejectsDuplicateKey(t *testing.T) {
	data := []byte(`
id: claude
id: codex
match: [claude]
run:
  args: ["-p"]
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse: want error for duplicate YAML key, got nil")
	}
}

func TestValidateRequiresID(t *testing.T) {
	f := &Family{Match: []string{"x"}, Run: RunSpec{Args: []string{"-p"}}}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for missing id, got nil")
	}
}

func TestValidateRejectsBadID(t *testing.T) {
	f := &Family{ID: "Claude Code", Match: []string{"x"}, Run: RunSpec{Args: []string{"-p"}}}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for malformed id, got nil")
	}
}

func TestValidateRequiresMatch(t *testing.T) {
	f := &Family{ID: "claude", Run: RunSpec{Args: []string{"-p"}}}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for empty match, got nil")
	}
}

func TestValidateRequiresRunArgs(t *testing.T) {
	f := &Family{ID: "claude", Match: []string{"claude"}}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for empty run.args, got nil")
	}
}

func TestValidateRejectsFlagWithoutPlaceholder(t *testing.T) {
	f := &Family{
		ID:    "claude",
		Match: []string{"claude"},
		Run: RunSpec{
			Args:  []string{"-p"},
			Flags: map[string][]string{"model": {"--model", "opus"}},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for flag missing placeholder, got nil")
	}
}

func TestValidateRejectsFlagWithMultiplePlaceholders(t *testing.T) {
	f := &Family{
		ID:    "claude",
		Match: []string{"claude"},
		Run: RunSpec{
			Args:  []string{"-p"},
			Flags: map[string][]string{"model": {"{value}", "{value}"}},
		},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("Validate: want error for flag with two placeholders, got nil")
	}
}

func TestFlagSubstitutesValue(t *testing.T) {
	f := &Family{
		ID:    "claude",
		Match: []string{"claude"},
		Run: RunSpec{
			Args:  []string{"-p"},
			Flags: map[string][]string{"model": {"--model", "{value}"}},
		},
	}
	args, ok := f.Flag("model", "opus")
	if !ok {
		t.Fatal("Flag: ok = false, want true")
	}
	want := []string{"--model", "opus"}
	if len(args) != len(want) || args[0] != want[0] || args[1] != want[1] {
		t.Errorf("Flag = %v, want %v", args, want)
	}
}

func TestFlagUnknownNameNotOK(t *testing.T) {
	f := &Family{ID: "claude", Match: []string{"claude"}, Run: RunSpec{Args: []string{"-p"}}}
	if _, ok := f.Flag("thinking", "high"); ok {
		t.Fatal("Flag: ok = true for undeclared name, want false")
	}
}
