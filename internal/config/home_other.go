//go:build !unix

package config

import "io/fs"

// checkOwnedByCurrentUser is a no-op on non-unix platforms; the Sift build
// matrix is darwin/linux (unix), so this stub only keeps the package compiling
// elsewhere.
func checkOwnedByCurrentUser(_ fs.FileInfo, _ string) error {
	return nil
}
