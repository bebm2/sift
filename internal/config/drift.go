package config

import (
	"os"
	"sync"
	"time"
)

// DriftStatus is the outcome of a single drift check.
type DriftStatus int

const (
	// DriftNone: the on-disk file's effective hash matches the startup hash.
	DriftNone DriftStatus = iota
	// DriftDetected: the effective hash differs from the startup hash. The
	// running config is unchanged; the caller should append a
	// config_drift_detected security event (once per drift episode) and have
	// doctor report a warning (config.md §4).
	DriftDetected
	// DriftUndecodable: the file changed and can no longer be parsed or
	// validated. Treated as drift (something changed) but the new content is
	// not applied; the error is surfaced for diagnostics.
	DriftUndecodable
)

// DriftResult is returned by [DriftChecker.Check].
type DriftResult struct {
	Status      DriftStatus
	CurrentHash string
	// NewEvent is true exactly on the check that transitions into a drifted
	// state. While drift persists, subsequent checks report DriftDetected but
	// NewEvent=false ("只追加一次"). Restoring the original hash clears the
	// state, so a later drift emits the event once again.
	NewEvent bool
	// Err carries the decode/normalize error for DriftUndecodable.
	Err error
}

// DriftChecker watches config.yaml for on-disk changes after startup
// (config.md §4). It never applies new content: V0 does not hot-reload global
// config (§1.3). On a hash mismatch it flags drift for a single security event
// and a doctor warning; restoring the original hash clears the warning but
// keeps the historical event.
//
// The caller schedules Check at scheduler.config_drift_check_interval.
type DriftChecker struct {
	home        Home
	startupHash string
	startupSrc  SourceInfo

	mu     sync.Mutex
	active bool // a drift episode is currently outstanding
}

// NewDriftChecker constructs a checker anchored to the startup snapshot.
func NewDriftChecker(home Home, startupHash string, startupSrc SourceInfo) *DriftChecker {
	return &DriftChecker{home: home, startupHash: startupHash, startupSrc: startupSrc}
}

// Check compares the current on-disk file against the startup snapshot. It
// short-circuits on identical existence/mtime/size; only a candidate change
// triggers the hash recompute. now is accepted for the injected-clock
// convention even though the fingerprint is time-independent.
func (d *DriftChecker) Check(_ time.Time) DriftResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	path := ConfigPath(d.home)
	info, err := os.Stat(path)
	present := err == nil

	// Fast path: same existence, mtime and size as startup ⇒ byte-identical,
	// no drift. Clears any outstanding warning.
	if present == d.startupSrc.Present &&
		present &&
		info.Size() == d.startupSrc.Size &&
		sameMillis(info.ModTime(), d.startupSrc.MTime) {
		d.active = false
		return DriftResult{Status: DriftNone, CurrentHash: d.startupHash}
	}
	if !present && !d.startupSrc.Present {
		// Still absent, still nothing.
		d.active = false
		return DriftResult{Status: DriftNone, CurrentHash: d.startupHash}
	}

	// Candidate change: recompute the effective hash. Any failure to read,
	// parse or normalize is drift (the file changed into something unusable)
	// but the running config is untouched.
	snap, err := Load(d.home, time.Time{})
	if err != nil {
		// Only the first transition into drift emits the event.
		nev := !d.active
		d.active = true
		return DriftResult{Status: DriftUndecodable, NewEvent: nev, Err: err}
	}
	if snap.Hash != d.startupHash {
		nev := !d.active
		d.active = true
		return DriftResult{Status: DriftDetected, CurrentHash: snap.Hash, NewEvent: nev}
	}
	// Reformatted but same effective config: not drift.
	d.active = false
	return DriftResult{Status: DriftNone, CurrentHash: snap.Hash}
}

// sameMillis reports whether two times are equal at millisecond resolution,
// guarding against filesystem mtime granularity differences between the load
// host and the check host.
func sameMillis(a, b time.Time) bool {
	return a.Truncate(time.Millisecond).Equal(b.Truncate(time.Millisecond))
}
