//go:build darwin

package runtime

// ProcessExecutable returns the proc_info pid path used by
// PlatformProcessInspector.
func ProcessExecutable(pid int) (string, error) {
	return darwinPIDPath(pid)
}
