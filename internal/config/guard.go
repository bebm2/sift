package config

import (
	"errors"
	"fmt"
	"sync"
)

// Scheduling hard-guard errors (config.md §1.4). They are returned by [Guard]
// and must cause the scheduler to reject the request, never to silently exceed
// a limit or admit an unknown agent.
var (
	ErrUnknownAgent    = errors.New("config: unknown agent")
	ErrAgentAtCapacity = errors.New("config: agent at max_concurrent")
	ErrTotalAtCapacity = errors.New("config: runtime at max_concurrent_total")
	ErrProjectBusy     = errors.New("config: project held exclusively")
)

// Guard enforces the scheduling hard guards of config.md §1.4:
//
//   - an unknown agent id is rejected outright,
//   - each agent's in-flight attempts are capped at its effective
//     max_concurrent (an omitted value inherited Runtime.DefaultAgentMaxConcurrent),
//   - the global in-flight count is capped at Runtime.MaxConcurrentTotal,
//   - a project may be held exclusively ("when needed") so two attempts do not
//     contend for the same run worktree.
//
// Acquire is atomic: either every guard passes and the slot is held, or no
// counter changes. The returned release func decrements exactly what Acquire
// reserved and is safe to call exactly once.
type Guard struct {
	cfg *Config

	mu                sync.Mutex
	agentInUse        map[string]int
	totalInUse        int
	exclusiveProjects map[string]bool
}

// NewGuard builds a Guard for an effective config. The config must already be
// normalized (every agent has a resolved MaxConcurrent).
func NewGuard(cfg *Config) *Guard {
	return &Guard{
		cfg:               cfg,
		agentInUse:        map[string]int{},
		exclusiveProjects: map[string]bool{},
	}
}

// Agent returns the effective agent definition for id, or [ErrUnknownAgent].
func (g *Guard) Agent(id string) (Agent, error) {
	for _, a := range g.cfg.Agents {
		if a.ID == id {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("%w: %q", ErrUnknownAgent, id)
}

// Acquire reserves one scheduling slot for agentID within projectID. When
// exclusiveProject is true the project is held exclusively until release: no
// other in-flight attempt for that project may be admitted. It returns a
// release function the caller MUST invoke when the slot is freed.
func (g *Guard) Acquire(agentID, projectID string, exclusiveProject bool) (Release, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	agent, err := g.Agent(agentID)
	if err != nil {
		return nil, err
	}
	if g.agentInUse[agentID] >= agent.MaxConcurrent {
		return nil, fmt.Errorf("%w: %q at %d/%d", ErrAgentAtCapacity, agentID, g.agentInUse[agentID], agent.MaxConcurrent)
	}
	if g.totalInUse >= g.cfg.Runtime.MaxConcurrentTotal {
		return nil, fmt.Errorf("%w: %d/%d", ErrTotalAtCapacity, g.totalInUse, g.cfg.Runtime.MaxConcurrentTotal)
	}
	if exclusiveProject && g.exclusiveProjects[projectID] {
		return nil, fmt.Errorf("%w: %q", ErrProjectBusy, projectID)
	}

	g.agentInUse[agentID]++
	g.totalInUse++
	heldExclusive := false
	if exclusiveProject {
		g.exclusiveProjects[projectID] = true
		heldExclusive = true
	}
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if v := g.agentInUse[agentID]; v > 0 {
			g.agentInUse[agentID] = v - 1
		}
		if g.totalInUse > 0 {
			g.totalInUse--
		}
		if heldExclusive {
			delete(g.exclusiveProjects, projectID)
		}
	}, nil
}

// Release decrements the counters reserved by a successful Acquire.
type Release func()

// Snapshot is a point-in-time view of in-flight usage, for doctor and metrics.
type UsageSnapshot struct {
	TotalInUse        int
	AgentInUse        map[string]int
	ExclusiveProjects []string
}

// Usage returns a point-in-time snapshot of current in-flight reservations.
func (g *Guard) Usage() UsageSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	agent := make(map[string]int, len(g.agentInUse))
	for k, v := range g.agentInUse {
		agent[k] = v
	}
	var excl []string
	for p := range g.exclusiveProjects {
		excl = append(excl, p)
	}
	return UsageSnapshot{TotalInUse: g.totalInUse, AgentInUse: agent, ExclusiveProjects: excl}
}
