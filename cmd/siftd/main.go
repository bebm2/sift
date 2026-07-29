// Command siftd runs the local Sift control-plane daemon.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/controlplane"
	"github.com/miaoxiaoyong/sift/internal/daemon"
	"github.com/miaoxiaoyong/sift/internal/launchworker"
	"github.com/miaoxiaoyong/sift/internal/runtime"
	"github.com/miaoxiaoyong/sift/internal/storage"
	"path/filepath"
)

func main() {
	home, err := config.ResolveHome()
	if err != nil {
		fatal(err)
	}
	snapshot, err := config.Load(home, time.Now())
	if err != nil {
		fatal(err)
	}
	if hasEnabledProjects(snapshot.Config) {
		if _, err := runtime.ResolveInstalledWrapper(controlplane.Version); err != nil {
			fatal(err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	now := time.Now()
	db, err := storage.Open(ctx, storage.OpenConfig{Path: filepath.Join(home.Path, "sift.db"), BinaryVersion: controlplane.Version, Now: now})
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	if err := db.ActivateConfig(ctx, snapshot, controlplane.Version, now.UnixMilli()); err != nil {
		fatal(err)
	}
	bootID, err := db.StartDaemonBoot(ctx, snapshot.Hash, controlplane.Version, controlplane.ProtocolMajor, os.Getpid(), now.UnixMilli())
	if err != nil {
		fatal(err)
	}
	termination := &daemon.TerminationCoordinator{
		DB: db, Terminator: runtime.Terminator{Inspector: runtime.PlatformProcessInspector{}, Signaler: runtime.UnixProcessSignaler{}}, Runtime: snapshot.Config.Runtime,
		ControlRoot:         home.Path,
		AttentionDailyQuota: attentionQuota(snapshot.Config.Attention.DailyQuota), DayTimezone: snapshot.Config.Attention.DayTimezone, Now: time.Now,
	}
	// Recovery runs before Assemble starts any worker. Incomplete process
	// evidence deliberately fails closed and becomes a visible startup_stall
	// instead of allowing a launch lease to be reclaimed.
	if err := termination.RecoverStartup(ctx, bootID); err != nil {
		fatal(err)
	}
	if err := db.CompleteStartupRecovery(ctx, bootID, time.Now().UnixMilli()); err != nil {
		fatal(err)
	}
	workers, err := daemon.Assemble(db, snapshot.Config, time.Now)
	if err != nil {
		fatal(err)
	}
	daemonPath, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	backend, err := runtime.NewProcessBackend(daemonPath, controlplane.Version)
	if err != nil {
		fatal(err)
	}
	workers.SetLaunchWorker(&launchworker.Worker{DB: db, BootID: bootID, WorkerID: "siftd:launch_agent", Root: home.Path, Lease: snapshot.Config.Runtime.SpawnOperationLeaseTTL, Now: time.Now, Backend: launchworker.ProcessBackend{Backend: backend}, Agents: snapshot.Config.Agents})
	s, err := controlplane.Start(home, db)
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	s.SetOperatorAction(func(ctx context.Context, method, runID string, version int64) error {
		return termination.Operator(ctx, runID, version, method == "ops.retry")
	})
	startSchedulers(ctx, db, workers, termination, snapshot.Config.Scheduler)
	defer db.SetOutboxWakeup(nil)
	if err := s.Serve(ctx); err != nil {
		fatal(err)
	}
}
func hasEnabledProjects(cfg *config.Config) bool {
	for _, project := range cfg.Projects {
		if project.Enabled {
			return true
		}
	}
	return false
}

func attentionQuota(q config.DailyQuota) map[storage.InterruptSeverity]int {
	return map[storage.InterruptSeverity]int{storage.SeverityLow: q.Low, storage.SeverityNormal: q.Normal, storage.SeverityHigh: q.High}
}

// startSchedulers is the sole owner of siftd's three DESIGN §6.1 clocks.
// Intake's cursor still determines whether a poll is due; the supervisor
// interval also bounds recovery of persisted outbox retry deadlines.
func startSchedulers(ctx context.Context, db *storage.DB, workers *daemon.Daemon, termination *daemon.TerminationCoordinator, cfg config.Scheduler) {
	intake := storage.NewIntakeScheduler(reportSchedulerError("intake", workers.IntakeTick))
	supervisor := storage.NewSupervisorScheduler(reportSchedulerError("supervisor", termination.Timeout))
	outbox := storage.NewOutboxScheduler(reportSchedulerError("outbox", workers.OutboxTick))
	db.SetOutboxWakeup(outbox.Wake)

	go runScheduler(ctx, intake, minIntakeInterval(cfg))
	go runScheduler(ctx, supervisor, cfg.SupervisorInterval)
	go runScheduler(ctx, outbox, cfg.SupervisorInterval)
}

type wakeScheduler interface {
	Wake()
	Run(context.Context) error
}

func runScheduler(ctx context.Context, scheduler wakeScheduler, interval time.Duration) {
	go func() { _ = scheduler.Run(ctx) }()
	scheduler.Wake() // startup recovery must not wait for a clock edge.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scheduler.Wake()
		}
	}
}

func minIntakeInterval(cfg config.Scheduler) time.Duration {
	interval := cfg.IntakeIdleInterval
	for _, candidate := range []time.Duration{cfg.IntakeActiveInterval, cfg.IntakeInterruptInterval} {
		if candidate > 0 && (interval == 0 || candidate < interval) {
			interval = candidate
		}
	}
	return interval
}

func reportSchedulerError(name string, run func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "siftd: %s scheduler: %v\n", name, err)
		}
		return nil
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "siftd:", err); os.Exit(1) }
