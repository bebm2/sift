package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/forge"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

func TestAssembleWiresIntakeT1ReconcilerCommentsAndBudget(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1000)
	db, err := storage.Open(ctx, storage.OpenConfig{
		Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, projectID := range []string{"github-project", "gitlab-project"} {
		if err := db.SeedProjectForTest(ctx, "cfg-"+projectID, projectID, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{
		Projects: []config.Project{
			{ID: "github-project", Enabled: true, Forge: config.ForgeRef{Kind: config.ForgeKind("github"), Host: "github.example", Project: "acme/widgets", CLI: "unused"}},
			{ID: "gitlab-project", Enabled: true, Forge: config.ForgeRef{Kind: config.ForgeKind("gitlab"), Host: "gitlab.example", Project: "acme/widgets", CLI: "unused"}},
		},
		Brain:     config.Brain{CallTimeout: time.Second},
		Forge:     config.Forge{HourlyAPILimit: 10, WarningRatio: .8, SlowPollInterval: time.Minute},
		Outbox:    config.Outbox{LeaseTTL: time.Minute},
		Scheduler: config.Scheduler{IntakeIdleInterval: time.Minute, IntakeActiveInterval: time.Second},
		Labels:    config.Labels{Trigger: "sift"},
	}
	workers, err := Assemble(db, cfg, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if len(workers.Pollers) != 2 || len(workers.Evaluators) != 2 || len(workers.Reconcilers) != 2 || len(workers.Comments) != 2 {
		t.Fatalf("assembly counts: pollers=%d evaluators=%d reconcilers=%d comments=%d", len(workers.Pollers), len(workers.Evaluators), len(workers.Reconcilers), len(workers.Comments))
	}
	for i, p := range workers.Pollers {
		if p.OnIssue == nil {
			t.Fatalf("poller %d has no T1 callback", i)
		}
		if len(p.Projects) != 1 {
			t.Fatalf("poller %d project scope=%d, want 1", i, len(p.Projects))
		}
		client, ok := p.Forge.(*forge.Adapter)
		if !ok {
			t.Fatalf("poller %d forge=%T, want production adapter", i, p.Forge)
		}
		if client.Kind != forge.Kind(p.Projects[0].Ref.Kind) {
			t.Fatalf("poller %d adapter kind=%s project kind=%s", i, client.Kind, p.Projects[0].Ref.Kind)
		}
		reconciler := workers.Reconcilers[i]
		if len(reconciler.Projects) != 1 || !reflect.DeepEqual(reconciler.Projects[0], p.Projects[0]) {
			t.Fatalf("reconciler %d project scope=%+v, want %+v", i, reconciler.Projects, p.Projects)
		}
		reconcilerClient, ok := reconciler.Forge.(*forge.Adapter)
		if !ok || reconcilerClient != client {
			t.Fatalf("reconciler %d is not scoped to its poller's adapter", i)
		}

		commentClient, ok := workers.Comments[i].Client.(*forge.Adapter)
		if !ok || commentClient != client {
			t.Fatalf("comment worker %d is not scoped to its poller's adapter", i)
		}

		// Production adapters must reject a Forge call without the stable key;
		// this proves the assembled path is budget-enforcing rather than a test
		// adapter silently making an uncharged call.
		_, _, err = client.ListIssueComments(ctx, p.Projects[0].Ref, "1", "")
		var classified *forge.ClassifiedError
		if !errors.As(err, &classified) || !errors.Is(err, forge.ErrContractViolation) {
			t.Fatalf("adapter call without charge key: %v", err)
		}
	}
}

func TestAssembleProbesAndRecordsAutoMergeCapabilityOnEveryStartup(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1000)
	db, err := storage.Open(ctx, storage.OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SeedProjectForTest(ctx, "cfg-project", "project", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	cfg := daemonTestConfig("project")
	ref := forge.ProjectRef{Kind: forge.KindGitHub, Host: "github.com", ProjectKey: "org/repo-project"}
	probes := 0
	factory := daemonAdapterFactory(func(_ context.Context, _ string, args []string, _ []byte) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"api", "--help"}) {
			t.Fatalf("probe args = %q", args)
		}
		probes++
		return []byte("--input file"), nil, nil
	})
	if _, err := assemble(db, cfg, func() time.Time { return now }, factory); err != nil {
		t.Fatal(err)
	}
	if enabled, err := db.AutoMergeEnabled(ctx, ref); err != nil || !enabled {
		t.Fatalf("first startup capability = %v, %v; want true, nil", enabled, err)
	}
	if _, err := assemble(db, cfg, func() time.Time { return now.Add(time.Second) }, factory); err != nil {
		t.Fatal(err)
	}
	if probes != 2 {
		t.Fatalf("startup probes = %d, want 2", probes)
	}
}

func TestAssembleRecordsAmbiguousCapabilityAndFailsOnStorageError(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1000)
	ambiguousFactory := daemonAdapterFactory(func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return nil, []byte("CLI unavailable"), errors.New("exit status 1")
	})

	t.Run("ambiguous probe remains available and is persisted false", func(t *testing.T) {
		db, err := storage.Open(ctx, storage.OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.SeedProjectForTest(ctx, "cfg-project", "project", now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		workers, err := assemble(db, daemonTestConfig("project"), func() time.Time { return now }, ambiguousFactory)
		if err != nil || len(workers.Pollers) != 1 {
			t.Fatalf("ambiguous startup workers=%v err=%v", workers, err)
		}
		ref := forge.ProjectRef{Kind: forge.KindGitHub, Host: "github.com", ProjectKey: "org/repo-project"}
		if enabled, err := db.AutoMergeEnabled(ctx, ref); err != nil || enabled {
			t.Fatalf("ambiguous capability = %v, %v; want false, nil", enabled, err)
		}
	})

	t.Run("storage failure stops startup", func(t *testing.T) {
		db, err := storage.Open(ctx, storage.OpenConfig{Path: filepath.Join(t.TempDir(), "sift.db"), BinaryVersion: "test", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := assemble(db, daemonTestConfig("missing"), func() time.Time { return now }, ambiguousFactory); err == nil {
			t.Fatal("Assemble succeeded despite capability storage failure")
		}
	})
}

func daemonTestConfig(projectID string) *config.Config {
	return &config.Config{
		Projects:  []config.Project{{ID: projectID, Enabled: true, Forge: config.ForgeRef{Kind: config.ForgeKind("github"), Host: "github.com", Project: "org/repo-" + projectID, CLI: "gh"}}},
		Brain:     config.Brain{CallTimeout: time.Second},
		Forge:     config.Forge{HourlyAPILimit: 10, WarningRatio: .8, SlowPollInterval: time.Minute},
		Outbox:    config.Outbox{LeaseTTL: time.Minute},
		Scheduler: config.Scheduler{IntakeIdleInterval: time.Minute, IntakeActiveInterval: time.Second},
		Labels:    config.Labels{Trigger: "sift"},
	}
}

func daemonAdapterFactory(r forge.Runner) func(forge.Kind, string, forge.Runner, forge.Charger) (*forge.Adapter, error) {
	return func(k forge.Kind, cli string, _ forge.Runner, charger forge.Charger) (*forge.Adapter, error) {
		return forge.NewProductionAdapter(k, cli, r, charger)
	}
}
