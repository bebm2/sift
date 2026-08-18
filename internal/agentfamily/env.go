package agentfamily

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
)

// EnvSnapshot is what a family's Auth/Config env names resolve to in one
// concrete process environment at one point in time (typically `sift init`
// or `sift agent add`, run in the user's interactive shell). Secrets and
// NonSecret are kept separate because they are stored differently: Secrets
// belongs in the 0600 secrets file (env.go below); NonSecret is safe to fold
// into config.yaml or logs.
type EnvSnapshot struct {
	Secrets   map[string]string
	NonSecret map[string]string
}

// Lookup abstracts os.LookupEnv so tests never depend on the real process
// environment.
type Lookup func(name string) (value string, ok bool)

// SnapshotEnv resolves f's declared Auth.Env and Config.Env names through
// lookup, keeping only the names that are actually set to a non-empty value.
// A declared-but-absent name is not an error: most of these agents also
// support login flows that never touch the environment.
func SnapshotEnv(f *Family, lookup Lookup) EnvSnapshot {
	return EnvSnapshot{
		Secrets:   collectPresent(f.Auth.Env, lookup),
		NonSecret: collectPresent(f.Config.Env, lookup),
	}
}

func collectPresent(names []string, lookup Lookup) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		if v, ok := lookup(name); ok && v != "" {
			out[name] = v
		}
	}
	return out
}

// WriteSecretsFile persists env in the same KEY=VALUE line format normalized
// config already reads for its own bridges, sorted by key for a
// deterministic byte-for-byte result. The file is created (or truncated)
// owner-only (0600); callers are responsible for the containing directory's
// permissions (SIFT_HOME layout, config.md §2.1).
func WriteSecretsFile(path string, env map[string]string) error {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	for _, name := range names {
		value := env[name]
		if strings.ContainsAny(name, "=\n\x00") {
			return fmt.Errorf("agentfamily: secrets file key %q contains =, newline or NUL", name)
		}
		if strings.ContainsAny(value, "\n\x00") {
			return fmt.Errorf("agentfamily: secrets file value for %q contains newline or NUL", name)
		}
		buf.WriteString(name)
		buf.WriteByte('=')
		buf.WriteString(value)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("agentfamily: write secrets file %s: %w", path, err)
	}
	return nil
}

// ReadSecretsFile parses a file [WriteSecretsFile] produced. It rejects lines
// without a "=" separator so a malformed file is a load-time error, not a
// silently dropped credential.
func ReadSecretsFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agentfamily: read secrets file %s: %w", path, err)
	}
	out := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("agentfamily: secrets file %s:%d: missing '='", path, lineNo)
		}
		out[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("agentfamily: scan secrets file %s: %w", path, err)
	}
	return out, nil
}
