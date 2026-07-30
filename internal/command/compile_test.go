package command

import (
	"encoding/json"
	"strings"
	"testing"
)

func baseEnv(source CommandSource, remoteID string) CommandEventEnvelopeV1 {
	key, _ := RecomputeEventKey("proj", source, remoteID)
	actor := "alice"
	env := CommandEventEnvelopeV1{
		SchemaVersion: 1, EventKey: key, ProjectID: "proj", Source: source,
		RemoteEventID: remoteID, Target: CommandTarget{Kind: TargetIssue, ID: "42"},
		Actor: &actor, RawDigest: strings.Repeat("a", 64), OccurredAtMS: 1,
	}
	if source == SourceForgeComment {
		env.Comment = &CommandComment{ID: remoteID, Body: "/sift approve " + validRunID() + " " + validNonce()}
	} else {
		pos := "100"
		env.Label = &CommandLabel{EventID: remoteID, Name: "approved", Action: "added"}
		env.LabelPosition = &pos
	}
	return env
}

func openInterrupt(reason InterruptReason, options []string) *InterruptView {
	return &InterruptView{
		ID: "int-1", RunID: validRunID(), Version: 3, RunVersion: 5, Reason: reason,
		Status: StatusOpen, DispatchState: DispatchReady, Nonce: validNonce(),
		Options: options, HoldMaxDurationMS: 0,
	}
}

func TestCompileApproveReject(t *testing.T) {
	interrupt := openInterrupt(ReasonDesignApproval, []string{"approve", "reject", "hold"})
	env := baseEnv(SourceForgeComment, "c1")
	parsed, err := ParseCommand(env.Comment.Body)
	if err != nil {
		t.Fatal(err)
	}
	res := Compile(env, parsed, interrupt, env.Target)
	if res.Outcome != OutcomeApplied {
		t.Fatalf("expected applied got %s", res.Outcome)
	}
	if res.Compiled.InterruptID != "int-1" || res.Compiled.ExpectedInterruptVersion != 3 || res.Compiled.RunID != validRunID() {
		t.Fatalf("compiled fields wrong: %+v", res.Compiled)
	}
	// Wrong nonce -> rejected_stale.
	bad := parsed
	bad.Nonce = strings.Repeat("0", 32)
	if r := Compile(env, bad, interrupt, env.Target); r.Outcome != OutcomeRejectedStale {
		t.Fatalf("expected stale got %s", r.Outcome)
	}
	// Wrong target -> rejected_target.
	bind := CommandTarget{Kind: TargetIssue, ID: "999"}
	if r := Compile(env, parsed, interrupt, bind); r.Outcome != OutcomeRejectedTarget {
		t.Fatalf("expected target got %s", r.Outcome)
	}
	// Action not in options -> rejected_option (e.g. ask on design_approval).
	ask, _ := ParseCommand("/sift ask " + validRunID() + " " + validNonce() + " why")
	if r := Compile(env, ask, interrupt, env.Target); r.Outcome != OutcomeRejectedOption {
		t.Fatalf("expected option got %s", r.Outcome)
	}
}

func TestCompileNoCurrentInterrupt(t *testing.T) {
	env := baseEnv(SourceForgeComment, "c1")
	parsed, _ := ParseCommand(env.Comment.Body)
	if r := Compile(env, parsed, nil, env.Target); r.Outcome != OutcomeRejectedTarget {
		t.Fatalf("expected target got %s", r.Outcome)
	}
	closed := openInterrupt(ReasonDesignApproval, []string{"approve"})
	closed.Status = StatusClosed
	if r := Compile(env, parsed, closed, env.Target); r.Outcome != OutcomeRejectedTarget {
		t.Fatalf("expected target for closed got %s", r.Outcome)
	}
}

func TestCompileStartupStallApproveRejected(t *testing.T) {
	// startup_stall options are only retry/reject/hold; approve must reject.
	interrupt := openInterrupt(ReasonStartupStall, []string{"retry", "reject", "hold"})
	env := baseEnv(SourceForgeComment, "c1")
	parsed, _ := ParseCommand("/sift approve " + validRunID() + " " + validNonce())
	if r := Compile(env, parsed, interrupt, env.Target); r.Outcome != OutcomeRejectedOption {
		t.Fatalf("startup_stall approve must be rejected_option, got %s", r.Outcome)
	}
	// retry/reject/hold are accepted.
	for _, body := range []string{
		"/sift retry " + validRunID() + " " + validNonce(),
		"/sift reject " + validRunID() + " " + validNonce(),
		"/sift hold " + validRunID() + " " + validNonce() + " 1h",
	} {
		p, _ := ParseCommand(body)
		if r := Compile(env, p, interrupt, env.Target); r.Outcome != OutcomeApplied {
			t.Fatalf("startup_stall %s expected applied got %s", p.Action, r.Outcome)
		}
	}
}

func TestCompileStartupStallProbeInProgress(t *testing.T) {
	interrupt := openInterrupt(ReasonStartupStall, []string{"retry", "reject", "hold"})
	interrupt.DispatchState = DispatchProbeInProgress
	env := baseEnv(SourceForgeComment, "c1")
	parsed, _ := ParseCommand("/sift retry " + validRunID() + " " + validNonce())
	if r := Compile(env, parsed, interrupt, env.Target); r.Outcome != OutcomeProbeInProgress {
		t.Fatalf("expected probe_in_progress got %s", r.Outcome)
	}
}

