package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xsift/sift/internal/agentfamily"
	"github.com/xsift/sift/internal/config"
)

// agentFamiliesDir is where users may drop custom family YAML files to
// extend or override the built-in set (specs/agentfamily.md §3).
func agentFamiliesDir(home config.Home) string {
	return filepath.Join(home.Path, "agent-families")
}

// agentSecretsDir holds one 0600 file per agent id, each carrying that
// agent's captured auth/config environment, injected at launch instead of
// config.yaml (specs/agentfamily.md §4, issue #1024). The daemon reads this
// same directory (cmd/sift/daemon.go) via launchworker.Worker.SecretsDir.
func agentSecretsDir(home config.Home) string {
	return filepath.Join(home.Path, "agent-secrets")
}

// agentSecretsPath is the specific file for one agent id within
// agentSecretsDir.
func agentSecretsPath(home config.Home, id string) string {
	return agentfamily.SecretsFilePath(agentSecretsDir(home), id)
}

// loadSetupFamilies resolves the effective family set the wizard matches
// agents against: built-in families overlaid by any user overrides.
func loadSetupFamilies(home config.Home) (map[string]*agentfamily.Family, error) {
	families, err := agentfamily.Load(agentFamiliesDir(home))
	if err != nil {
		return nil, fmt.Errorf("load agent families: %w", err)
	}
	return families, nil
}

// syncAgentSecrets re-captures the auth/config environment for every agent
// entry in doc that resolved to a known family, reading through lookup (the
// wizard's own process environment — the user's interactive shell, issue
// #1024). An agent with no family, or whose family the current set no
// longer knows, is left alone: it keeps whatever secrets file it already
// had, since a family removed from the effective set says nothing about
// whether the agent's credentials changed.
func syncAgentSecrets(home config.Home, doc map[string]any, families map[string]*agentfamily.Family, lookup agentfamily.Lookup) error {
	for _, item := range list(doc, "agents") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		familyID, _ := m["family"].(string)
		if id == "" || familyID == "" {
			continue
		}
		family, ok := families[familyID]
		if !ok {
			continue
		}
		if err := writeAgentSecrets(home, id, family, lookup); err != nil {
			return err
		}
	}
	return nil
}

// writeAgentSecrets snapshots family's declared auth/config environment
// names through lookup and persists whatever is present. An empty snapshot
// removes any stale file from a previous run instead of leaving it behind
// with credentials the current shell no longer has.
func writeAgentSecrets(home config.Home, id string, family *agentfamily.Family, lookup agentfamily.Lookup) error {
	snap := agentfamily.SnapshotEnv(family, lookup)
	merged := make(map[string]string, len(snap.Secrets)+len(snap.NonSecret))
	for k, v := range snap.Secrets {
		merged[k] = v
	}
	for k, v := range snap.NonSecret {
		merged[k] = v
	}
	path := agentSecretsPath(home, id)
	if len(merged) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale agent secrets %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create agent secrets dir: %w", err)
	}
	if err := agentfamily.WriteSecretsFile(path, merged); err != nil {
		return err
	}
	return nil
}
