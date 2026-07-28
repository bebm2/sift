package brain

import (
	"strings"
	"testing"
)

// Task Spec v1 assembler tests (brain.md §9, §10.8): the four sources
// (Description / Goals / Guardrails / Context) and their hashes are
// reconstructible from the snapshot; missing context hashes to the SHA-256
// of the prescribed empty content; assembly is deterministic.

func baseParams() TaskSpecParams {
	return TaskSpecParams{
		Title: "Fix crash", Body: "it panics", SourceURL: "https://x/1",
		Goals:          []string{"reproduce", "fix"},
		PolicyHash:     "abc123",
		Rules:          []string{"no force push"},
		ProjectContext: ContextSegment{Text: "project ctx"},
		GlobalContext:  ContextSegment{Text: "global ctx"},
		TaskAnnotations: []T2Annotation{
			{EventID: "ev-1", Text: "note"},
		},
		Kind: TaskBug, Agent: "claude-code", HITLBeforeStart: true,
		LogicalCallID: "call-1", PromptVersion: "T2/v1/abcdef012345",
	}
}

func TestAssembleTaskSpecSourcesAndHashes(t *testing.T) {
	canonical, digest, err := AssembleTaskSpec(baseParams())
	if err != nil {
		t.Fatalf("AssembleTaskSpec: %v", err)
	}
	s := string(canonical)
	// Four frozen sources all present with derived hashes.
	for _, want := range []string{
		`"schema_version":1`,
		`"description":{"body":"it panics","source_url":"https://x/1","title":"Fix crash"}`,
		`"goals":["reproduce","fix"]`,
		`"guardrails":{"policy_hash":"abc123","rules":["no force push"]}`,
		`"blob_hash":"` + ContextSegment{Text: "project ctx"}.Hash() + `"`,
		`"content_hash":"` + ContextSegment{Text: "global ctx"}.Hash() + `"`,
		`"task_annotations":[{"event_id":"ev-1","text":"note"}]`,
		`"assignment":{"agent":"claude-code","hitl_before_start":true,"kind":"bug"}`,
		`"brain":{"logical_call_id":"call-1","prompt_version":"T2/v1/abcdef012345"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("spec missing %s:\n%s", want, s)
		}
	}
	if digest != DigestBytes(canonical) {
		t.Fatal("digest must cover the canonical bytes")
	}

	// Deterministic: same frozen inputs rebuild the identical snapshot.
	again, againDigest, err := AssembleTaskSpec(baseParams())
	if err != nil || string(again) != s || againDigest != digest {
		t.Fatal("assembly must be deterministic")
	}
}

func TestAssembleTaskSpecEmptyContextHash(t *testing.T) {
	p := baseParams()
	p.ProjectContext = ContextSegment{}
	p.GlobalContext = ContextSegment{}
	p.TaskAnnotations = nil
	p.Rules = nil
	canonical, _, err := AssembleTaskSpec(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(canonical)
	// Missing context is the empty text hashed per config.md §4 (SHA-256 of
	// the empty string): the well-known empty digest.
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if !strings.Contains(s, `"blob_hash":"`+emptySHA256+`"`) || !strings.Contains(s, `"content_hash":"`+emptySHA256+`"`) {
		t.Fatalf("empty context hash wrong: %s", s)
	}
	if !strings.Contains(s, `"task_annotations":[]`) || !strings.Contains(s, `"rules":[]`) {
		t.Fatalf("empty slices must serialize as []: %s", s)
	}
}

func TestAssembleTaskSpecGuardrailsNeverFromLLM(t *testing.T) {
	p := baseParams()
	p.PolicyHash = ""
	if _, _, err := AssembleTaskSpec(p); err == nil {
		t.Fatal("missing policy hash must fail: guardrails have no LLM source")
	}
	p = baseParams()
	p.Goals = nil
	if _, _, err := AssembleTaskSpec(p); err == nil {
		t.Fatal("empty goals must fail")
	}
}
