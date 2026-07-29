FAIL

# M5 #529 Channel webhook after #522 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#522 / PR #526，实现提交 `ef0e4311b4e4e7125b2fe465d3adbd9b29e02eea`，合入提交 `0cde4ae`
> 评审基线：`main` / `origin/main` `0cde4ae`
> 判定基准：[#517 FAIL](2026-07-30-m5-channel-webhook-worker-517-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #522 修正了 Gate 的 strict-next daily-summary 边界、为 `RecordReportQuotaExhaustion` 补入 Channel/batch 参数、修正 Channel operation 与 alert operation 的诊断双 join，并增加 HTTP-date `Retry-After`、部分 config closed 校验、Ledger 碰撞核验和 `0020_channel_authority_invariants`。这些均是有效增量，且 migration 编号符合 conductor 指定。

但 #517 要求的关闭证据仍未建立：`RecordReportQuotaExhaustion` 在生产代码中没有调用点；storage §6.6 的 production sealer/canonical alert/failure-reclaim exact vectors仍不存在；authority migration只有 trigger 补强，没有完整 matrix；`ps`/`doctor` 与 config 没有新增定向测试。更直接地，新增 `0020` 后 `internal/storage.TestMigrationRecordedAndIdempotent` 仍硬编码 version/count 19，PR #526 的 `vet + test` check 与当前 `go test ./...` 都失败。

本轮只新增评审报告，不修改被评审代码或规格。#517 的 **P1-1..P1-5 仍 OPEN，P1-6 保持 CLOSED**；#522 不可核销。

## 2. #517 P1 逐项复审

### P1-1 — OPEN：字段和 daily 边界已修，但 Report quota 仍未形成 production 纵向路径

`RecordReportQuotaExhaustion` 现在把 `DailySummaryAt`、`Channels` 和 strict-next `BatchAtMS` 传给 `EmitInterrupt`；Gate 也不再传 `nowMS-1`。新增测试覆盖 exact instant 的 helper，并验证手工调用 Report quota owner 后可创建单条 Channel delivery。

但全仓 `RecordReportQuotaExhaustion(` 的唯一调用仍在 `internal/storage/interrupt_test.go`，没有 daemon/control-plane/report runtime producer 调用该 owner。新增测试也没有覆盖 Report quota commit → batch authority/seal → outbox commit wake → webhook worker → completion。因此 production producer 到 delivery 的关闭条件仍不成立。

证据：`internal/storage/report_quota.go:15-25,76-91`，`internal/gate/interrupt.go:11-15`，`internal/storage/interrupt_test.go:333-363`；全仓调用点搜索仅命中声明与该测试。

### P1-2 — OPEN：§6.6 exact bytes 仍只是 adapter 手写 fixture

#522 没有新增 storage sealer exact fixture。现有 `ae3dba…` 仍由 `internal/channelworker/webhook_test.go` 手写 payload 后直接喂 adapter，不能证明 `PrepareAttentionBatch` 从两名真实成员生成 exact bytes/digest、冻结完整 authority，并在 response-loss 后重放相同 operation/key/bytes。canonical alert `ba1805…` 仍无测试，production payload 与 persisted batch/member authority 的逐字段绑定也无 exact evidence。

证据：`internal/channelworker/webhook_test.go:22-49`；全仓只有该测试命中 `ae3dba…`，无测试命中 `ba1805…`。

### P1-3 — OPEN：authority trigger 有增量，完整 durable target/threshold matrix仍缺

`0020_channel_authority_invariants` 编号唯一，并禁止新 authority 行缺 NULL 字段、规格外 `failed` batch state及 sealed authority 改写；`EnqueueChannelPublish` 也开始核验既有 batch payload/key和 delivery binding。这是正确方向。

但 migration没有补齐规格声明的 project FK或完整状态/authority关系；member trigger只比较 Channel ID/snapshot，production `INSERT OR IGNORE` member路径仍没有逐字节核验碰撞行。更关键的是没有新增不同 host/project/target 并发分批、重启、threshold、stale completion、alert target/digest或 sealed mutation定向测试，不能以 trigger文本替代 §6.6 matrix。新增 migration自身还导致 schema version门禁失败。

证据：`internal/storage/migrations/0020_channel_authority_invariants.sql:1-52`，`internal/storage/advance_interrupt.go:330-358`，`internal/storage/channel.go:320-350`，`internal/storage/storage_test.go:88-137`。

### P1-4 — OPEN：HTTP-date 有实现增量，terminal reclaim exact vectors仍为零

HTTP sender现在接受 numeric seconds和未来 HTTP-date `Retry-After`。但实现直接读取 `time.Now()`/`time.Until()`，没有注入时钟或确定性测试。#522 仍未新增 storage §6.6 的 attempt 3 expiry、immutable旧 result、旧 completion stale、无 attempt 4、阈值同 CAS、重启恢复、双 worker、alert失败、第三次 transient/rate-limited与成功清零 vectors。

证据：`internal/channelworker/webhook.go:165-178`；Channel 相关测试仍只有两个 adapter测试，未命中 `lease_expired`、Channel reclaim或 failure episode。

### P1-5 — OPEN：next-retry join和Ledger核验已修，但门禁及可观测性证据未闭合

`ChannelDiagnostics` 已分别 join delivery operation 与 alert operation，`next_attempt_at_ms` 来源修正；batch Ledger 的 `INSERT OR IGNORE` 后也会重读并核验固定 evidence。config新增 Channel ID、capability enum、target closed-object及部分 `secret_ref` 检查。

但没有 `ChannelDiagnostics`、`ops.ps` 或 `ops.doctor` 测试证明 next retry、episode与 alert state在重启后可见，也没有新增 config positive/negative vectors。最重要的是 repository gate 当前确定性失败：migration version/count仍期待19而实际为20；PR #526 的 `vet + test` check为失败。因此 durable projections/config/诊断不能核销。

证据：`internal/storage/channel.go:118-151,221-237`，`internal/config/normalize.go:586-613`，`internal/controlplane/server.go:260-290`，`internal/storage/storage_test.go:88-137`。

### P1-6 — CLOSED，未回退：secret/error-summary边界保持

payload仍保存 `secret_ref:` handle而非解析 endpoint；worker仍只返回 closed error class/summary，现有 query-token负向测试保留。HTTP-date解析未持久化 response body或 credential。本轮未发现 P1-6 回退。

## 3. 关闭条件对账

| #529 / #522 条件 | 结果 | 说明 |
|---|---|---|
| P1-1 Report quota Channel接线 / daily boundary | **NO** | helper与手工 owner测试已修；owner没有 production 调用点，缺纵向 worker测试。 |
| P1-2 §6.6 exact/replay fixtures | **NO** | 仍只有 adapter手写 `ae3dba…`；无 production sealer或 `ba1805…` alert fixture。 |
| P1-3 authority constraints / durable target | **NO** | `0020` trigger有增量；完整关系与并发/碰撞/重启 matrix缺失。 |
| P1-4 terminal reclaim | **NO** | HTTP-date有实现；failure/reclaim exact vectors仍未新增。 |
| P1-5 diagnostics / config / Ledger | **NO** | join与Ledger核验已修；无 `ps`/`doctor`测试，migration gate失败。 |
| P1-6 secret边界不得回退 | **YES** | handle-only payload与closed error summary保持。 |
| migration必须为 `0020_channel_authority_invariants` | **YES** | 文件名和version唯一，未与0019冲突。 |
| 禁止自修自审 | **YES** | #522由Cursor实现；本轮Sol只写复审报告。 |

## 4. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 529`、`gh issue view 529 --comments`（无评论），并回溯 #517/#522/#505/#510及comments、PR #526元数据/checks和完整 diff。
- `git diff a574ec8..HEAD --check`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/channelworker ./internal/storage ./internal/config ./internal/controlplane ./internal/daemon ./internal/gate`：**失败**；确定性阻断为 `TestMigrationRecordedAndIdempotent` 期望19、实际20；并行运行另见已知 doctor时序/资源失败。
- `go test ./...`：**失败**；同一 migration门禁确定性失败，另有 doctor及 launchworker时序/资源失败。
- `go test -race ./internal/storage -run 'TestMigrationRecordedAndIdempotent|TestRecordReportQuotaExhaustionUsesSystemEventIdentity|TestNextDailySummaryAtSkipsTheCurrentOccurrence' -count=1`：**失败**于同一 migration断言。
- PR #526 checks：四平台 build与schema drift通过；`vet + test` **FAIL**。
- 全仓测试搜索：无 `ba1805…`、Channel terminal reclaim/failure episode、`ChannelDiagnostics`、`ops.ps`/`ops.doctor` exact vectors。

## 5. Issue #529 验收清单

- [x] 获取并阅读 #529 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #517 FAIL / #522 复审 P1-1..P1-6：**YES**
- [x] 核验 migration 为 `0020_channel_authority_invariants` 且无 version冲突：**YES**
- [x] 确认 P1-6 不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5 全部关闭：**NO**
- [ ] #522 可核销：**NO**

## 6. 最终裁决

**FAIL。** 先修复并通过 migration version/count门禁；接通 Report quota production caller；补 production sealer `ae3dba…`、canonical alert `ba1805…`、target并发/碰撞、完整 reclaim/terminal/restart/stale matrix及 `ps`/`doctor`/config测试，再由不同代理复审。P1-6 继续保持关闭。
