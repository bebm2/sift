package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/miaoxiaoyong/sift/internal/decode"
)

// SourceInfo records the on-disk facts about config.yaml at load time. The
// drift checker compares these first (existence, mtime, size) before the more
// expensive hash recompute (config.md §4).
type SourceInfo struct {
	Path    string
	Present bool
	MTime   time.Time // zero when absent
	Size    int64
}

// Snapshot is the immutable startup product: the effective Config, its
// canonical-JSON fingerprint, and the source facts needed for drift detection.
// The runtime uses only Config + Hash; CanonicalJSON is what storage persists
// in config_snapshots.canonical_json (config.md §4 step 8).
type Snapshot struct {
	Config        *Config
	Hash          string
	CanonicalJSON []byte
	Source        SourceInfo
}

// ConfigPath returns the config.yaml path under a resolved home.
func ConfigPath(home Home) string {
	return filepath.Join(home.Path, "config.yaml")
}

// Load is the single entry point by which global config enters the system
// (config.md §1.2, §4). It reads config.yaml once, converts YAML to JSON under
// the strict bridge, decodes via [decode.Closed], normalizes to the effective
// snapshot and computes the fingerprint. An absent file yields the full
// zero-config default (config.md §6 scenario 1). now is accepted for symmetry
// with the storage layer's injected clock; the fingerprint itself is
// time-independent.
func Load(home Home, _ time.Time) (*Snapshot, error) {
	path := ConfigPath(home)
	src := SourceInfo{Path: path}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		src.Present = true
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("config: stat %s: %w", path, statErr)
		}
		src.MTime = info.ModTime()
		src.Size = info.Size()

		jsonBytes, err := YAMLToJSON(data)
		if err != nil {
			return nil, err
		}
		raw := &RawConfig{}
		if err := decode.Decode(jsonBytes, raw, decode.Closed); err != nil {
			return nil, fmt.Errorf("config: decode %s: %w", path, err)
		}
		return finalize(raw, src)

	case errors.Is(err, fs.ErrNotExist):
		// Absent file ⇒ full defaults. No decode path: there is nothing to
		// close-validate, and version is implicit (config.md §6 scenario 1).
		return finalize(&RawConfig{}, src)

	default:
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
}

// finalize runs normalization + fingerprinting shared by the present- and
// absent-file paths.
func finalize(raw *RawConfig, src SourceInfo) (*Snapshot, error) {
	cfg, err := Normalize(raw)
	if err != nil {
		return nil, err
	}
	hash, canonical, err := Fingerprint(cfg)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Config: cfg, Hash: hash, CanonicalJSON: canonical, Source: src}, nil
}
