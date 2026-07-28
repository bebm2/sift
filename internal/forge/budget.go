package forge

import (
	"context"
)

// Forge API budget charging port (forge.md §9, DESIGN §9.2).
//
// API calls are charged once per remote HTTP request at the Forge adapter
// layer and nowhere else. The adapter calls its Charger before launching each
// CLI subprocess with a stable charge key; a refusal (budget exhausted)
// surfaces as ErrRateLimited and the subprocess is never launched. Charge
// keys are idempotent across crash replay, so re-executing the same logical
// call after a restart does not double-bill.
//
// The concrete Charger over storage is supplied by the daemon wiring; the
// adapter itself depends only on this interface so the forge package stays
// free of storage imports.

// Charger reserves one unit of forge API budget for a single remote request.
// Implementations must be safe for concurrent use. A nil Charger means
// charging is disabled (tests, fake chains, dry runs).
type Charger interface {
	// Charge reserves budget for project under chargeKey. A replay of an
	// already-committed key returns Charged=false and the current
	// consumption without billing again. Exhausted must report whether the
	// project has reached its hourly limit; the adapter treats Exhausted as
	// a request to reject the call without launching the CLI.
	Charge(ctx context.Context, project ProjectRef, chargeKey string) (ChargeResult, error)
}

// ChargeResult reports budget state after a charge.
type ChargeResult struct {
	Consumed  int64
	Limit     int64
	Charged   bool // false on idempotent replay
	SlowPoll  bool // consumed >= warning ratio: Intake should slow-poll
	Exhausted bool // consumed >= limit: caller must reject the call
}

// BudgetConfig carries the resolved forge budget knobs the adapter needs when
// it has to decide slow-poll locally (config.md §3.8).
type BudgetConfig struct {
	Limit        int64
	WarningRatio float64
}

type chargeKeyCtx struct{}

// WithChargeKey associates a stable charge-key base with the context for a
// sequence of adapter calls (one logical operation: an Intake tick, one
// outbox attempt). The adapter appends an incrementing ":<seq>" suffix per
// CLI subprocess so each request gets a distinct, replay-stable key.
//
// outbox workers pass "forge-call:<outbox_attempt_id>"; Intake passes an
// equally stable "tick:<tick>:<project>" key (forge.md §9, outbox.md §2).
func WithChargeKey(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, chargeKeyCtx{}, base)
}

// chargeKeyBaseFrom reads the charge-key base set by WithChargeKey. ok is
// false when the caller did not opt into charging for this operation.
func chargeKeyBaseFrom(ctx context.Context) (string, bool) {
	base, ok := ctx.Value(chargeKeyCtx{}).(string)
	return base, ok
}
