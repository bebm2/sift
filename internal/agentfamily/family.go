// Package agentfamily implements the family contract of
// specs/agentfamily.md: how to drive a coding agent CLI non-interactively,
// which flags carry model/thinking overrides, and which environment
// variables/config paths carry login state or behavior-changing settings. A
// family is agent-software-level knowledge (one per CLI, e.g. "claude",
// "codex"); config.yaml's agents[] reference a family by id and hold only
// instance-specific overrides (issue #1024).
//
// Families never carry secret values themselves — only the *names* of the
// environment variables that might. Capturing and storing the actual values
// is [SnapshotEnv] and the secrets file helpers in env.go, not this file.
package agentfamily

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/xsift/sift/internal/config"
)

// idRe mirrors config.yaml's agent id grammar (config.md §3.2) so family ids
// and instance ids read the same way.
var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Family is the decoded, validated contract for one coding-agent CLI.
type Family struct {
	// ID identifies the family (e.g. "claude", "codex"); config.yaml's
	// agents[].family references this.
	ID string `json:"id"`
	// Match lists executable base names auto-detection recognizes as this
	// family (e.g. ["claude"]).
	Match []string `json:"match"`
	Run   RunSpec  `json:"run"`
	// Auth names environment variables that carry login credentials. Sift
	// never persists their values in config.yaml; see env.go.
	Auth AuthSpec `json:"auth"`
	// Config names environment variables/paths that change agent behavior
	// (base URL, model, custom config dirs) without being secrets.
	Config ConfigSpec `json:"config,omitempty"`

	// sourceFile is the loader-populated origin path, used only for
	// diagnostics (duplicate-id error messages); Parse itself leaves it
	// empty since a bare document has no file identity yet.
	sourceFile string
}

// RunSpec describes how to invoke the agent non-interactively.
type RunSpec struct {
	// Args are the default CLI arguments for a newly registered agent
	// instance (e.g. ["-p"] for claude). They seed agents[].args in
	// config.yaml; task delivery ({task_file} / task_transport) stays owned
	// by config.yaml's existing contract (config.md §3.2), not by this
	// package.
	Args []string `json:"args"`
	// VersionArgs probes the installed version (e.g. ["--version"]); empty
	// means the agent has no reliable version flag.
	VersionArgs []string `json:"version_args,omitempty"`
	// Flags maps an override name (e.g. "model", "thinking") to the argv
	// fragment that carries it, with exactly one "{value}" placeholder
	// (e.g. ["--model", "{value}"]). A name absent here means the family
	// does not support overriding it from config.yaml.
	Flags map[string][]string `json:"flags,omitempty"`
}

// AuthSpec names the environment variables that carry login credentials for
// this family (e.g. ANTHROPIC_API_KEY). Presence is a hint, not a guarantee:
// some of these agents also accept a config-file-based login.
type AuthSpec struct {
	Env []string `json:"env,omitempty"`
}

// ConfigSpec names non-secret signals that still change what the agent does:
// environment variables (e.g. ANTHROPIC_BASE_URL), config directories, and
// specific files. JSON files listed in Files are read at launch for an
// `env` object (specs/agentfamily.md §4); Dirs remain unused.
type ConfigSpec struct {
	Env   []string `json:"env,omitempty"`
	Dirs  []string `json:"dirs,omitempty"`
	Files []string `json:"files,omitempty"`
}

// placeholder is the single substitution token allowed in a Flags fragment.
const placeholder = "{value}"

// Parse decodes and validates one family document from YAML bytes. It reuses
// config's YAML→JSON bridge (config.YAMLToJSON) so family files get the same
// strict-document guarantees as config.yaml (single document, no duplicate
// keys, no non-string keys, no alias cycles), then rejects unknown JSON
// fields itself since families are not part of the config.yaml schema
// gateway.
func Parse(data []byte) (*Family, error) {
	jsonBytes, err := config.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("agentfamily: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.DisallowUnknownFields()
	f := &Family{}
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("agentfamily: decode: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// Validate enforces the closed shape Parse relies on: required fields
// present, ids well-formed, and every Flags fragment carrying exactly one
// placeholder.
func (f *Family) Validate() error {
	if f.ID == "" {
		return fmt.Errorf("agentfamily: id is required")
	}
	if !idRe.MatchString(f.ID) {
		return fmt.Errorf("agentfamily: id %q must match %s", f.ID, idRe.String())
	}
	if len(f.Match) == 0 {
		return fmt.Errorf("agentfamily %s: match must list at least one executable name", f.ID)
	}
	for _, m := range f.Match {
		if strings.TrimSpace(m) == "" {
			return fmt.Errorf("agentfamily %s: match entries must not be blank", f.ID)
		}
	}
	if len(f.Run.Args) == 0 {
		return fmt.Errorf("agentfamily %s: run.args must not be empty", f.ID)
	}
	names := make([]string, 0, len(f.Run.Flags))
	for name := range f.Run.Flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		frag := f.Run.Flags[name]
		if len(frag) == 0 {
			return fmt.Errorf("agentfamily %s: run.flags.%s must not be empty", f.ID, name)
		}
		count := 0
		for _, tok := range frag {
			count += strings.Count(tok, placeholder)
		}
		if count != 1 {
			return fmt.Errorf("agentfamily %s: run.flags.%s must contain exactly one %q placeholder, found %d", f.ID, name, placeholder, count)
		}
	}
	return nil
}

// Flag renders the argv fragment for overriding name with value, substituting
// the single "{value}" placeholder. ok is false when the family does not
// declare a mapping for name.
func (f *Family) Flag(name, value string) (args []string, ok bool) {
	frag, ok := f.Run.Flags[name]
	if !ok {
		return nil, false
	}
	out := make([]string, len(frag))
	for i, tok := range frag {
		out[i] = strings.ReplaceAll(tok, placeholder, value)
	}
	return out, true
}
