//go:build darwin

package runtime

import (
	"context"
)

// PlatformProcessInspector independently reads Darwin kinfo/proc_info and the
// wrapper's owner-only control file (control-plane.md process identity).
// Missing or incomplete evidence produces a live, incomplete observation so
// Terminator fails closed without signalling.
type PlatformProcessInspector struct{}

func (PlatformProcessInspector) Observe(ctx context.Context, want ProcessIdentity) (ProcessObservation, error) {
	if err := ctx.Err(); err != nil {
		return ProcessObservation{}, err
	}
	kp, err := darwinKinfo(want.PID)
	if err != nil {
		if darwinProcessAbsent(want.PID) {
			return ProcessObservation{}, nil
		}
		return ProcessObservation{Exists: true}, nil
	}
	if int(kp.Proc.P_pid) != want.PID || kp.Eproc.Pgid <= 0 {
		return ProcessObservation{Exists: true}, nil
	}
	executable, err := darwinPIDPath(want.PID)
	if err != nil {
		return ProcessObservation{Exists: true}, nil
	}
	observation := ProcessObservation{Exists: true, ProcessIdentity: ProcessIdentity{
		PID: want.PID, PGID: int(kp.Eproc.Pgid), StartedAtMS: darwinStartMS(kp), Executable: executable,
	}}
	observation.ControlNonceHash = controlNonceHash(want.ControlPath)
	return observation, nil
}
