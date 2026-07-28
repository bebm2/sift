// Package forgebudget wires the Forge adapter's API budget charging port
// (forge.Charger) to the storage persistence port (storage.ChargeForgeAPICall).
//
// This is the single concrete Charger implementation over storage; the daemon
// constructs one per resolved forge budget (config.md §3.8) and injects it
// into the adapter via Adapter.WithCharger. Keeping it out of both the forge
// and storage packages leaves forge free of storage imports and storage free
// of forge imports — this package is the only place that depends on both.
package forgebudget

import (
	"context"
	"errors"
	"time"

	"github.com/miaoxiaoyong/sift/internal/forge"
	"github.com/miaoxiaoyong/sift/internal/storage"
)

// Charger implements forge.Charger over the storage forge API budget port.
// Now supplies the wall-clock timestamp used to resolve the UTC hour bucket;
// it defaults to time.Now when nil.
type Charger struct {
	DB           *storage.DB
	Limit        int64
	WarningRatio float64
	Now          func() time.Time
}

// Charge resolves the project, records one unit against its hourly bucket, and
// maps the storage result back to the adapter's budget decision. A storage
// exhaustion refusal becomes ChargeResult.Exhausted (no error) so the adapter
// surfaces it as forge.ErrRateLimited without launching the CLI. A charge that
// succeeds is never marked Exhausted even when it consumes the last unit:
// that call is allowed to proceed; only the *next* one is refused.
func (c *Charger) Charge(ctx context.Context, project forge.ProjectRef, chargeKey string) (forge.ChargeResult, error) {
	if c.DB == nil {
		return forge.ChargeResult{}, errors.New("forgebudget: nil storage handle")
	}
	projectID, err := c.DB.ProjectIDByForge(ctx, string(project.Kind), project.Host, project.ProjectKey)
	if err != nil {
		return forge.ChargeResult{}, err
	}
	now := c.Now()
	if now.IsZero() {
		now = time.Now()
	}
	res, err := c.DB.ChargeForgeAPICall(ctx, storage.ChargeForgeAPICallCmd{
		ProjectID:      projectID,
		CallAttemptKey: chargeKey,
		NowMS:          now.UnixMilli(),
		Limit:          c.Limit,
		WarningRatio:   c.WarningRatio,
	})
	if errors.Is(err, storage.ErrForgeBudgetExhausted) {
		return forge.ChargeResult{Exhausted: true, Limit: c.Limit}, nil
	}
	if err != nil {
		return forge.ChargeResult{}, err
	}
	return forge.ChargeResult{
		Consumed: res.Consumed,
		Limit:    res.Limit,
		Charged:  res.Charged,
		SlowPoll: res.SlowPoll,
	}, nil
}
