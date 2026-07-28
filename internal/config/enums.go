package config

// This file holds the closed-set enums of the global config. Each named string
// type implements [decode.Enumerated]; EnumValues is the single source for both
// the runtime membership check (run by the decode gateway) and the generated
// JSON Schema. V0 pins several enums to a single allowed value: any other
// value is rejected fail-closed rather than silently tolerated.

// TaskTransport is the closed set of agent task delivery channels
// (config.md §3.2).
type TaskTransport string

const (
	TaskTransportStdin TaskTransport = "stdin"
	TaskTransportFile  TaskTransport = "file"
)

// EnumValues satisfies [decode.Enumerated].
func (TaskTransport) EnumValues() []string {
	return []string{string(TaskTransportStdin), string(TaskTransportFile)}
}

// Backend selects the agent execution backend (config.md §3.2, §3.6).
type Backend string

const (
	BackendProcess Backend = "process"
	BackendTmux    Backend = "tmux"
)

// EnumValues satisfies [decode.Enumerated].
func (Backend) EnumValues() []string {
	return []string{string(BackendProcess), string(BackendTmux)}
}

// ForgeKind selects the forge platform adapter (config.md §3.3).
type ForgeKind string

const (
	ForgeKindGitHub ForgeKind = "github"
	ForgeKindGitLab ForgeKind = "gitlab"
)

// EnumValues satisfies [decode.Enumerated].
func (ForgeKind) EnumValues() []string {
	return []string{string(ForgeKindGitHub), string(ForgeKindGitLab)}
}

// defaultHost returns the platform's public host when a project omits
// forge.host (config.md §3.3).
func (k ForgeKind) defaultHost() string {
	switch k {
	case ForgeKindGitHub:
		return "github.com"
	case ForgeKindGitLab:
		return "gitlab.com"
	default:
		return ""
	}
}

// defaultCLI returns the platform's default CLI executable when a project
// omits forge.cli (config.md §3.3).
func (k ForgeKind) defaultCLI() string {
	switch k {
	case ForgeKindGitHub:
		return "gh"
	case ForgeKindGitLab:
		return "glab"
	default:
		return ""
	}
}

// BrainProtocol pins the Brain I/O protocol version (config.md §3.4). V0 admits
// only claude-json-v1; a protocol change must introduce a new value.
type BrainProtocol string

const (
	BrainProtocolClaudeJSONv1 BrainProtocol = "claude-json-v1"
)

// EnumValues satisfies [decode.Enumerated].
func (BrainProtocol) EnumValues() []string {
	return []string{string(BrainProtocolClaudeJSONv1)}
}

// ReviewPolicy is the project gate review default (config.md §3.11).
type ReviewPolicy string

const (
	ReviewPolicyAlways    ReviewPolicy = "always"
	ReviewPolicyRiskyOnly ReviewPolicy = "risky-only"
	ReviewPolicyNever     ReviewPolicy = "never"
)

// EnumValues satisfies [decode.Enumerated].
func (ReviewPolicy) EnumValues() []string {
	return []string{string(ReviewPolicyAlways), string(ReviewPolicyRiskyOnly), string(ReviewPolicyNever)}
}

// InterruptQuotaExceededAction pins the report quota-exceeded action
// (config.md §3.10). V0 admits only failure_review_once.
type InterruptQuotaExceededAction string

const (
	InterruptQuotaFailureReviewOnce InterruptQuotaExceededAction = "failure_review_once"
)

// EnumValues satisfies [decode.Enumerated].
func (InterruptQuotaExceededAction) EnumValues() []string {
	return []string{string(InterruptQuotaFailureReviewOnce)}
}
