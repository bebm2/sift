//go:build !linux && !darwin

package runtime

import "context"

// PlatformProcessInspector is deliberately fail-closed on platforms that have
// no native inspector (not Linux, not Darwin). It never supplies start time,
// executable or control-nonce proof, so Terminator will not signal.
type PlatformProcessInspector struct{ UnknownProcessInspector }

func (PlatformProcessInspector) Observe(ctx context.Context, want ProcessIdentity) (ProcessObservation, error) {
	return UnknownProcessInspector{}.Observe(ctx, want)
}
