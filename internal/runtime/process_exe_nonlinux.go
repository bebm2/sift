//go:build !linux && !darwin

package runtime

import "os"

// ProcessExecutable falls back to the invoking executable outside Linux and Darwin.
func ProcessExecutable(int) (string, error) {
	return os.Executable()
}
