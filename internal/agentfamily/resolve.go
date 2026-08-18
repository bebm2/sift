package agentfamily

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SecretsFilePath is the per-agent secrets file path convention shared by
// the setup wizard (which writes it) and the launch worker (which reads it,
// specs/agentfamily.md §4).
func SecretsFilePath(dir, agentID string) string {
	return filepath.Join(dir, agentID+".env")
}

// LaunchOverrides is the subset of one config.yaml agent instance this
// package resolves against its family: the family reference plus the
// model/thinking override values (specs/agentfamily.md §4). Executable,
// args and launch_env stay config.yaml's own contract; only the
// family-derived additions flow through here.
type LaunchOverrides struct {
	FamilyID string
	Model    string
	Thinking string
}

// overrideOrder is fixed so two resolutions of the same inputs always
// append flags in the same order, keeping the topology qualification key
// and the resumed-bootstrap equality check (launchworker) stable.
var overrideOrder = []string{"model", "thinking"}

// ResolveArgs appends o's model/thinking overrides, rendered through the
// matching family's run.flags, to baseArgs. o.FamilyID empty is a no-op:
// baseArgs comes back unchanged, so callers can call this unconditionally
// even for agents that never reference a family.
//
// A non-empty FamilyID absent from families, or a non-empty Model/Thinking
// the resolved family does not map under run.flags, is a fail-closed error
// (specs/agentfamily.md §4): an override that cannot be honored must not
// launch silently without it.
func ResolveArgs(families map[string]*Family, o LaunchOverrides, baseArgs []string) ([]string, error) {
	if o.FamilyID == "" {
		return baseArgs, nil
	}
	family, ok := families[o.FamilyID]
	if !ok {
		return nil, fmt.Errorf("agentfamily: unknown family %q", o.FamilyID)
	}
	values := map[string]string{"model": o.Model, "thinking": o.Thinking}
	args := append([]string(nil), baseArgs...)
	for _, name := range overrideOrder {
		value := values[name]
		if value == "" {
			continue
		}
		frag, ok := family.Flag(name, value)
		if !ok {
			return nil, fmt.Errorf("agentfamily %s: does not support %q override", o.FamilyID, name)
		}
		args = append(args, frag...)
	}
	return args, nil
}

// ResolveLaunchEnv builds the Agent process environment without mutating
// base (specs/agentfamily.md §4):
//
//  1. start from base (the frozen HOME/PATH launch_env)
//  2. overlay the init-time secrets file, if any
//  3. overlay the family's declared config.files JSON `env` block, if any
//     — live file wins on overlapping names, so a tool that rewrites
//     ~/.claude/settings.json (e.g. CC Switch) takes effect on the next
//     launch without re-running sift init
//
// family nil, or a family with no config.files, skips step 3. A missing
// secrets file or missing config file is not an error. A present but
// unreadable/malformed config file is fail-closed: the user is clearly
// trying to drive the Agent from that file.
func ResolveLaunchEnv(family *Family, secretsDir, agentID string, base map[string]string) (map[string]string, error) {
	merged := cloneMap(base)
	if secretsDir != "" {
		secrets, err := ReadSecretsFile(SecretsFilePath(secretsDir, agentID))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for k, v := range secrets {
			if merged == nil {
				merged = map[string]string{}
			}
			merged[k] = v
		}
	}
	if family == nil || len(family.Config.Files) == 0 {
		return merged, nil
	}
	home := ""
	if merged != nil {
		home = merged["HOME"]
	}
	live, err := readDeclaredConfigEnv(family, home)
	if err != nil {
		return nil, err
	}
	for k, v := range live {
		if merged == nil {
			merged = map[string]string{}
		}
		merged[k] = v
	}
	return merged, nil
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// readDeclaredConfigEnv loads JSON objects at family.Config.Files and
// keeps only names the family listed under Auth.Env or Config.Env.
// Paths starting with ~/ expand against home (the frozen launch_env HOME).
func readDeclaredConfigEnv(family *Family, home string) (map[string]string, error) {
	allow := map[string]bool{}
	for _, name := range family.Auth.Env {
		allow[name] = true
	}
	for _, name := range family.Config.Env {
		allow[name] = true
	}
	if len(allow) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, raw := range family.Config.Files {
		path, skip := expandHome(raw, home)
		if skip {
			// No frozen HOME: cannot locate ~/...; keep the init snapshot.
			continue
		}
		if !strings.HasSuffix(strings.ToLower(path), ".json") {
			// Codex config.toml and similar stay unread until a second
			// decoder exists (specs/agentfamily.md §4).
			continue
		}
		env, err := readJSONEnvBlock(path)
		if err != nil {
			return nil, err
		}
		for name, value := range env {
			if !allow[name] || value == "" {
				continue
			}
			out[name] = value
		}
	}
	return out, nil
}

func expandHome(path, home string) (resolved string, skip bool) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home == "" {
			return "", true
		}
		if path == "~" {
			return home, false
		}
		return filepath.Join(home, path[2:]), false
	}
	return path, false
}

// readJSONEnvBlock decodes the Claude-style {"env":{"NAME":"value"}}
// object from path. Missing file → empty map. Other files (toml, etc.)
// that are not JSON fail closed when present: guessing a second format
// is out of scope until a second family actually needs it.
func readJSONEnvBlock(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentfamily: read %s: %w", path, err)
	}
	var doc struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("agentfamily: decode %s: %w", path, err)
	}
	if len(doc.Env) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(doc.Env))
	for name, raw := range doc.Env {
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("agentfamily: %s env.%s must be a string", path, name)
		}
		out[name] = value
	}
	return out, nil
}
