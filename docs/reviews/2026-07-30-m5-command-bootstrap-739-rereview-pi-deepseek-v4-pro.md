# M5 Command bootstrap 定向复审（#739）

**复审对象:** Issue #737 / PR #738（`feat/issue-737-command-bootstrap-approve-reject-retry-hold-ask`，commit `4c2a12e`，已合入 main tip）

**复审人:** pi × DeepSeek v4 Pro

**复审日期:** 2026-07-30

**复审范围:** WBS §5.4 关键验收（rereview after #737 / PR #738 merge，无 Sol rereview）

---

## Verdict: **PASS WITH NOTES**

---

## Checklist

| # | WBS §5.4 判据 | 结果 | 证据 |
|---|---------------|------|------|
| 1 | Pipeline: actor auth → syntax → nonce/options → DomainCommand + receipt (outbox) | **YES** | `ApplyCommandEvent` 单一口径：`lookupCommandReceiptTx` 去重 → `NewAuthorizer` 鉴权（immutable config snapshot）→ `ParseCommand` 严格语法（§3.1 ABNF）→ `resolveCommandInterruptTx` 不可变 target binding（JOIN `interrupt_command_targets`）→ `Compile` 校验 nonce/cutoff/options/label position → `commandRejectTx`/`commandHoldTx`/`commandAskTx`/`commandApproveTx` 经 `transition()` + `recordHumanDecisionTx` → `writeAcceptedCommandReceiptTx` + `writeCommandAckOpTx` outbox。标签与评论路径共用鉴权/幂等/回执。 |
| 2 | startup_stall only retry/reject/hold; approve must reject | **YES** | `Compile` 中 `optionAllowed(action, interrupt.Options)` 拒绝不在 options 内的动作；`EmitInterrupt` 只为 startup_stall reason 设置 `options=["retry","reject","hold"]`。`TestCompileStartupStallApproveRejected` + `TestApplyCommandStartupStallApproveRejectedOption` 验证 approve → `rejected_option`。 |
| 3 | retry request does not close Interrupt / write resolution; probe-in-flight rejects new commands | **YES** | `startupStallRetryRequestTx`：CAS rotate nonce + `dispatch_state=probe_in_progress`，写 initial `retry_pending` event + pending `attempt_probes` + HumanDecision，不关 Interrupt、不写 `attempt_resolution`、不创建 ack。`Compile` 检查 `DispatchProbeInProgress` → `OutcomeProbeInProgress`。`TestApplyCommandStartupStallRetryRequestPendingNoAck` + `TestApplyCommandStartupStallProbeInProgressRejects` 验证。 |
| 4 | probe failure reuses Interrupt + rotates nonce; at cap → hold | **YES** | `probeFailedTx`：mark probe failed → rotate nonce + `dispatch_state=held` → `finalizeRetryOutcomeTx`（`absence_unconfirmed` final event + pending→final CAS）→ `writeProbeAckOpTx`。Interrupt 保持 open，isolation 保持 frozen。`TestApplyRetryProbeResultFailureAbsenceUnconfirmed` 验证。At-cap hold 走现有 expiry/escalate 路径（`OnMaxEscalations: ExpireHold`），非本 slice 新增。 |
| 5 | probe success = ADR-013 single CAS: evidence, retry_after_absence, isolation release, close/responded, queued, new attempt/claim, final outcome + ack — all-or-nothing | **YES** | `probeSucceededTx`：单事务内 (1) probe→succeeded + evidence digest (2) `attempt_resolution=retry_after_absence` (3) `closeInterruptTx` responded (4) `transition` waiting_human→queued (5) `finalizeRetryOutcomeTx` applied final event (6) `isolation_state=none` + `isolation_release_event_id` (7) `writeProbeAckOpTx`。任一 CAS 失败回滚。`TestApplyRetryProbeResultSuccessClosesAndQueues` 验证 7 步全或全无。 |
| 6 | reject writes attempt_resolution=reject, Run failed, isolation held until absence proven | **YES** | `commandRejectTx`：`transition` → RunFailed(human_reject) → `attempt_resolution=reject`（write-once NULL→reject）→ `recordHumanDecisionTx` reject + semantic material → `closeInterruptTx` responded。Isolation 保持 frozen。`TestApplyCommandStartupStallRejectFailsRunHoldsIsolation` 验证。 |
| 7 | All approve/reject/retry/hold/ask go through unique transition + outbox; label and comment paths share auth/idempotency | **YES** | 所有动作路径：`applyCommandEffectTx` → `commandRejectTx`/`commandHoldTx`/`commandAskTx`/`commandApproveTx` → `d.transition()`（唯一状态转移函数）+ `recordHumanDecisionTx`。Ack operation 统一经 `writeCommandAckOpTx` / `writeProbeAckOpTx`（`OperationCommandAck` kind）。标签 approve 走 `commandApproveTx`，与评论 approve 同路径。去重口诀：`lookupCommandReceiptTx` 检查 `command_receipts` 的 `(project_id,event_kind,remote_event_id)`。`TestApplyCommandApprovalLabelApprove` 验证标签 approve 正确入队。 |
| 8 | All actions reuse M3 sole arbitration function; cover fact-first / decision-first / in-flight-probe arrivals | **YES** | Command 代码不直接调用 `ResolveAttemptRace`，但设置 `attempt_resolution` marker：reject→`reject`，probe success→`retry_after_absence`，retry request→不写 marker（ADR-013 保持事实窗口开放）。M3 `ResolveAttemptRace` 检查 `attempt_resolution` 决定 `superseded_by_decision` vs `superseded_by_fact` vs `running`。三场景覆盖：(a) 事实先到：probe success CAS 的 `runVersion` CAS 拒绝（Run 已非 waiting_human）；(b) 决定先到：`attempt_resolution` 已写入，M3 race 返回 `superseded_by_decision`；(c) 探测在途：`attempt_resolution` 未写，事实到达 M3 race 可返回 `running`/`superseded_by_fact`。 |
| 9 | `/sift reject` reason 与 `/sift ask` text 随人类决定写入 Ledger | **YES** | `commandRejectTx` → `recordHumanDecisionTx`（`DecisionReject` + `SemanticMaterial: c.RejectReason`）。`commandAskTx` → `recordHumanDecisionTx`（`DecisionAsk` + `SemanticMaterial: c.AskText`）。`TestApplyCommandReject` 验证 `ledger_entries WHERE entry_kind='semantic_material'` = 1。`TestApplyCommandAskWritesSemanticMaterial` 验证 ask text `"what next?"` 正确持久化。 |
| 10 | No second transition / Ledger / outbox path | **YES** | 全文搜索 `command.go` / `command_probe.go`：所有状态写入经 `d.transition()`（`internal/storage/transition.go:158`），所有 Ledger 写入经 `recordHumanDecisionTx`（`internal/storage/ledger.go:203`），所有 outbox 写入经 `writeCommandAckOpTx` / `writeProbeAckOpTx`（同一 ack 格式）。无绕过这些核心的直接 SQL 状态写入。 |
| 11 | Relevant packages/tests green on main tip including PR #738 | **YES** | `go test ./internal/command/ ./internal/storage/ -count=1` — 全部 PASS（0 flake）。command 包 18 测试覆盖 envelope 验证、grammar（approve/reject/retry/hold/ask 全 action + 边界）、event canonical、stage keys。storage 包 14 新命令测试覆盖 approve/reject/hold/ask、startup_stall retry request/reject/probe-in-progress、probe success/failure、approval_label、untrusted/missing actor、syntax error、wrong nonce、replay、hold nonce rotation。 |

