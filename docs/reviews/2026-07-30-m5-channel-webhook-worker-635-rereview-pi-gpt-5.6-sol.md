FAIL

# M5 #635 Channel webhook after #629 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#629 / PR #632，实现提交 `d760a903dbe1501301d29e297218e5eee38281ac`，合入提交 `2613add286fd4d8b4a4531a59918cfbb72df2175`
> 评审基线：`main` / `origin/main` `2613add`
> 判定基准：[#623 FAIL](2026-07-30-m5-channel-webhook-worker-623-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #629 / PR #632 对实现范围的自述“Legacy NULL critical-limit collision reread + migration 0044; broader P1 matrix still open”准确。四文件 diff 关闭了 #623 新发现的 legacy daily batch 阻断：collision reread改用 nullable critical limits，daily batch新写入保持三列 `NULL`，只对 `critical_fuse` 比较三项 limits；migration `0044_channel_daily_batch_compat.sql` 把 intermediate binary产生的 collecting daily非 NULL值规范回 `NULL`。既有 legacy NULL daily batch现在可再次入批，critical batch仍要求三项 authority非 NULL且逐值相等。

但这不是 #623 P1-1..P1-5 的整体关闭。此次没有修改 Report production vertical、production exact sealer、response-loss replay、canonical failure alert、failure/reclaim concurrency、controlplane或 CLI。P1-1、P1-2、P1-4、P1-5没有实现增量；P1-3仅核销 legacy NULL回退，`i-a/i-b/i-c`并发双 target、critical collision、真实 restart/upgrade、delivered/cancelled逐列矩阵与 §6.6 exact sealing仍缺。P1-6未回退。本轮只新增评审报告，不修改被评审实现或规格。

## 2. #623 P1逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#629 不修改 Report、生产调度/worker或相关验收。`TestReportQuotaCommandRetainsFrozenChannels`仍只直接构造内存 command；没有从真实 `RecordReport` quota exhaustion纵向到 frozen Channel、batch/member、production sealing、outbox wake、webhook及 completion。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#629 不修改 `channel_batch.go`、Channel worker或 failure renderer。全仓 batch digest `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e`仍只在 adapter手写 fixture命中，canonical alert digest `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2`仍无代码测试命中。两名真实 member exact sealing、response-loss后同 key/payload replay及真实第三次 `rate_limited` completion生成 canonical alert仍缺。

### P1-3 — PARTIAL / OPEN：legacy daily NULL阻断关闭，完整 authority矩阵仍缺

有效增量：

- collision reread把三项 limits改为 `sql.NullInt64`，因此 `0027` 合法遗留的 daily `NULL`不再发生 NULL→整数 scan错误；
- daily新 batch明确以 `NULL`保存 critical limits，collision只比较 daily identity；`critical_fuse`仍要求三项 limits有效且逐值匹配；
- `0044`只规范 collecting daily行，保留 sealed历史并遵守 `0042` immutable guard；schema version、migration count和reopen幂等升至 44；
- 新测试命中 legacy NULL collision reread，定向 race重复执行通过。

仍阻断：

- 新测试是在 latest schema DB内手工置 NULL后直接重入，没有构造旧 schema资料库并执行 `0044` 后 close/reopen，因此 migration数据效果和真实 upgrade/restart组合没有回归测试；
- 没有 `i-a/i-b/i-c`并发形成两个稳定 target batch、critical identity collision拒绝、reopen后完整 authority核验、sealed/delivered/cancelled逐状态逐列矩阵；
- 没有把 production sealing联结到 §6.6 exact payload/digest。

故 #623 的具体 legacy NULL production阻断可以核销，但 P1-3总项仍为 PARTIAL。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#629 不修改 failure episode、claim/complete/reclaim或 alert代码。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection`仍是手写不完整 payload、单 DB handle的窄测试。threshold reclaim续租、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert失败不递归及完整 terminal projection assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart与operator surface无增量

#629 不修改 diagnostics、controlplane或 CLI测试。全仓 controlplane/CLI测试仍无 Channel projection断言；缺少重启后 count/state/error/alert key/alert state以及 `ops.ps` / `ops.doctor` 对 delivery/episode/alert/generated-not-delivered的验收。

### P1-6 — CLOSED，未回退

此次只修改 batch critical-limit兼容读取及 migration，没有触及 resolver、sender、sealed payload或错误摘要；`secret_ref:` handle-only与安全错误边界保持。

## 3. migration、CI与执行证据

- `internal/storage/migrations/0044_channel_daily_batch_compat.sql`存在且编号唯一，位于 `0043`后；embedded schema version、migration row count及reopen幂等均为 44。
- `git diff f153348..2613add --check`：通过。
- `go test ./internal/storage -count=1`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- legacy daily / sealed authority / migration定向 race测试 `-count=3`：通过。
- `go vet ./...`：通过。
- `go test ./... -count=1`：通过。
- PR #632 CI：四平台 build、schema drift、`vet + test`全部通过。

## 4. #635验收清单

- [x] 获取并阅读 #635全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #623 FAIL / #629逐项复审 P1-1..P1-5：**YES**
- [x] 严格核验实现方“仅修 legacy NULL + 0044”自述：**YES；具体回退关闭，broader matrix仍开放**
- [x] 核验 migration为 `0044_channel_daily_batch_compat.sql`：**YES**
- [x] `go test ./internal/storage/`：**YES**
- [x] 确认 P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整 concurrency/restart/collision/exact-sealing矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #629可按 #623关闭标尺整体核销：**NO**

## 5. 最终裁决

**FAIL。** #629正确修复了 legacy daily batch三项 critical limits为 `NULL`时的 collision reread，并以 `0044`规范 collecting daily资料；该具体 production upgrade阻断可核销，migration编号和测试基线也正确。但实现方明确未尝试、diff也没有关闭 P1-1/2/4/5，P1-3要求的并发/restart/collision/终态/exact-sealing矩阵仍不完整。后续仍须完成 Report vertical、production exact sealing与response-loss/canonical alert、三 interrupt双 target及 critical collision、failure recovery/concurrency和 `ops.ps`/`ops.doctor`验收。P1-6继续保持关闭。
