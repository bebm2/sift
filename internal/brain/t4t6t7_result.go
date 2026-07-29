package brain

import (
	"fmt"

	"github.com/miaoxiaoyong/sift/internal/decode"
)

// T4FallbackOutput is the complete frozen skeleton rendered as a closed result.
func T4FallbackOutput(in T4Input) []byte {
	points := append([]string(nil), in.Interrupt.BriefFragments...)
	if len(points) > 3 {
		points = points[:3]
	}
	options := make([]string, len(in.Interrupt.CandidateOptions))
	for i := range in.Interrupt.CandidateOptions {
		options[i] = in.Interrupt.CandidateOptions[i].ID
	}
	conclusion := in.Interrupt.BriefFragments[0]
	recommended := options[0]
	o, err := decode.Canonical(T4Output{Headline: &in.Interrupt.FallbackHeadline, Conclusion: &conclusion, KeyPoints: &points, RecommendedOptionID: &recommended, Options: &options})
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

func T4ResultFromCall(result CallResult, in T4Input) (T4Output, BrainSource, error) {
	var out T4Output
	if result.Status == "valid" {
		if err := decode.Decode(result.Output, &out, decode.Closed); err != nil {
			return out, BrainSource{}, err
		}
		return out, brainSource(result), nil
	}
	if result.Status != "fallback" {
		return out, BrainSource{}, fmt.Errorf("brain: T4 call %s is not terminal", result.CallID)
	}
	if err := decode.Decode(T4FallbackOutput(in), &out, decode.Closed); err != nil {
		return out, BrainSource{}, err
	}
	return out, fallbackSource(result, "T4"), nil
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

// T7ResultFromCall returns no proposal on fallback; callers must not create a draft.
func T7ResultFromCall(result CallResult, aggregateKey string, evidenceIDs []string) (T7Output, BrainSource, error) {
	var out T7Output
	if result.Status == "valid" {
		if err := decode.Decode(result.Output, &out, decode.Closed); err != nil {
			return out, BrainSource{}, err
		}
		if _, err := T7Contract(aggregateKey, evidenceIDs).ValidateOutput(result.Output); err != nil {
			return out, BrainSource{}, err
		}
		return out, brainSource(result), nil
	}
	if result.Status != "fallback" {
		return out, BrainSource{}, fmt.Errorf("brain: T7 call %s is not terminal", result.CallID)
	}
	return out, fallbackSource(result, "T7"), nil
}

func brainSource(r CallResult) BrainSource {
	return BrainSource{Kind: "brain", LogicalCallID: r.CallID, PromptVersion: r.PromptVersion, OutputSchemaVersion: r.OutputSchemaVersion}
}
func fallbackSource(r CallResult, touchpoint string) BrainSource {
	return BrainSource{Kind: "fallback", LogicalCallID: r.CallID, Version: touchpoint + "/fallback/v1", Reason: fallbackReason(r.FallbackReason)}
}