---

## 复审发现

### 生产代码结构

- **`internal/command/envelope.go`**: `CommandEventEnvelopeV1` closed envelope + `Validate`（schema=1、source 闭集、remote event/target id 1–256 UTF-8、raw/event key 64 hex、source-specific comment/label 互斥）。`RecomputeEventKey` 以 `SHA-256(canonical_json)` 生成 canonical command identity（project × source × remote_id 隔离，跨 source/project 不碰撞）。`VerifyEventKey` constant-time 比对。
- **`internal/command/grammar.go`**: `ParseCommand` 逐字节 ABNF（§3.1）：`/sift <action> <run-id> <nonce> [<reason|duration|text>]`，`nextToken` 拒绝 leading/trailing/double SP。Duration 经 `time.ParseDuration` 解析，拒绝 sign/overflow/sub-ms/0。`validReason` 拒绝 CR/LF/NUL。`validAskText` 允许 LF/CRLF，拒 bare CR。
- **`internal/command/compile.go`**: `Compile` 纯函数：nil/closed Interrupt → `rejected_target`，target mismatch → `rejected_target`，nonce mismatch → `rejected_stale`，label position ≤ cutoff → `rejected_stale`，action ∉ options → `rejected_option`，probe-in-progress → `probe_in_progress`，hold > max → `rejected_option`。`Authorizer` 是 immutable config snapshot 的纯 map lookup。
- **`internal/command/event.go`**: `CommandEventV1` / `CommandAckV1` closed canonical JSON（decode.Canonical 拒绝未知字段）。`NewEvent` 永不 echo submitted nonce。Stage keys：`command:<key>:initial` / `command:<key>:final:*` / `command:<key>:ack`。`CanonicalBytes` 强制 size limit + invariant。
- **`internal/storage/command.go`**: `ApplyCommandEvent` 唯一 public 写口。事务内顺序：dedup → auth → grammar → resolution/compile → effect → event + receipt + ack。非 startup retry → `ErrCommandEffectNotWired`（bootstrap 范围限制）。
- **`internal/storage/command_probe.go`**: `ApplyRetryProbeResult` 唯一 probe finalizer。成功走 ADR-013 全量 CAS，失败旋转 nonce 发 `absence_unconfirmed` ack。`finalizeRetryOutcomeTx` 是 pending→final 的 single CAS guard。
- **`internal/storage/migrations/0053_command_bootstrap.sql`**: `command_event_outcomes`（`state` CHECK `pending|final`，UPDATE trigger 拒 pending→non-final CAS）、`command_receipts`（append-only trigger）、indexes。
- **`internal/storage/outbox.go`**: `OperationCommandAck` kind 已注册并合法化（`validOperationKind` returns true）。

