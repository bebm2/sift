package agentfamily

import "path/filepath"

// Match finds the family whose Match list names executable's base name
// (e.g. "/usr/local/bin/claude" matches a family with match: [claude]).
// When more than one family lists the same name, the one whose id sorts
// first wins so the result is deterministic; families is expected to come
// from [Load] or [Builtin], where ids are already unique.
func Match(families map[string]*Family, executable string) (*Family, bool) {
	base := filepath.Base(executable)
	var best *Family
	for _, f := range families {
		for _, m := range f.Match {
			if m != base {
				continue
			}
			if best == nil || f.ID < best.ID {
				best = f
			}
		}
	}
	return best, best != nil
}
