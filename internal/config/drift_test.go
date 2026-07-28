package config

import (
	"os"
	"testing"
	"time"
)

// Drift detection is warn-only: the running config never changes (config.md
// §4, H16). A mismatch raises one event; persistence does not raise another.

func TestDriftNoChange(t *testing.T) {
	home := tempHome(t)
	writeConfig(t, home, "version: 1\n")
	snap, err := Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dc := NewDriftChecker(home, snap.Hash, snap.Source)
	for i := 0; i < 3; i++ {
		r := dc.Check(time.Now())
		if r.Status != DriftNone {
			t.Fatalf("check %d: expected none, got %v", i, r.Status)
		}
		if r.NewEvent {
			t.Fatal("unchanged file must not raise event")
		}
	}
}

func TestDriftDetectedOnceThenRestored(t *testing.T) {
	home := tempHome(t)
	writeConfig(t, home, "version: 1\nruntime:\n  max_attempts: 3\n")
	snap, err := Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dc := NewDriftChecker(home, snap.Hash, snap.Source)

	// Mutate effective config on disk (different content length so the
	// mtime/size short-circuit cannot mask the change; values stay in range).
	rewrite(t, home, "version: 1\nruntime:\n  max_attempts: 20\n")
	r1 := dc.Check(time.Now())
	if r1.Status != DriftDetected || !r1.NewEvent {
		t.Fatalf("first drift must raise event once, got %+v", r1)
	}
	// Still drifted: no second event.
	r2 := dc.Check(time.Now())
	if r2.Status != DriftDetected || r2.NewEvent {
		t.Fatalf("persistent drift must not re-raise, got %+v", r2)
	}
	// Restore original effective config.
	rewrite(t, home, "version: 1\nruntime:\n  max_attempts: 3\n")
	r3 := dc.Check(time.Now())
	if r3.Status != DriftNone {
		t.Fatalf("restored file must clear drift, got %+v", r3)
	}
	// Drift again → event once again.
	rewrite(t, home, "version: 1\nruntime:\n  max_attempts: 15\n")
	r4 := dc.Check(time.Now())
	if r4.Status != DriftDetected || !r4.NewEvent {
		t.Fatalf("new drift episode must raise event once, got %+v", r4)
	}
}

func TestDriftReformatSameHashNoEvent(t *testing.T) {
	home := tempHome(t)
	writeConfig(t, home, "version: 1\nruntime: {max_attempts: 3}\n")
	snap, err := Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dc := NewDriftChecker(home, snap.Hash, snap.Source)
	// Same effective content, different whitespace.
	rewrite(t, home, "version: 1\nruntime:\n  max_attempts: 3\n")
	r := dc.Check(time.Now())
	if r.Status != DriftNone {
		t.Fatalf("reformatted-but-same config must not drift, got %+v", r)
	}
}

func TestDriftFileBecomesUndecodable(t *testing.T) {
	home := tempHome(t)
	writeConfig(t, home, "version: 1\n")
	snap, err := Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dc := NewDriftChecker(home, snap.Hash, snap.Source)
	rewrite(t, home, "version: 1\nruntime:\n  retry_multiplier: 99\n") // out of range
	r := dc.Check(time.Now())
	if r.Status != DriftUndecodable || !r.NewEvent {
		t.Fatalf("undecodable drift must raise once, got %+v", r)
	}
	if r.Err == nil {
		t.Fatal("expected underlying error")
	}
}

func TestDriftFileAppearsAfterAbsentStartup(t *testing.T) {
	home := tempHome(t) // no config.yaml
	snap, err := Load(home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dc := NewDriftChecker(home, snap.Hash, snap.Source)
	writeConfig(t, home, "version: 1\nruntime:\n  max_attempts: 7\n")
	r := dc.Check(time.Now())
	if r.Status != DriftDetected {
		t.Fatalf("file appearing post-startup is drift, got %+v", r)
	}
}

func rewrite(t *testing.T, home Home, yaml string) {
	t.Helper()
	path := ConfigPath(home)
	if err := os.WriteFile(path, []byte(yaml), ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, ConfigFileMode); err != nil {
		t.Fatal(err)
	}
	// Sleep briefly so mtime advances beyond the startup capture on coarse-mtime
	// filesystems; sameMillis tolerates the rest.
	time.Sleep(15 * time.Millisecond)
}