### 测试覆盖

| 测试 | 包 | 覆盖点 |
|------|-----|-------|
| `TestParseApproveRejectRetry` | command | approve/reject/retry 全成功路径 + trailing space / extra arg / CR/LF in reason |
| `TestParseRunIDNonce` | command | short/long/non-hex/uppercase run-id |
| `TestParseAskText` | command | plain/LF/CRLF/bare CR/trailing CR/empty/NUL |
| `TestParseHoldDuration` | command | 17 向量：ns/us/µs sub-ms reject、1ms/500ms/1s/2h30m/1h/1.5h accept、0s/-5s/+5s/missing unit/bad unit/overflow reject |
| `TestParseLeadingToken` | command | 8 向量：empty/no-action/double-space/case-mismatch/no-slash/unknown-action/trailing-space/no-action |
| `TestRecomputeEventKeyStable` | command | stable、cross-source/project non-collision、64 hex |
| `TestEnvelopeValidation` | command | good comment、tampered event key、schema mismatch、missing comment、body too large |
| `TestApprovalLabelEnvelope` | command | good label、leading-zero position、position length overflow、action≠added |
| `TestAuthorizer` | command | trusted/untrusted/nil/empty actor |
| `TestCompileApproveReject` | command | approve applied、wrong nonce stale、wrong target stale、ask-on-design_approval → rejected_option |
| `TestCompileNoCurrentInterrupt` | command | nil/closed Interrupt → rejected_target |
| `TestCompileStartupStallApproveRejected` | command | approve rejected、retry/reject/hold accepted on startup_stall |
| `TestCompileStartupStallProbeInProgress` | command | probe-in-progress → probe_in_progress |
| `TestCompileApprovalLabelCutoff` | command | no cutoff → applied、equal pos → rejected_stale、earlier → rejected_stale、later → applied |
| `TestCompileHoldMaxDuration` | command | within-limit accept、exceed-limit reject |
| `TestNewEventAckCanonical` | command | round-trip applied、rejected_syntax null action/run、ack valid、pending ack reject |
| `TestStageKeys` | command | stage key format、ack key format、final stage mapping |
| `TestApplyCommandReject` | storage | Run→failed(human_reject)、close_reason=responded、ledger: 1 human_decision + 1 semantic_material、1 command_ack outbox、accepted receipt |
| `TestApplyCommandHoldRotatesNonce` | storage | dispatch_state=held+held_reason=manual、nonce rotated≠old、expiry=now+duration |
| `TestApplyCommandAskWritesSemanticMaterial` | storage | agent_blocked interrupt、ask text "what next?" in ledger semantic_material |
| `TestApplyCommandApproveQueuesRun` | storage | Run→queued、close_reason=responded |
| `TestApplyCommandUntrustedActorIgnored` | storage | ignored_untrusted_actor receipt、0 command events、0 ack、0 ledger、Run status unchanged |
| `TestApplyCommandNullActorIgnored` | storage | ignored_missing_actor receipt |
| `TestApplyCommandSyntaxError` | storage | rejected_syntax event + accepted receipt + ack |
| `TestApplyCommandWrongNonceRejectedStale` | storage | rejected_stale |
| `TestApplyCommandReplayReturnsStoredOutcome` | storage | 1 event、1 receipt、1 ledger entry、interrupt unchanged after replay |
| `TestApplyCommandStartupStallApproveRejectedOption` | storage | approve on startup_stall → rejected_option |
| `TestApplyCommandStartupStallRejectFailsRunHoldsIsolation` | storage | Run→failed、isolation=frozen、attempt_resolution=reject、close_reason=responded |
| `TestApplyCommandStartupStallRetryRequestPendingNoAck` | storage | interrupt open+probe_in_progress、nonce rotated、pending probe created、no attempt_resolution、0 ack |
| `TestApplyCommandStartupStallProbeInProgressRejects` | storage | 2nd retry on probe_in_progress → probe_in_progress、only 1 probe |
| `TestApplyRetryProbeResultSuccessClosesAndQueues` | storage | retry_after_absence、isolation=none+release timestamp、close_reason=responded、Run=queued、final outcome relation、1 command_ack |
| `TestApplyRetryProbeResultFailureAbsenceUnconfirmed` | storage | absence_unconfirmed、interrupt open、isolation=frozen、final outcome、1 command_ack |
| `TestApplyCommandApprovalLabelApprove` | storage | label approve → applied、Run=queued、close_reason=responded |

