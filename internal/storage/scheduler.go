package storage

import (
	"context"
	"sync"
)

// Scheduler is a wakeup-driven skeleton shared by the only three daemon
// schedulers. It intentionally owns no ticker: callers wake it after a commit
// or arm it from persisted next-at timestamps, so restart recovery has no
// in-memory timing authority.
type Scheduler struct {
	wake   chan struct{}
	onWake func(context.Context) error
}

func newScheduler(onWake func(context.Context) error) Scheduler {
	return Scheduler{wake: make(chan struct{}, 1), onWake: onWake}
}
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Scheduler) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wake:
			if s.onWake != nil {
				if err := s.onWake(ctx); err != nil {
					return err
				}
			}
		}
	}
}

// IntakeScheduler owns persisted forge-cursor polling wakeups.
type IntakeScheduler struct{ Scheduler }

func NewIntakeScheduler(run func(context.Context) error) *IntakeScheduler {
	return &IntakeScheduler{newScheduler(run)}
}

// ReconcilerScheduler owns convergence of Runs and external facts.
type ReconcilerScheduler struct{ Scheduler }

func NewReconcilerScheduler(run func(context.Context) error) *ReconcilerScheduler {
	return &ReconcilerScheduler{newScheduler(run)}
}

// SupervisorScheduler owns attempts, interrupt expiry and outbox retry wakeups.
type SupervisorScheduler struct{ Scheduler }

func NewSupervisorScheduler(run func(context.Context) error) *SupervisorScheduler {
	return &SupervisorScheduler{newScheduler(run)}
}

// Wakeups groups the three named scheduler entry points; write ports may call
// the appropriate one after commit, never while a transaction is open.
type Wakeups struct {
	Intake     *IntakeScheduler
	Reconciler *ReconcilerScheduler
	Supervisor *SupervisorScheduler
	once       sync.Once
}
