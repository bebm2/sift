package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File-mode contract (config.md §2.1). The home dir and config.yaml are
// owner-only; anything wider refuses startup and is never auto-corrected.
const (
	HomeDirMode    os.FileMode = 0o700
	ConfigFileMode os.FileMode = 0o600
)

// Home is the resolved SIFT_HOME. Path is the cleaned, stable external path
// used for every file under it; Resolved is the symlink-resolved real path
// recorded for diagnostics when the directory already exists (config.md §2.1.3).
type Home struct {
	Path     string
	Resolved string
}

// ResolveHome resolves SIFT_HOME from the process environment: a non-empty
// SIFT_HOME (which must be absolute) wins, otherwise $HOME/.sift. The result is
// filepath.Clean-ed. A user home that cannot be determined refuses startup
// (config.md §2.1.4).
func ResolveHome() (Home, error) {
	return ResolveHomeWith(os.UserHomeDir)
}

// ResolveHomeWith is ResolveHome with an injectable user-home resolver, for
// tests that must not touch the real $HOME.
func ResolveHomeWith(userHome func() (string, error)) (Home, error) {
	if env := os.Getenv("SIFT_HOME"); env != "" {
		if !filepath.IsAbs(env) {
			return Home{}, fmt.Errorf("SIFT_HOME must be an absolute path, got %q", env)
		}
		return makeHome(env), nil
	}
	hd, err := userHome()
	if err != nil {
		return Home{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	return makeHome(filepath.Join(hd, ".sift")), nil
}

func makeHome(p string) Home {
	clean := filepath.Clean(p)
	h := Home{Path: clean}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			h.Resolved = filepath.Clean(resolved)
		}
	}
	return h
}

// EnsureHomeLayout verifies (or initially creates) the SIFT_HOME directory and,
// when present, config.yaml, against the §2.1 permission and ownership
// contract. It refuses startup on:
//   - a home path that exists but is not a directory,
//   - a home directory or config.yaml whose mode grants group/other access,
//   - a home directory not owned by the current user.
//
// It never relaxes permissions: an existing too-wide mode is an error, not a
// chmod target (config.md §2.1.4).
func EnsureHomeLayout(home Home) error {
	info, err := os.Stat(home.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(home.Path, HomeDirMode); err != nil {
			return fmt.Errorf("create SIFT_HOME %s: %w", home.Path, err)
		}
		// Re-verify the actual on-disk mode; never trust umask alone
		// (config.md §2.1 final paragraph).
		if err := os.Chmod(home.Path, HomeDirMode); err != nil {
			return fmt.Errorf("enforce SIFT_HOME mode: %w", err)
		}
		info, err = os.Stat(home.Path)
		if err != nil {
			return fmt.Errorf("stat SIFT_HOME after create: %w", err)
		}
	case err != nil:
		return fmt.Errorf("stat SIFT_HOME %s: %w", home.Path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("SIFT_HOME %s is not a directory", home.Path)
	}
	if err := checkOwnerExclusive(info, home.Path); err != nil {
		return err
	}
	if err := checkOwnedByCurrentUser(info, home.Path); err != nil {
		return err
	}

	cfgPath := filepath.Join(home.Path, "config.yaml")
	if cinfo, err := os.Stat(cfgPath); err == nil {
		if cinfo.IsDir() {
			return fmt.Errorf("config.yaml (%s) is a directory", cfgPath)
		}
		if err := checkOwnerExclusive(cinfo, cfgPath); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat config.yaml: %w", err)
	}
	return nil
}

// checkOwnerExclusive rejects any mode bit granting access beyond the owner
// (config.md §2.1.4: "权限宽于属主访问时，daemon 拒绝启动").
func checkOwnerExclusive(info fs.FileInfo, what string) error {
	if info.Mode()&0o077 != 0 {
		return fmt.Errorf("%s: permissions too open (%s); must be owner-only (no group/other access)", what, info.Mode())
	}
	return nil
}
