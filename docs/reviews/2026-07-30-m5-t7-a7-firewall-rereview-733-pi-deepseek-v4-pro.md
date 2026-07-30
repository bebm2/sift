# M5 T7 A7 防火墙定向复审（#733）

**复审对象:** Issue #730 / PR #732（`feat/issue-730-t7-a7-firewall`，commit `3e716d1`，已合入 main tip）

**复审人:** pi × DeepSeek v4 Pro

**复审日期:** 2026-07-30

**复审范围:** I5 验收判据（wave1 plan I5、WBS §5.1 T7、brain.md §13/§15.4）

---

## Verdict: **PASS**

---

## Checklist

| # | 判据 | 结果 | 证据 |
|---|------|------|------|
| 1 | T7 proposals/drafts persist but never auto-apply (policy/context) | **YES** | `SaveProposalDraft` 仅 INSERT `proposal_drafts`（status 恒为 `pending_human_approval`），无 outbox/budget/Gate/Interrupt/状态转移；UPDATE/DELETE 触发器阻止写入；idempotent insert-or-return identical；policy 与 context 两种 proposalkind 均有覆盖。Fallback（provider_disabled / invalid_output / provider_error）一律不创建 draft。 |
| 2 | T7/historical data cannot relax single Gate or suppress single HITL | **YES** | Gate 未读取 `proposal_drafts`/T7 trace/Ledger semantic material（`internal/gate/` 非测试代码零引用）。`Evaluate` 是冻结 Input 的纯函数。`TestA7GateVerdictAndDigestInvariantUnderT7Proposal` 证明 pending draft 在场时 verdict 与 digest 逐字节不变。`TestA7HITLNotSuppressedByT7Proposal` + `TestT7ProposalAndLedgerDataDoNotRelaxGateOrSuppressHITL` 证明 HITL 仍以原 severity 发出、未产生额外 Interrupt/outbox。 |
| 3 | Reuses Brain shell; no second emitter/charge path | **YES** | T7 经 `Shell.Call`（与 T4/T6 同 trace/收费口）。`PersistT7ProposalDraft` 是唯一写口，下行仅调 `SaveProposalDraft` 的纯 INSERT，无第二 outbox、无第二 budget entry。`TestT7ValidCallPersistsInertDraftViaSingleShellPort` 与 `TestT7ProposalDraftPersistsButNeverAutoApplies` 分别从 Brain 和 storage 层验证 baseline budget_entries 仅因 Brain call 本身增长一次。 |
| 4 | Relevant packages/tests green on main tip | **YES** | `go test ./internal/brain/ ./internal/storage/ ./internal/gate/ -run "T7|A7|a7"` 全 8 新测试 + 历史 T7 测试（Issue 436 等）PASS，零 flake。 |

---

## 复审发现

### 生产代码

- **`internal/storage/proposal.go`**: `SaveProposalDraft` 是唯一写口。表 `proposal_drafts` 的 `status` CHECK 约束强制 `pending_human_approval`，schema 无 policy/context/action/Gate 列。事务内核验 terminal valid T7 call（touchpoint + status + prompt/schema 版本），拒绝非 T7/fallback/版本漂移调用。`INSERT ... ON CONFLICT DO NOTHING` + 读回比对实现 idempotent + 冲突 reject。
- **`internal/storage/migrations/0010_m5_t6_dispatch.sql`**: `proposal_drafts` 表仅含 inert 字段，`BEFORE UPDATE/DELETE` 触发器强制 append-only。
- **`internal/brain/t4t6t7_result.go`**: `PersistT7ProposalDraft` 封装 fallback no-draft（`T7CallResult.NoDraft`）、closed 解码、contract 校验与调用 `SaveProposalDraft`。无第二收费/outbox 路径。
- **`internal/brain/t4t6t7.go`**: `T7Output` 仅含 `proposal_kind`/`target_scope`/`title`/`body`/`evidence_entry_ids`/`requires_human_approval`——故意无 Gate/Interrupt/action/policy patch 字段。`T7Contract.ValidateOutput` 强制 `requires_human_approval=true`、target-scope 不提权、evidence IDs 在输入集合内且排序去重。
- **`internal/gate/`** (非测试): 零引用 `proposal_drafts`、`ProposalDraft`、T7。`Evaluate` 不读这些表。Interrupt 路径同理。

### 测试覆盖

| 测试 | 包 | 覆盖点 |
|------|-----|-------|
| `TestT7ValidCallPersistsInertDraftViaSingleShellPort` | brain | policy + context 两种 `proposalkind`，单 Shell 口径，idempotent re-persist，prompt/schema 版本绑定 |
| `TestT7FallbackNeverPersistsADraft` | brain | provider_disabled / invalid_output / provider_error 三种 fallback 均不创建 draft |
| `TestT7ProposalDraftPersistsButNeverAutoApplies` | storage | 仅 `proposal_drafts` 行数变化；无 outbox/budget/Gate/Interrupt/ledger 副作用；idempotent；divergent 拒绝 |
| `TestT7ProposalDraftIsAppendOnly` | storage | UPDATE/DELETE 触发 abort |
| `TestT7ProposalDraftRequiresTerminalValidT7Call` | storage | 拒绝 unknown call、非 T7 touchpoint（T3）、fallback call、prompt/schema 版本漂移 |
| `TestT7ProposalAndLedgerDataDoNotRelaxGateOrSuppressHITL` | storage | pending draft + 历史 T7 calls + Ledger semantic material 在场时，frozen Gate snapshot/digest/cache 不变，单条 HITL 仍以原 severity 发出 |
| `TestA7GateVerdictAndDigestInvariantUnderT7Proposal` | gate | `Evaluate` verdict 与 `CanonicalInput` digest 在 T7 draft 在场时逐字节不变 |
| `TestA7HITLNotSuppressedByT7Proposal` | gate | `EvaluateRecordAndEmitInterrupt` 在 T7 draft 在场时仍发出单条 HITL |

---

## 备注

- 测试全部 PASS（`go test ./internal/brain/ ./internal/storage/ ./internal/gate/ -run "T7|A7|a7" -v -count=1`），零 flake。
- 生产 T7 调用器（周期聚合 → T7 → 持久化草稿）未接线：超出 I5 范围，留后续波次。
- WBS §5.1 第 456/457 行复选框（T7 只生成 policy/context 草稿不自动生效；测试 T7/历史数据不能放松 Gate/抑制 HITL）可据此复审核销。

## 处置

Verdict **PASS**。WBS §5.1 前三项 T7 复选框的测试证据已由复审闭合；生产接线（第四项"三触点"）的 T4/T6 部分已由 #706/#721 核销，T7 生产接线留后续波次。