func TestCompileApprovalLabelCutoff(t *testing.T) {
	interrupt := openInterrupt(ReasonDesignApproval, []string{"approve", "reject", "hold"})
	env := baseEnv(SourceApprovalLabel, "l1")
	// No cutoff: position 100 is current.
	if r := Compile(env, ParsedCommand{}, interrupt, env.Target); r.Outcome != OutcomeApplied {
		t.Fatalf("expected applied got %s", r.Outcome)
	}
	// Cutoff at 100: position 100 is NOT > 100 -> rejected_stale.
	c := "100"
	interrupt.ApprovalLabelCutoffPosition = &c
	if r := Compile(env, ParsedCommand{}, interrupt, env.Target); r.Outcome != OutcomeRejectedStale {
		t.Fatalf("equal position must reject, got %s", r.Outcome)
	}
	// Position 99 < cutoff 100 -> rejected_stale.
	ninetyNine := "99"
	env99 := env
	env99.LabelPosition = &ninetyNine
	if r := Compile(env99, ParsedCommand{}, interrupt, env.Target); r.Outcome != OutcomeRejectedStale {
		t.Fatalf("earlier position must reject, got %s", r.Outcome)
	}
	// Position 101 > cutoff 100 -> applied.
	one := "101"
	env101 := env
	env101.LabelPosition = &one
	if r := Compile(env101, ParsedCommand{}, interrupt, env.Target); r.Outcome != OutcomeApplied {
		t.Fatalf("later position must apply, got %s", r.Outcome)
	}
}

func TestCompileHoldMaxDuration(t *testing.T) {
	interrupt := openInterrupt(ReasonAgentBlocked, []string{"ask", "retry", "reject", "hold"})
	interrupt.HoldMaxDurationMS = 3_600_000 // 1h
	env := baseEnv(SourceForgeComment, "c1")
	// 30m ok.
	p, _ := ParseCommand("/sift hold " + validRunID() + " " + validNonce() + " 30m")
	if r := Compile(env, p, interrupt, env.Target); r.Outcome != OutcomeApplied {
		t.Fatalf("30m within 1h expected applied got %s", r.Outcome)
	}
	// 2h exceeds -> rejected_option.
	p, _ = ParseCommand("/sift hold " + validRunID() + " " + validNonce() + " 2h")
	if r := Compile(env, p, interrupt, env.Target); r.Outcome != OutcomeRejectedOption {
		t.Fatalf("2h over 1h expected option got %s", r.Outcome)
	}
}

func TestNewEventAckCanonical(t *testing.T) {
	env := baseEnv(SourceForgeComment, "c1")
	ev := NewEvent(env, OutcomeApplied, ActionApprove, "run-1", "int-1", "abcdef0123456789abcdef0123456789", "")
	b, err := ev.CanonicalBytes()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	var round CommandEventV1
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Outcome != OutcomeApplied || *round.Action != ActionApprove || *round.RunID != "run-1" {
		t.Fatalf("round-trip lost fields: %s", b)
	}
	// rejected_syntax carries null action/run/interrupt (closed schema keeps
	// the field with a JSON null rather than omitting it).
	syn := NewEvent(env, OutcomeRejectedSyntax, "", "", "", "", "")
	sb, err := syn.CanonicalBytes()
	if err != nil {
		t.Fatalf("canonical syntax: %v", err)
	}
	if !strings.Contains(string(sb), "\"action\":null") || !strings.Contains(string(sb), "\"run_id\":null") {
		t.Fatalf("rejected_syntax must carry null action/run: %s", sb)
	}
	// Ack from final event.
	ack := NewAck("evt-1", ev)
	ab, err := ack.CanonicalBytes()
	if err != nil {
		t.Fatalf("canonical ack: %v", err)
	}
	if !strings.Contains(string(ab), "\"command_event_id\":\"evt-1\"") {
		t.Fatalf("ack missing ref: %s", ab)
	}
	// Pending outcome cannot produce an ack.
	pending := NewAck("evt-1", CommandEventV1{Outcome: OutcomeRetryPending})
	if _, err := pending.CanonicalBytes(); err == nil {
		t.Fatal("pending outcome must not produce ack")
	}
}

func TestStageKeys(t *testing.T) {
	if got := EventStageKey("K", StageInitial); got != "command:K:initial" {
		t.Fatalf("stage key %q", got)
	}
	if got := AckOperationKey("K"); got != "command:K:ack" {
		t.Fatalf("ack key %q", got)
	}
	if s := FinalStageForOutcome(OutcomeAbsenceUnconfirmed); s != StageFinalProbeFailed {
		t.Fatalf("absence stage %q", s)
	}
	if s := FinalStageForOutcome(OutcomeApplied); s != StageFinalProbeSucceeded {
		t.Fatalf("applied stage %q", s)
	}
	if s := FinalStageForOutcome(OutcomeRejectedSyntax); s != "" {
		t.Fatalf("non-retry stage must be empty, got %q", s)
	}
}
