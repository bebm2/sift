package config

import (
	"errors"
	"testing"
)

// Scheduling hard guards (config.md §1.4): unknown agent rejected, per-agent
// max_concurrent, global max_concurrent_total, project exclusive mutex.

func newGuardCfg() *Config {
	cfg := DefaultConfig()
	cfg.Runtime.MaxConcurrentTotal = 2
	cfg.Runtime.DefaultAgentMaxConcurrent = 1
	cfg.Agents = []Agent{
		{ID: "a", Executable: "echo", MaxConcurrent: 1},
		{ID: "b", Executable: "echo", MaxConcurrent: 2},
	}
	return cfg
}

func TestGuardRejectsUnknownAgent(t *testing.T) {
	g := NewGuard(newGuardCfg())
	if _, err := g.Acquire("ghost", "p1", false); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("expected ErrUnknownAgent, got %v", err)
	}
}

func TestGuardAgentMaxConcurrent(t *testing.T) {
	g := NewGuard(newGuardCfg())
	rel, err := g.Acquire("a", "p1", false)
	if err != nil {
		t.Fatal(err)
	}
	// Agent a has max_concurrent 1: second acquire must fail.
	if _, err := g.Acquire("a", "p1", false); !errors.Is(err, ErrAgentAtCapacity) {
		t.Fatalf("expected ErrAgentAtCapacity, got %v", err)
	}
	// Agent b still has headroom.
	if _, err := g.Acquire("b", "p1", false); err != nil {
		t.Fatalf("agent b must be admitted: %v", err)
	}
	rel()
	// After releasing a, it is admissible again.
	if _, err := g.Acquire("a", "p1", false); err != nil {
		t.Fatalf("agent a must be readmitted after release: %v", err)
	}
}

func TestGuardTotalMaxConcurrent(t *testing.T) {
	cfg := newGuardCfg()
	cfg.Runtime.MaxConcurrentTotal = 1
	cfg.Agents[0].MaxConcurrent = 5 // agent headroom exceeds total
	g := NewGuard(cfg)
	rel, err := g.Acquire("a", "p1", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Acquire("a", "p2", false); !errors.Is(err, ErrTotalAtCapacity) {
		t.Fatalf("expected ErrTotalAtCapacity, got %v", err)
	}
	rel()
}

func TestGuardProjectExclusiveMutex(t *testing.T) {
	cfg := newGuardCfg()
	cfg.Agents[0].MaxConcurrent = 5
	cfg.Agents[1].MaxConcurrent = 5
	cfg.Runtime.MaxConcurrentTotal = 10
	g := NewGuard(cfg)
	rel, err := g.Acquire("a", "proj-x", true)
	if err != nil {
		t.Fatal(err)
	}
	// Same project held exclusively: any other attempt rejected, even on
	// a different agent.
	if _, err := g.Acquire("b", "proj-x", true); !errors.Is(err, ErrProjectBusy) {
		t.Fatalf("expected ErrProjectBusy, got %v", err)
	}
	// A different project is unaffected.
	if _, err := g.Acquire("a", "proj-y", true); err != nil {
		t.Fatalf("other project must be admitted: %v", err)
	}
	rel()
	// After release, proj-x is available again.
	if _, err := g.Acquire("b", "proj-x", true); err != nil {
		t.Fatalf("proj-x must be available after release: %v", err)
	}
}

func TestGuardReleaseIdempotentSafe(t *testing.T) {
	g := NewGuard(newGuardCfg())
	rel, _ := g.Acquire("a", "p1", false)
	rel()
	// Calling release twice must not underflow counters.
	rel()
	if u := g.Usage(); u.TotalInUse != 0 {
		t.Fatalf("total in use after double release = %d", u.TotalInUse)
	}
}

func TestGuardAgentLookup(t *testing.T) {
	g := NewGuard(newGuardCfg())
	a, err := g.Agent("a")
	if err != nil || a.ID != "a" {
		t.Fatalf("Agent(a) = %+v, %v", a, err)
	}
	if _, err := g.Agent("nope"); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("expected ErrUnknownAgent, got %v", err)
	}
}