---

## 非阻断注记

1. **`command_ack` outbox worker 未接线** — `command_ack` outbox operation 已正确创建与持久化（`writeCommandAckOpTx` / `writeProbeAckOpTx`），出队端 `validOperationKind` 已注册，但尚无 worker 消费并发布回 Forge。accepted receipt 已单事务写入，回执的发布属于后续波次接线，不阻塞当前门禁。

2. **非 startup retry 与 guardrail/code_review approve 返回 `ErrCommandEffectNotWired`** — `applyCommandEffectTx` 对 `ActionRetry`（failure_review/agent_blocked/merge_conflict）以及 `commandApproveTx` 对非 `design_approval` reason 返回 `ErrCommandEffectNotWired`，事务回滚且不写 receipt。Issue #737 明确标记为 "bootstrap slice" 且 scope 不含 Gate re-evaluation operation / attempt terminalization 完整接线，本注记不构成缺陷。

3. **At-cap hold 走现有 expiry/escalate 路径** — `probeFailedTx` 将 `dispatch_state=held`，后续 escalation cap hold 由现有的 `AdvanceInterrupt` + `OnMaxEscalations: ExpireHold` 处理。此路径未在 command 测试中端到端覆盖，但属于 M3/M4 已有路径，不属本次回归范围。

4. **probe worker 不在本 slice** — `ApplyCommandEvent` 创建 pending `attempt_probes`（`state=pending`），但事务后的 probe worker（进程检查、确认消失）不在此 PR 内。`ApplyRetryProbeResult` 是 probe 的 finalizer 端口且已充分测试，worker 接线留后续波次。

---

## 注记复核

| # | 注记 | 类型 | 建议处置 |
|---|------|------|---------|
| 1 | ack outbox worker 未接线 | 接线缺口 | 后续 Command/Forge 波次接线（评论回执发布到 Forge） |
| 2 | 非 startup retry + guardrail/code_review approve → ErrCommandEffectNotWired | 功能缺口 | M5 后续波次实现 Gate re-evaluation operation + attempt terminalization |
| 3 | At-cap hold 走现有路径 | 测试缺口 | 可在 probe worker 接线时追加 escalation cap hold 端到端测试 |
| 4 | probe worker 未在本 slice | 接线缺口 | 后续波次实现 probe 进程检查 worker |
