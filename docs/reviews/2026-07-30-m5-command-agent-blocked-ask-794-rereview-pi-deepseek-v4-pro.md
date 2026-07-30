---
status: done
date: 2026-07-30
issue: 794
pr: 795
verdict: PASS
model: deepseek/deepseek-v4-pro
---

# M5 Command agent_blocked|/sift ask — Sol rereview (#794)

## Review Round 1

**Artifacts reviewed:**
- Issue #794 body + scope
- PR #795 (`feat/issue-794-wire-agent-blocked-sift-ask-full-contract-task-s`) diff (2 files, +201/−12)
- Full test run: `go test ./internal/storage/ -run "TestApplyCommand" -count=1 -race` — **28/28 PASS, 0 flakes**
- Spec cross-reference: `command.md` §4 `agent_blocked|ask` row, `storage.md` §5.1 / §12.3

---

### Findings

**No P0 or P1 findings.** The implementation faithfully wires every item in the issue's scope:

| Scope item | Status |
|---|---|
| Task Spec snapshot sourced by command event | ✅ `insertClarificationTaskSpecTx` — append-only, `source_event_id` FK linked, `MAX(version)+1` under txn |
| Close Interrupt `responded` | ✅ CAS via `closeInterruptTx(id, expectedVersion, nonce, "responded")` |
| Terminalize bound blocked attempt | ✅ `UPDATE attempts SET phase='orphaned'` with `phase IN ('pending','starting','spawning','running')` guard |
| Next pending attempt/claim/launch | ✅ `spawnNextAttemptTx` shared helper, extended with `taskSpecSnapshotID` param |
| HumanDecision(DecisionAsk) + unmodified SemanticMaterial | ✅ `recordHumanDecisionTx` + test asserts unmodified `"use the cached token"` |
| No project/global Context promotion | ✅ Test asserts zero `proposal_drafts` |
| Reuse only — no second write path | ✅ `spawnNextAttemptTx` extended (parameterized), not duplicated; retry path passes `""` for backward compat |
| Other reasons' ask→`ErrCommandEffectNotWired` | ✅ Defense-in-depth in `commandAskTx`; compile already rejects via `optionAllowed` |
| `-race` on new tests | ✅ `TestApplyCommandAgentBlockedAskFullContract` passes `-race` |
| Crash/replay idempotency | ✅ Replay returns stored `OutcomeApplied` / `FinalEventID`, zero duplicate writes |
| Existing tests still green | ✅ Full `TestApplyCommand*` suite 28/28 PASS |

**[P2] agent_blocked ask row missing from `TestApplyCommandReasonActionMatrix`**

- **Description:** The matrix test comment says "table-driven reason×action proof that the canonical newly-wired rows apply and persist their successors." The `agent_blocked|ask` row is newly wired but not included in the table. The comprehensive `TestApplyCommandAgentBlockedAskFullContract` covers all effects, but adding it to the matrix improves discoverability and consistency with the existing pattern.
- **Close criterion:** Add `{"agent_blocked ask", InterruptAgentBlocked, command.ActionAsk, false}` to the matrix table, or assert it's intentionally out-of-scope (full-contract test is complete and more thorough than per-row matrix entries).
- **Evidence:** `grep "reasonActionMatrix" internal/storage/command_effects_test.go` shows 4 rows (guardrail approve, code_review approve, merge_conflict retry, failure_review gate_recheck retry) — agent_blocked ask missing.
- **fixer=same**

---

### Scope summary

| 级别 | 数量 | 本轮是否实施 |
|---|---|---|
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 1 | 否（记录） |
| DEFER | 0 | — |

### Verdict

**PASS** — P0/P1 全关。`agent_blocked` `/sift ask` full contract 已按 `command.md` §4 完整接线，Task Spec 快照、终结、新 attempt/claim/launch、Ledger 语义原料均在单一事务内原子完成，澄清保持任务层不升格 Context；现有 28 个 Command 测试全部 green（含 `-race`）。P2（矩阵表补行）为非阻断注记。
