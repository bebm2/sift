package config

import "path/filepath"

// isAbsPath reports whether p is an absolute path. It is a thin wrapper kept
// in-package so config tests can stub the filesystem-independent rule without
// importing filepath at call sites.
func isAbsPath(p string) bool {
	return filepath.IsAbs(p)
}

// cleanPath canonicalizes p with filepath.Clean (config.md §2.1.3).
func cleanPath(p string) string {
	return filepath.Clean(p)
}
