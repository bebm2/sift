FAIL

# M5 #659 Channel webhook after #653 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#653 / PR #656，实现提交 `43319443889caf20b0c6fdc277e6a8c53ef5c2a0`，合入提交 `59dd9dbde0e5dbc0b5f201a8b86e9df834355a4b`
> 评审基线：`main` / `origin/main` `59dd9db`
> 判定基准：[#647 FAIL](2026-07-30-m5-channel-webhook-worker-647-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #653 / PR #656 对实现范围的自述“Terminal critical-limits race closed; migration renumbered to 0049 after #652; P1-1/2/4/5 still open”准确。实现 diff 仅有 migration `0049_channel_daily_terminal_authority.sql`、terminal transition 回归测试与 schema version/count 更新。新 trigger 以 `OLD.state='collecting' AND NEW.kind='daily_summary'`约束 UPDATE，并对三项 critical limits 使用 OR 判定，故 #647 复现的单条 `collecting -> sealed|delivered|cancelled` UPDATE 同时注入 critical authority 已被拒绝；失败语句保持 batch 为 `collecting`。该具体 terminal authority race 可以核销，migration 编号也满足 #659 明确要求的 **0049**。

这不构成 #647 P1-1..P1-5 的整体关闭。diff 没有修改 Report production vertical、production exact sealer/replay、failure episode/reclaim/concurrency、controlplane 或 CLI。P1-1、P1-2、P1-4、P1-5仍开放；P1-3只增加 terminal race 子路径，`i-a/i-b/i-c`并发双 target、critical identity collision、完整 restart/upgrade authority、§6.6 production exact sealing仍缺。新增测试按三个 terminal state 覆盖，但每次同时写三列，仍不是此前要求的逐状态逐列回归矩阵。P1-6未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #647 P1逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#653不修改 Report、生产调度/worker或验收。`TestReportQuotaCommandRetainsFrozenChannels`仍只直接构造内存 command；没有从真实 `RecordReport` quota exhaustion纵向到 frozen Channel、batch/member、production sealing、outbox wake、webhook与 completion。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#653不修改 `channel_batch.go`、Channel worker、failure renderer或相关测试。batch digest `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e`仍只在 adapter 手写 fixture 命中；canonical alert digest `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2`仍无代码测试命中。两名真实 member exact sealing、response-loss 后同 key/payload replay及真实第三次 `rate_limited` completion生成 canonical alert仍缺。

### P1-3 — PARTIAL / OPEN：terminal race关闭，完整 authority矩阵仍未关闭

有效增量：

- `0049`新增独立 UPDATE trigger，以 OLD collecting authority封住同一语句先转 terminal、再携带 critical limits的绕过；
- trigger 的 OR 谓词覆盖 `critical_window_ms`、`critical_total_limit`、`critical_per_run_limit`任一非 NULL；
- 回归测试覆盖 `sealed`、`delivered`、`cancelled`三种目标状态，并确认拒绝后仍为 `collecting`；
- schema version、migration row count与 reopen 幂等升至 49；定向测试重复执行通过。

仍阻断：

- 没有 `i-a/i-b/i-c`并发形成两个稳定 target batch、critical identity collision拒绝或 §6.6 production exact sealing；
- 没有覆盖完整 upgrade/restart 后 authority及 production sealing组合；
- terminal测试每个状态一次性写三列，没有逐状态逐列形成独立回归向量。

因此 #647 的具体 terminal-transition 漏洞已关闭，但 P1-3总项保持 PARTIAL。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#653不修改 failure episode、claim/complete/reclaim、alert或测试。`TestChannelDiagnosticsIncludesBatchFailureProjection`仍是手写不完整 payload、单 DB handle的窄测试。threshold reclaim续租、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert失败不递归及完整 terminal projection assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart与operator surface无增量

#653不修改 diagnostics、controlplane或 CLI。controlplane/CLI测试仍无 Channel projection断言；缺少重启后 count/state/error/alert key/alert state以及 `ops.ps` / `ops.doctor` 对 delivery/episode/alert/generated-not-delivered的验收。

### P1-6 — CLOSED，未回退

此次只增加数据库 authority trigger和测试，没有触及 resolver、sender、sealed payload或错误摘要；`secret_ref:` handle-only与安全错误边界保持。

## 3. migration、CI与执行证据

- `internal/storage/migrations/0049_channel_daily_terminal_authority.sql`存在且编号唯一，位于含 `0047`、`0048`的合入基线上；embedded schema version、migration row count及 reopen 幂等均为 49。
- `git diff ff3be3f..59dd9db --check`：通过；实现 diff仅 3 文件、51 行新增/4 行修改。
- `go test ./internal/storage -count=1`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- terminal/legacy/sealed/migration定向测试 `-count=10`：通过。
- `go vet ./...`：通过。
- `go test ./... -count=1`：失败于 `internal/controlplane.TestDoctorBaselineChecksConfiguredDependencies` 的 fixture command `signal: killed`及 `internal/launchworker.TestLaunchWorkerWrapperCrashSuite` 的 crash marker时序；两项各自单独重跑均通过，符合既有并行时序 flake 特征，未发现与 #653 三文件 diff 的因果关系。
- PR #656：四平台 build、schema drift、`vet + test`全部通过。

## 4. #659验收清单

- [x] 获取并阅读 #659全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #647 FAIL / #653逐项复审 P1-1..P1-5：**YES**
- [x] 严格核验实现方“仅关 terminal race”自述：**YES；自述准确，broader P1 matrix仍开放**
- [x] 核验新 migration为 `0049_channel_daily_terminal_authority.sql`：**YES**
- [x] #647 terminal-transition critical authority race关闭：**YES**
- [x] `go test ./internal/storage/`：**YES**
- [x] 确认 P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整 concurrency/upgrade/restart/collision/terminal逐列/exact-sealing矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #653可按 #647关闭标尺整体核销：**NO**
- [x] PR #656 `vet + test`已有最终绿色结论：**YES**

## 5. 最终裁决

**FAIL。** #653以正确编号的 migration `0049`封住了 #647 发现的 collecting daily 在转 terminal同一 UPDATE 中注入 critical authority的漏洞，三种 terminal状态回归及 schema/reopen基线正确；该具体 race可以核销。但实现方明确未尝试且 diff没有关闭 P1-1/2/4/5，P1-3要求的并发双 target、critical collision、完整 upgrade/restart authority、逐状态逐列回归与 production exact sealing仍不完整。后续仍须完成 Report production vertical、exact sealing/replay/canonical alert、failure recovery/concurrency和 `ops.ps`/`ops.doctor`验收。P1-6继续保持关闭。
