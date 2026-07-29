FAIL

# M5 #612 Channel webhook after #606 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#606 / PR #609，实现提交 `b42c945717fa339b38a256123e94cd44f1951ea5`，合入提交 `9f14a4e`
> 评审基线：`main` / `origin/main` `9f14a4e`
> 判定基准：[#600 FAIL](2026-07-30-m5-channel-webhook-worker-600-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #606 / PR #609 的实现方自述“broader P1-1/5 still open”属实，而且本次也没有关闭 P1-2、P1-4。实现 diff 只有四个 storage 文件：`addBatchMemberTx` 增加既有 batch 的部分 identity reread，migration `0040_channel_sealing_matrix.sql` 增加 sealed batch/member UPDATE/DELETE guards，一个既有 sealed 测试只新增 `nonce` 负测，以及 schema version/count 升至 40。它没有修改 Report production vertical、production sealer exact fixture、Channel worker replay、failure/reclaim matrix、controlplane 或 CLI 验收。

P1-3 获得有效但不完整的增量：sealed batch 的多数 identity/payload 字段和整行 member history 现被保护，既有 batch collision 也会比较多数 daily identity 字段。但 `0040` 漏掉 `episode_admission_id`、`critical_window_ms`、`critical_total_limit`、`critical_per_run_limit`；临时 production-schema probe 证明 sealed 后四列均可由直接 SQL 成功改写。`addBatchMemberTx` 的 collision reread也不读取或比较这些 critical authority 列。新增测试仍没有 `i-a/i-b/i-c` 并发双 batch、collision拒绝、关闭重开、critical identity、逐状态/逐列矩阵或 §6.6 exact sealing。

migration编号 **0040** 唯一，schema version/count/reopen幂等测试正确，`go test ./internal/storage/` 通过。P1-6 的 secret/error-summary边界未回退。本轮只新增评审报告，不修改被评审实现或规格。

## 2. #600 P1 逐项复审

### P1-1 — PARTIAL / OPEN：Report production vertical无增量

#606 不修改 Report、生产调度/worker或相关测试。`TestReportQuotaCommandRetainsFrozenChannels` 仍只直接构造内存 command；没有从真实 `RecordReport` quota exhaustion纵向到 frozen Channel、batch/member、production sealing、outbox wake、webhook和 completion的验收。

### P1-2 — OPEN：exact sealer、canonical alert与response-loss replay无增量

#606 不修改 `channel_batch.go`、Channel worker或 failure renderer，也未新增对应测试。全仓 `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 仍只由 `internal/channelworker/webhook_test.go` 的手写 adapter payload命中；canonical alert digest `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 仍无测试命中。两名真实 member exact sealing、远端成功/本地 response-loss 后同 key/payload replay、真实第三次 `rate_limited` completion生成 canonical alert均缺。

### P1-3 — PARTIAL / OPEN：sealed guards与部分 collision reread有效，完整矩阵及 critical authority仍缺

有效增量：

- `0040` 冻结 sealed batch 的多数 identity、operation、payload与时间字段，拒绝 sealed member任意 UPDATE/DELETE，并拒绝 sealed batch DELETE；
- `addBatchMemberTx` 在 `INSERT OR IGNORE` 后 reread并比较 project/channel/target/kind/delivery/scope/due等字段，可拒绝这部分 identity collision；
- sealed test新增 member `nonce` 改写负测。

仍阻断：

- `0040` 未比较 `episode_admission_id` 及三项 critical fuse冻结配置；临时 probe 在 production migration schema上 sealed batch后逐列 UPDATE，四次都成功；
- collision reread同样漏掉上述四列，不能证明 critical batch authority collision被拒绝；
- 没有 `i-a/i-b/i-c` 并发形成两个稳定 target batch、collision测试、reopen后 authority核验、critical identity fixture、delivered/cancelled逐列矩阵；
- 没有把最终 production sealing连接到 §6.6 exact payload/digest。

故 P1-3只能保持 PARTIAL。

### P1-4 — PARTIAL / OPEN：failure/reclaim/concurrency矩阵无增量

#606 不修改 failure episode、claim/complete/reclaim或 alert代码。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍是手写不完整 payload、单 DB handle、两次 completion后 attempt 3 terminal reclaim的窄测试。threshold reclaim后续 lease、并发双 worker、第三次 rate-limited canonical alert、restart count=2后唯一 alert、success清零、alert失败不递归及完整 terminal projection assertions仍缺。

### P1-5 — PARTIAL / OPEN：restart与 operator surface无增量

#606 不修改 diagnostics、controlplane或 CLI测试。没有重启后 count/state/error/alert key/alert state断言，也没有 `ops.ps` / `ops.doctor` 对 Channel delivery/episode/alert/generated-not-delivered投影的 RPC/CLI验收。direct storage测试不能替代 operator surface。

### P1-6 — CLOSED，未回退

#606 只修改 batch identity/schema guards，没有触及 resolver、sender、payload或错误摘要；`secret_ref:` handle-only与安全错误边界保持。

## 3. migration、CI与测试核验

- `internal/storage/migrations/0040_channel_sealing_matrix.sql` 存在且编号唯一；embedded schema version和 migration row count均为 40，reopen幂等测试通过。
- migration意图有效但列矩阵不完整：sealed `episode_admission_id` 与三项 critical配置仍可改写。
- `git diff 9f14a4e^1..9f14a4e --check`：通过。
- `go test ./internal/storage/ -count=1`：通过。
- `go test ./internal/channelworker -run TestWebhook -count=10`：通过。
- 定向 Channel storage race测试：通过。
- `go vet ./...`：通过。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：通过。
- `go test ./... -count=1`：失败；`internal/intake` 有 5 个 `invalid interrupt binding provenance` 失败，`internal/launchworker` 与 `internal/wrapper` 各有 crash-window spawn断言失败。PR #609 的 `vet + test` check同样为失败；四平台 build和 schema drift通过。这些失败不来自 #606 的四文件 diff，但本基线不能记全量绿。

## 4. #612 验收清单

- [x] 获取并阅读 #612全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #600 FAIL / #606逐项复审 P1-1..P1-5：**YES**
- [x] 核验 migration为 `0040_channel_sealing_matrix.sql`：**YES**
- [x] `go test ./internal/storage/`：**YES**
- [x] 确认 `0035` / `0037` / `BatchAtMS` / hooks 与 P1-6未回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1关闭：**NO / PARTIAL**
- [ ] P1-2关闭：**NO**
- [ ] P1-3完整 concurrency/restart/collision/exact-sealing矩阵关闭：**NO / PARTIAL**
- [ ] P1-4关闭：**NO / PARTIAL**
- [ ] P1-5关闭：**NO / PARTIAL**
- [ ] #606可按 #600关闭标尺核销：**NO**

## 5. 最终裁决

**FAIL。** `0040` 编号、schema升级、部分 sealed guard及部分 collision reread正确，但 migration和reread均遗漏 critical authority列，且要求的并发/restart/collision/exact-sealing测试矩阵不存在。P1-1/2/4/5没有实现增量；PR自身也明确承认 broader P1仍开放。后续须冻结并比较 `episode_admission_id` 与三项 critical配置，补齐三 interrupt双 target、collision/reopen/各终态和 exact sealing矩阵，并完成 Report vertical、response-loss/canonical alert、failure recovery/concurrency及 `ops.ps`/`ops.doctor`验收。P1-6继续保持关闭。
