FAIL

# M5 #623 Channel webhook after #617 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#617 / PR #620，实现提交 `1802265b321544a89b9d69bffca93cc22057539c`，合入提交 `4e6bbb0e993c88ce3405cec6ed3bfd9b6e901754`
> 评审基线：`main` / `origin/main` `4e6bbb0`
> 判定基准：[#612 FAIL](2026-07-30-m5-channel-webhook-worker-612-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #617 / PR #620 的实现方自述“P1-1/2/4/5 still open”准确。本次四文件 diff 只补 #612 指出的 P1-3 critical authority 子路径：migration `0042_channel_sealed_critical_authority.sql` 把 `episode_admission_id`、`critical_window_ms`、`critical_total_limit`、`critical_per_run_limit` 纳入 sealed batch UPDATE guard；`addBatchMemberTx` 的既有 batch collision reread也纳入这四列。production sealing 后的四列直接 SQL 改写负测通过，故 #612 明示的 critical authority 漏列可核销。

但 P1-3 总项仍未关闭，而且 collision reread引入一个 upgrade/restart 阻断：`0027_advance_interrupt_p1_closure.sql` 新增三项 critical limit列时只回填 `kind='critical_fuse'`，历史 `daily_summary` batch允许三列保持 `NULL`；新代码却把三列直接 `Scan` 到 `int64/int`。临时 production-schema probe 将 collecting daily batch还原为该合法历史形态后再次走 `addBatchMemberTx`，稳定返回 `converting NULL to int64 is unsupported`，无法继续入批。`0042` 没有回填或约束这些历史行，代码也未使用 nullable scan。因此要求中的 reopen/upgrade authority路径发生回退。

此外仍没有 `i-a/i-b/i-c` 并发双 batch、critical collision拒绝、关闭重开、delivered/cancelled逐列矩阵或 §6.6 exact sealing测试。P1-1、P1-2、P1-4、P1-5均无实现增量；P1-6 的 secret/error-summary边界未回退。本轮只新增评审报告，不修改被评审实现或规格。

## 2. #612 P1 逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#617 不修改 Report、生产调度/worker或相关验收。`TestReportQuotaCommandRetainsFrozenChannels` 仍只直接构造内存 command；没有从真实 `RecordReport` quota exhaustion纵向到 frozen Channel、batch/member、production sealing、outbox wake、webhook和 completion。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#617 不修改 `channel_batch.go`、Channel worker或 failure renderer。全仓 batch digest `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 仍只在 adapter手写 fixture命中，canonical alert digest `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 仍无测试命中。两名真实 member exact sealing、response-loss后同 key/payload replay及真实第三次 `rate_limited` completion生成 canonical alert仍缺。

### P1-3 — PARTIAL / OPEN：critical漏列已关，但完整矩阵与历史daily reopen失败

有效增量：

- `0042` 替换 `0040` trigger，sealed/delivered/cancelled batch的四项 critical authority列现在不可直接改写；
- collision reread在代码层比较 `episode_admission_id` 与三项 critical limits，非 critical episode继续以 SQL `NULL`保存；
- 新增 sealed production负测覆盖四列，schema version/count/reopen幂等升至 42。

仍阻断：

- `0027` 合法遗留的 daily batch三项 `NULL` 会被新 collision reread扫描到非 nullable Go整数并报错；`0042` 未迁移这些行；
- 新测试只验证普通 daily batch sealed 后改写失败，没有命中 critical batch或 collision reread；
- 没有三 interrupt并发、双稳定 target batch、critical identity collision拒绝、reopen后 authority核验、delivered/cancelled逐状态逐列矩阵及 production exact payload联结。

因此只能核销 critical sealed漏列子路径，P1-3保持 PARTIAL。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#617 不修改 failure episode、claim/complete/reclaim或 alert代码。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍是手写不完整 payload、单 DB handle的窄测试。threshold reclaim续租、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert失败不递归及完整 terminal projection assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart与operator surface无增量

#617 不修改 diagnostics、controlplane或 CLI测试。全仓 controlplane/CLI测试仍无 Channel projection断言；缺少重启后 count/state/error/alert key/alert state以及 `ops.ps` / `ops.doctor` 对 delivery/episode/alert/generated-not-delivered的验收。

### P1-6 — CLOSED，未回退

本次只修改 batch identity读取和schema guard，没有触及 resolver、sender、payload或错误摘要；`secret_ref:` handle-only与安全错误边界保持。

## 3. migration、CI与执行证据

- `internal/storage/migrations/0042_channel_sealed_critical_authority.sql` 存在且编号唯一，位于 `0041` 后；embedded schema version、migration row count及reopen幂等均为 42。
- `git diff 4e6bbb0^1..4e6bbb0 --check`：通过。
- `go test ./internal/storage/ -count=1`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- P1-3/diagnostics/migration定向 race测试 `-count=3`：通过。
- `go vet ./...`：通过。
- PR #620 四平台 build、schema drift、`vet + test`：通过。
- `go test ./... -count=1`：失败于既有并行时序 flake：`TestDoctorBaselineChecksConfiguredDependencies` 的 fixture command被 kill，`TestLaunchWorkerWrapperCrashSuite` 未观察到 marker；两项单独重跑均通过。组合定向包测试也复现 doctor fixture kill。这些不来自 #617 diff，但本地全量不能记绿。
- 临时未提交 probe：历史 daily batch三项 critical列为 `NULL` 时，`addBatchMemberTx` 的新 reread稳定发生 NULL→`int64` scan错误；probe已删除，worktree仅保留本报告。

## 4. #623 验收清单

- [x] 获取并阅读 #623全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #612 FAIL / #617逐项复审 P1-1..P1-5：**YES**
- [x] 严格核验实现方“仅关 P1-3 critical authority”自述：**YES；sealed漏列子路径关闭，但发现历史daily reopen回退**
- [x] 核验 migration为 `0042_channel_sealed_critical_authority.sql`：**YES**
- [x] `go test ./internal/storage/`：**YES**
- [x] 确认 P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整 concurrency/restart/collision/exact-sealing矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #617可按 #612关闭标尺核销：**NO**

## 5. 最终裁决

**FAIL。** migration `0042` 编号和 sealed critical authority trigger正确，代码也补入 critical collision比较，足以核销 #612 指出的四列漏项；但 P1-1/2/4/5没有增量，P1-3的完整矩阵仍缺，且新 non-nullable reread不能处理 `0027` 合法遗留的 daily batch NULL limits。后续至少须迁移或兼容历史 daily行并增加 upgrade/reopen回归测试，再补齐 Report vertical、exact sealing/replay/canonical alert、authority并发/collision/终态、failure recovery/concurrency及 `ops.ps`/`ops.doctor`验收。P1-6继续保持关闭。
