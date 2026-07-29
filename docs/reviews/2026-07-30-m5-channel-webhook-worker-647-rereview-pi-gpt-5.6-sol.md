FAIL

# M5 #647 Channel webhook after #641 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#641 / PR #644，实现提交 `544543f03c92e52f155df09639d828036a8620c3`，合入提交 `a2bd374ed6a7cca0a454f42b599acffdfee9bb83`
> 评审基线：`main` / `origin/main` `a2bd374`
> 判定基准：[#635 FAIL](2026-07-30-m5-channel-webhook-worker-635-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #641 / PR #644 对实现范围的自述“Legacy daily authority closure + migration 0046; broader P1 matrix still open”准确。实现 diff 只有 migration `0046_channel_daily_authority_closure.sql` 与 migration测试：它从真实 version 43资料库升级，验证 `0044`把 collecting daily三项 critical limits规范为 `NULL`，验证 reopen，并新增 trigger拒绝 daily INSERT携带 critical limits及 collecting daily UPDATE写入 limits。这补强了 #635 已核销的 legacy daily兼容子路径，但没有实现关闭条件要求的 P1-1/2/4/5，也没有补齐 P1-3 concurrency/upgrade/restart/collision/terminal/exact-sealing矩阵。

`0046`还没有闭合其声明的“every future write”不变量：UPDATE trigger以 `NEW.state='collecting'`为条件，而 `0042` sealed guard只在 `OLD.state<>'collecting'`时生效。因此单条 `collecting -> sealed|delivered|cancelled` UPDATE可以同时把 daily三项 critical limits写成非 NULL。临时 production-schema probe用真实 daily batch执行 `UPDATE ... SET state='sealed', critical_window_ms=1, ...`，数据库接受；probe测试按“应拒绝”稳定失败，随后已删除。故 P1-3要求的 terminal逐状态逐列 authority矩阵不仅仍缺测试，还存在可复现约束缺口。

P1-6未回退。本轮只新增评审报告，不修改被评审实现或规格。

## 2. #635 P1逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#641不修改 Report、生产调度/worker或验收。`TestReportQuotaCommandRetainsFrozenChannels`仍只直接构造内存 command；没有从真实 `RecordReport` quota exhaustion纵向到 frozen Channel、batch/member、production sealing、outbox wake、webhook与 completion。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#641不修改 `channel_batch.go`、Channel worker、failure renderer或相关测试。batch digest `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e`仍只在 adapter手写 fixture命中，canonical alert digest `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2`仍无代码测试命中。两名真实 member exact sealing、response-loss同 key/payload replay及真实第三次 `rate_limited` completion生成 canonical alert仍缺。

### P1-3 — PARTIAL / OPEN：upgrade/reopen子路径增强，完整矩阵未关且terminal guard可绕过

有效增量：

- 新测试从 embedded migration 1–43建立旧资料库，插入 intermediate binary产生的 collecting daily非 NULL limits，再执行 44–46，验证三列归一为 `NULL`及 close/reopen；
- `0046`拒绝 daily INSERT携带 critical limits，也拒绝保持 collecting状态的 daily UPDATE写入 limits；
- schema version、migration count及reopen幂等升至 46；定向 race重复执行通过。

仍阻断：

- 没有 `i-a/i-b/i-c`并发形成两个稳定 target batch、critical identity collision拒绝、完整 authority reopen核验或 §6.6 production exact sealing；
- 没有 sealed/delivered/cancelled逐状态逐列矩阵；
- `collecting -> terminal`与 limits同 UPDATE可绕过 `0046`及`0042`，使 daily terminal row携带非法 critical authority。

因此 legacy upgrade/restart子路径可进一步核销，但 P1-3总项保持 PARTIAL。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#641不修改 failure episode、claim/complete/reclaim、alert或测试。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection`仍是手写不完整 payload、单 DB handle的窄测试。threshold reclaim续租、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert失败不递归及完整 terminal projection assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart与operator surface无增量

#641不修改 diagnostics、controlplane或 CLI。controlplane/CLI测试仍无 Channel projection断言；缺少重启后 count/state/error/alert key/alert state以及 `ops.ps` / `ops.doctor` 对 delivery/episode/alert/generated-not-delivered的验收。

### P1-6 — CLOSED，未回退

此次只增加 daily authority migration与测试，没有触及 resolver、sender、sealed payload或错误摘要；`secret_ref:` handle-only与安全错误边界保持。

## 3. migration、CI与执行证据

- `internal/storage/migrations/0046_channel_daily_authority_closure.sql`存在且编号唯一，位于已含 `0045`的基线上；embedded schema version、migration row count及reopen幂等均为 46。
- `git diff 544543f^..544543f --check`：通过。
- `go test ./internal/storage -count=1`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- migration/legacy/sealed authority定向 race测试 `-count=3`：通过。
- `go vet ./...`：通过。
- `go test ./... -count=1`：失败。`cmd/siftd.TestProductionSchedulerWakesOutboxAfterEnqueueAndEmitInterrupt`单独重跑仍稳定失败于 `invalid interrupt binding identity`；并行全量另遇既有 doctor fixture kill，doctor单独重跑通过。
- PR #644：四平台 build与schema drift通过，`vet + test`失败；除同一 siftd稳定失败外，CI另有 wrapper crash-window失败。因此该 PR的测试门禁不是绿色。
- 临时未提交 terminal-transition probe：真实 daily collecting batch在状态转为 sealed的同一 UPDATE写入三项 critical limits被接受；probe已删除，worktree仅保留本报告。

## 4. #647验收清单

- [x] 获取并阅读 #647全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #635 FAIL / #641逐项复审 P1-1..P1-5：**YES**
- [x] 严格核验实现方“仅 legacy daily + 0046”自述：**YES；自述准确，broader P1 matrix仍开放**
- [x] 核验新 migration从 `0046`起且未回退 `0044`：**YES**
- [x] `go test ./internal/storage/`：**YES**
- [x] 确认 P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整 concurrency/upgrade/restart/collision/terminal/exact-sealing矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #641可按 #635关闭标尺整体核销：**NO**
- [ ] PR #644 `vet + test`门禁通过：**NO**

## 5. 最终裁决

**FAIL。** #641以 migration `0046`和真实 43→46 upgrade/reopen测试补强了 legacy daily critical-limit兼容，编号及已有定向测试正确；但实现方明确未尝试且 diff没有关闭 P1-1/2/4/5，P1-3大部分验收仍缺。新增 trigger还允许 collecting daily在转 terminal的同一 UPDATE注入 critical authority，terminal矩阵存在实质缺口。后续须修正该 trigger边界，并完成 Report production vertical、exact sealing/replay/canonical alert、三 interrupt双 target与 critical collision、failure recovery/concurrency及 `ops.ps`/`ops.doctor`验收；同时恢复全量与 PR `vet + test`门禁。P1-6继续保持关闭。
