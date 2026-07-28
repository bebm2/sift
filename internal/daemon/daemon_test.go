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
