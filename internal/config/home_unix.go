//go:build unix

package config

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// checkOwnedByCurrentUser rejects a home directory not owned by the daemon
// user (config.md §2.1.4). Unix-only: ownership is read from the inode stat.
func checkOwnedByCurrentUser(info fs.FileInfo, what string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// No inode metadata available (e.g. a virtual FS); cannot determine
		// ownership, so do not block startup on it.
		return nil
	}
	uid := os.Getuid()
	if int(stat.Uid) != uid {
		return fmt.Errorf("%s: not owned by current user (uid %d, file uid %d)", what, uid, stat.Uid)
	}
	return nil
}
