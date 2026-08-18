//go:build darwin

package runtime

// ProcessStartedAtMS returns the kinfo-derived start time used by
// PlatformProcessInspector, so persisted wrapper identity can match Observe.
// A lookup failure returns 0 (fail closed) rather than wall clock: a guessed
// timestamp would not match a later kinfo observation.
func ProcessStartedAtMS(pid int) int64 {
	kp, err := darwinKinfo(pid)
	if err != nil {
		return 0
	}
	return darwinStartMS(kp)
}
