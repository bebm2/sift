package command

import (
	"encoding/hex"
	"strings"
	"testing"
)

func validRunID() string {
	// 32 lowercase hex bytes.
	return "0123456789abcdef0123456789abcdef"
}
func validNonce() string { return "fedcba9876543210fedcba9876543210" }

func TestParseApproveRejectRetry(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
		want ParsedCommand
	}{
		{"approve", "/sift approve " + validRunID() + " " + validNonce(), true, ParsedCommand{Action: ActionApprove, RunID: validRunID(), Nonce: validNonce()}},
		{"approve trailing space", "/sift approve " + validRunID() + " " + validNonce() + " ", false, ParsedCommand{}},
		{"approve extra arg", "/sift approve " + validRunID() + " " + validNonce() + " x", false, ParsedCommand{}},
		{"retry", "/sift retry " + validRunID() + " " + validNonce(), true, ParsedCommand{Action: ActionRetry, RunID: validRunID(), Nonce: validNonce()}},
		{"retry extra", "/sift retry " + validRunID() + " " + validNonce() + " x", false, ParsedCommand{}},
		{"reject no reason", "/sift reject " + validRunID() + " " + validNonce(), true, ParsedCommand{Action: ActionReject, RunID: validRunID(), Nonce: validNonce()}},
		{"reject reason", "/sift reject " + validRunID() + " " + validNonce() + " too risky", true, ParsedCommand{Action: ActionReject, RunID: validRunID(), Nonce: validNonce(), RejectReason: "too risky"}},
		{"reject reason with CR", "/sift reject " + validRunID() + " " + validNonce() + " bad\rreason", false, ParsedCommand{}},
		{"reject reason with LF", "/sift reject " + validRunID() + " " + validNonce() + " bad\nreason", false, ParsedCommand{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCommand(tc.body)
			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Fatalf("got %+v want %+v", got, tc.want)
				}
			} else {
				if err == nil {
					t.Fatalf("expected syntax error, got %+v", got)
				}
			}
		})
	}
}

func TestParseRunIDNonce(t *testing.T) {
	base := func(tok string) string { return "/sift approve " + tok + " " + validNonce() }
	bad := []string{
		base("short"),                       // too short
		base(strings.Repeat("a", 33)),       // too long
		base(strings.Repeat("g", 32)),       // non-hex
		base(strings.ToUpper(validRunID())), // uppercase
	}
	for _, b := range bad {
		if _, err := ParseCommand(b); err == nil {
			t.Fatalf("expected error for %q", b)
		}
	}
}

func TestParseAskText(t *testing.T) {
	prefix := "/sift ask " + validRunID() + " " + validNonce() + " "
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"plain", prefix + "what is the plan?", true},
		{"lf", prefix + "line1\nline2", true},
		{"crlf", prefix + "line1\r\nline2", true},
		{"bare cr", prefix + "bad\rstandalone", false},
		{"trailing cr", prefix + "text\r", false},
		{"empty text", "/sift ask " + validRunID() + " " + validNonce(), false},
		{"nul", prefix + "bad\x00text", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCommand(tc.body)
			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				want := strings.TrimPrefix(tc.body, prefix)
				if got.AskText != want {
					t.Fatalf("ask text mismatch got %q want %q", got.AskText, want)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for %q", tc.body)
				}
			}
		})
	}
}

func TestParseHoldDuration(t *testing.T) {
	prefix := "/sift hold " + validRunID() + " " + validNonce() + " "
	cases := []struct {
		dur    string
		ok     bool
		wantMS int64
	}{
		{"1ns", false, 0},
		{"999us", false, 0}, // sub-ms
		{"999µs", false, 0}, // sub-ms micro sign
		{"1ms", true, 1},
		{"1.5ms", false, 0}, // sub-ms
		{"500ms", true, 500},
		{"1s", true, 1000},
		{"2h30m", true, 9_000_000},
		{"1h", true, 3_600_000},
		{"1.5h", true, 5_400_000},
		{"0s", false, 0},
		{"-5s", false, 0},
		{"+5s", false, 0},
		{"5", false, 0},                  // missing unit
		{"5x", false, 0},                 // bad unit
		{"1d", false, 0},                 // bad unit
		{"", false, 0},                   // missing
		{"10000000000000000h", false, 0}, // overflow
	}
	for _, tc := range cases {
		t.Run(tc.dur, func(t *testing.T) {
			got, err := ParseCommand(prefix + tc.dur)
			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.HoldDurationMS != tc.wantMS {
					t.Fatalf("got %d ms want %d", got.HoldDurationMS, tc.wantMS)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for %q got %+v", tc.dur, got)
				}
			}
		})
	}
}

