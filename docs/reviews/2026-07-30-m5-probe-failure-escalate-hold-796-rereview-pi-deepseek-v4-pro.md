# M5 #796 probe failure → escalate → hit-limit hold rereview

**Verdict: PASS** (Sol / deepseek-v4-pro, review_round 1)  
**PR:** #797 (`71dbb71`) · **Issue:** #796 · **Base:** `b1754f3`

## Scope

Close WBS §5.4: probe failure reuses Interrupt, escalation count advances via unique `AdvanceInterrupt`/expiry, at-cap hold (startup_stall never auto_reject).

## Findings

| ID | Severity | Status |
|----|----------|--------|
| `probeFailedTx` 未对称调用 `excludeStaleBatchMembersTx` | P2 | Record only — delivery authority still excludes stale members; no correctness risk |

P0/P1: **0**.

## Evidence

- Fix: below-cap `probeFailedTx` → `dispatch_state=batched` (escalate-able); at-cap → frozen capped hold (`held`/`max_escalations`).
- Tests: `TestStartupStallProbeFailureEscalatesToCapHold`, `...AtCapAppliesFrozenCappedHold`, `...StateIsEscalateableByDirectAdvance` — `-race` / `count=3` PASS.
- Specs: `command.md` §5 both paths; `interrupt.md` §8.2 batched in expiry scan; no resolution / no auto_reject for startup_stall at cap.
- Existing `TestApplyRetryProbeResultFailureAbsenceUnconfirmed` / success path still green; `go vet` clean.

## Notes

Do **not** claim M5 complete. Probe process-check worker and ack publish worker remain out of slice. P2 hygiene left for backlog.
