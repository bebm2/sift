package brain

import (
	"fmt"

	"github.com/miaoxiaoyong/sift/internal/decode"
)

// T4FallbackOutput preserves the complete frozen input skeleton. A fallback
// is not a reduced model-shaped brief: the Interrupt consumer needs all facts,
// verified links, and candidate option fields to call the deterministic emitter.
func T4FallbackOutput(in T4Input) []byte {
	o, err := decode.Canonical(in)
	if err != nil {
		panic(fmt.Sprintf("brain: T4 fallback must be canonical: %v", err))
	}
	return o
}

func T6FallbackOutput(in T6Input) []byte {
	delivery := T6Delivery("batch")
	if in.Candidate.Severity == "high" || in.Candidate.Severity == "critical" {
		delivery = "immediate"
	}
	channel := in.Candidate.DefaultChannelID
	downgrade := false
	rationale := "fallback"
	o, err := decode.Canonical(T6Output{Delivery: &delivery, ChannelID: &channel, SuggestedDowngrade: &downgrade, Rationale: &rationale})
	if err != nil {
		panic(fmt.Sprintf("brain: T6 fallback must be canonical: %v", err))
	}
	return o
}

// T4CallResult is a closed terminal union. Exactly one branch is populated.
type T4CallResult struct {
	Normal   *T4Output
	Fallback *T4Input
}

func T4ResultFromCall(result CallResult, in T4Input) (T4CallResult, BrainSource, error) {
	if result.Status == "valid" {
		var out T4Output
		if err := decode.Decode(result.Output, &out, decode.Closed); err != nil {
			return T4CallResult{}, BrainSource{}, err
		}
		return T4CallResult{Normal: &out}, brainSource(result), nil
	}
	if result.Status != "fallback" {
		return T4CallResult{}, BrainSource{}, fmt.Errorf("brain: T4 call %s is not terminal", result.CallID)
	}
	return T4CallResult{Fallback: &in}, fallbackSource(result, "T4"), nil
}

func T6ResultFromCall(result CallResult, in T6Input) (T6Output, BrainSource, error) {
	var out T6Output
	if result.Status == "valid" {
		if err := decode.Decode(result.Output, &out, decode.Closed); err != nil {
			return out, BrainSource{}, err
		}
		return out, brainSource(result), nil
	}
	if result.Status != "fallback" {
		return out, BrainSource{}, fmt.Errorf("brain: T6 call %s is not terminal", result.CallID)
	}
	if err := decode.Decode(T6FallbackOutput(in), &out, decode.Closed); err != nil {
		return out, BrainSource{}, err
	}
	return out, fallbackSource(result, "T6"), nil
}

// T7CallResult makes the fallback no-draft outcome explicit.
type T7CallResult struct {
	Proposal *T7Output
	NoDraft  bool
}

func T7ResultFromCall(result CallResult, aggregateKey string, evidenceIDs []string) (T7CallResult, BrainSource, error) {
	if result.Status == "valid" {
		var out T7Output
		if err := decode.Decode(result.Output, &out, decode.Closed); err != nil {
			return T7CallResult{}, BrainSource{}, err
		}
		if _, err := T7Contract(aggregateKey, "", nil, evidenceIDs).ValidateOutput(result.Output); err != nil {
			return T7CallResult{}, BrainSource{}, err
		}
		return T7CallResult{Proposal: &out}, brainSource(result), nil
	}
	if result.Status != "fallback" {
		return T7CallResult{}, BrainSource{}, fmt.Errorf("brain: T7 call %s is not terminal", result.CallID)
	}
	return T7CallResult{NoDraft: true}, fallbackSource(result, "T7"), nil
}

func brainSource(r CallResult) BrainSource {
	return BrainSource{Kind: "brain", LogicalCallID: r.CallID, PromptVersion: r.PromptVersion, OutputSchemaVersion: r.OutputSchemaVersion}
}
func fallbackSource(r CallResult, touchpoint string) BrainSource {
	return BrainSource{Kind: "fallback", LogicalCallID: r.CallID, Version: touchpoint + "/fallback/v1", Reason: fallbackReason(r.FallbackReason)}
}