func TestParseLeadingToken(t *testing.T) {
	bad := []string{
		"",
		"nope",
		"/sift",          // no action
		"/sift ",         // trailing space no action
		"/sift  approve", // double space
		"/Sift approve " + validRunID() + " " + validNonce(), // case
		"sift approve " + validRunID() + " " + validNonce(),  // no slash
		"/sift bogus " + validRunID() + " " + validNonce(),   // unknown action
	}
	for _, b := range bad {
		if _, err := ParseCommand(b); err == nil {
			t.Fatalf("expected error for %q", b)
		}
	}
}

func TestRecomputeEventKeyStable(t *testing.T) {
	a, err := RecomputeEventKey("proj", SourceForgeComment, "c1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RecomputeEventKey("proj", SourceForgeComment, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("event key not stable")
	}
	// Same remote id across sources/projects must differ (different keys).
	c, _ := RecomputeEventKey("proj", SourceApprovalLabel, "c1")
	d, _ := RecomputeEventKey("other", SourceForgeComment, "c1")
	if a == c || a == d || c == d {
		t.Fatalf("event keys collided across source/project")
	}
	if len(a) != 64 {
		t.Fatalf("event key len %d", len(a))
	}
	_, err = hex.DecodeString(a)
	if err != nil {
		t.Fatalf("event key not hex: %v", err)
	}
}

func TestEnvelopeValidation(t *testing.T) {
	key, _ := RecomputeEventKey("proj", SourceForgeComment, "c1")
	actor := "alice"
	goodComment := CommandEventEnvelopeV1{
		SchemaVersion: 1, EventKey: key, ProjectID: "proj", Source: SourceForgeComment,
		RemoteEventID: "c1", Target: CommandTarget{Kind: TargetIssue, ID: "42"},
		Actor: &actor, RawDigest: strings.Repeat("a", 64), OccurredAtMS: 1,
		Comment: &CommandComment{ID: "c1", Body: "/sift approve " + validRunID() + " " + validNonce()},
	}
	if err := goodComment.Validate(); err != nil {
		t.Fatalf("good comment: %v", err)
	}
	if !goodComment.VerifyEventKey() {
		t.Fatal("event key verification failed")
	}
	// Tampered event key must fail verification.
	bad := goodComment
	bad.EventKey = strings.Repeat("b", 64)
	if bad.VerifyEventKey() {
		t.Fatal("tampered event key verified")
	}
	// Schema mismatch.
	bad = goodComment
	bad.SchemaVersion = 2
	if err := bad.Validate(); err == nil {
		t.Fatal("expected schema error")
	}
	// Missing comment for forge_comment.
	bad = goodComment
	bad.Comment = nil
	if err := bad.Validate(); err == nil {
		t.Fatal("expected comment required error")
	}
	// Body too large.
	bad = goodComment
	bad.Comment = &CommandComment{ID: "c1", Body: strings.Repeat("x", MaxBodyBytes+1)}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected body size error")
	}
}

func TestApprovalLabelEnvelope(t *testing.T) {
	pos := "100"
	key, _ := RecomputeEventKey("proj", SourceApprovalLabel, "l1")
	actor := "alice"
	good := CommandEventEnvelopeV1{
		SchemaVersion: 1, EventKey: key, ProjectID: "proj", Source: SourceApprovalLabel,
		RemoteEventID: "l1", Target: CommandTarget{Kind: TargetIssue, ID: "42"},
		Actor: &actor, RawDigest: strings.Repeat("a", 64), OccurredAtMS: 1,
		Label: &CommandLabel{EventID: "l1", Name: "approved", Action: "added"}, LabelPosition: &pos,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good label: %v", err)
	}
	// Leading-zero / non-positive position rejects.
	zero := "0"
	bad := good
	bad.LabelPosition = &zero
	if err := bad.Validate(); err == nil {
		t.Fatal("expected position error for 0")
	}
	big := strings.Repeat("9", MaxLabelPositionDigits+1)
	bad = good
	bad.LabelPosition = &big
	if err := bad.Validate(); err == nil {
		t.Fatal("expected position length error")
	}
	// Action != added rejects.
	bad = good
	bad.Label = &CommandLabel{EventID: "l1", Name: "approved", Action: "removed"}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected action error")
	}
}

func TestAuthorizer(t *testing.T) {
	a := NewAuthorizer([]string{"alice", "bob", "alice"})
	alice := "alice"
	carol := "carol"
	if !a.Trusted(&alice) {
		t.Fatal("alice should be trusted")
	}
	if a.Trusted(&carol) {
		t.Fatal("carol should not be trusted")
	}
	if a.Trusted(nil) {
		t.Fatal("nil actor should not be trusted")
	}
	empty := ""
	if a.Trusted(&empty) {
		t.Fatal("empty actor should not be trusted")
	}
}
