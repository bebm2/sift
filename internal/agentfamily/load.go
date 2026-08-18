package agentfamily

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// loadFS decodes every *.yaml file directly under dir in fsys into a family,
// keyed by Family.ID. A duplicate id across two files is rejected: ids must
// be globally unique regardless of which file declared them.
func loadFS(fsys fs.FS, dir string) (map[string]*Family, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("agentfamily: read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	out := make(map[string]*Family, len(names))
	for _, name := range names {
		filePath := path.Join(dir, name)
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("agentfamily: read %s: %w", filePath, err)
		}
		f, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("agentfamily: %s: %w", filePath, err)
		}
		if prev, dup := out[f.ID]; dup {
			return nil, fmt.Errorf("agentfamily: duplicate id %q (%s and %s)", f.ID, prev.sourceFile, filePath)
		}
		f.sourceFile = filePath
		out[f.ID] = f
	}
	return out, nil
}

// LoadUserDir decodes every *.yaml file directly under dir on the real
// filesystem, the same way [Builtin] decodes the embedded set. A missing
// directory is not an error: it simply contributes no families (users who
// never created an override directory keep the built-in set unchanged).
func LoadUserDir(dir string) (map[string]*Family, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return map[string]*Family{}, nil
		}
		return nil, fmt.Errorf("agentfamily: stat %s: %w", dir, err)
	}
	return loadFS(os.DirFS(dir), ".")
}

// Load returns the effective family set: every built-in family, overlaid by
// any same-id file under userDir (full replacement, not a field merge — a
// user override must be a complete, valid family document) plus any new ids
// userDir defines. An empty userDir means "no override directory configured"
// and yields exactly the built-in set.
func Load(userDir string) (map[string]*Family, error) {
	out := Builtin()
	if userDir == "" {
		return out, nil
	}
	overrides, err := LoadUserDir(userDir)
	if err != nil {
		return nil, err
	}
	for id, f := range overrides {
		out[id] = f
	}
	return out, nil
}
