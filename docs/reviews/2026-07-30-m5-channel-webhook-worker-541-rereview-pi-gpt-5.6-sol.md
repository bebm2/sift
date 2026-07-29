FAIL

# M5 #541 Channel webhook after #534 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#534 / PR #538，实现提交 `4ba7ece48372987edbc0eac8e6eccf3a7d96b474`，合入提交 `73c7476`
> 评审基线：`main` / `origin/main` `73c7476`
> 判定基准：[#529 FAIL](2026-07-30-m5-channel-webhook-worker-529-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #534 的有效增量是：按 conductor 要求新增 `0023_channel_target_matrix`，为新 member 校验 Run/项目/完整 Forge target，为 member identity 增加 update immutability；migration version/count 门禁已更新为 23；HTTP-date `Retry-After` 改用可注入时钟并增加确定性 adapter 测试。PR #538 的全部 checks 通过，#529 的 migration 门禁阻断已关闭。

但实现方自述的 P1 未完全关闭属实。#534 没有增加 production sealer/canonical alert exact fixture，没有 failure episode/reclaim/restart/stale-worker/terminal matrix，也没有 `ChannelDiagnostics`、`ops.ps`、`ops.doctor` 或 Channel config 定向测试。当前 `RecordReport` 虽已成为 `RecordReportQuotaExhaustion` 的 production caller，却没有从 config snapshot 解出或传入 `attention.channels`，因此 quota exhaustion 的生产路径仍以空 Channel 列表调用 owner，无法建立 Report quota → Channel delivery 的纵向证据。

此外，`0023` 只在 member INSERT 时比较 target；既有 collecting batch 仍可在已有 member 后改写 project/channel/Forge target，因为 `0020` 的 batch authority immutability 只保护 `sealed|delivered|cancelled`。production `INSERT OR IGNORE` 的碰撞重读也仍只比较 channel/snapshot/version/nonce，而没有逐字节核验 admission、member/delivery identity及冻结展示字段。故 P1-1..P1-5 不能核销；P1-6 未发现回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #529 P1 逐项复审

### P1-1 — OPEN：有 production caller，但 production Channel 参数仍丢失

当前 `RecordReport` 在 blocker 子配额耗尽并提交 rate token 后调用 `RecordReportQuotaExhaustion`，关闭了“完全没有 production caller”的旧缺口；owner 也保留 strict-next daily-summary 计算。

但 `reportRuntimeConfig` 不含 `attention.channels`，`reportQuotaCmd` 没有设置 `Channels`，最终 `EmitInterruptCmd.Channels` 为空。现有测试仍只手工调用 owner并显式构造 Channel，未从 `RecordReport` 触发 quota exhaustion，更未覆盖 batch authority/seal、outbox commit wake、webhook worker与 completion。因此 production Report quota 路径不能创建所要求的 Channel delivery。

证据：`internal/storage/report.go:145-150,220-222`，`internal/storage/report_quota.go:76-91`，`internal/storage/interrupt_test.go:333-363`。

### P1-2 — OPEN：§6.6 exact bytes仍未由 production sealer生成

#534 没有修改 `PrepareDueAttentionBatches` 或增加 storage batch sealing测试。`ae3dba…` 仍只在 `internal/channelworker/webhook_test.go` 中由手写 payload命中；全仓测试仍不命中 canonical alert digest `ba1805…`。没有测试证明两名真实成员经 production sealer得到 §6.6 exact bytes/digest，也没有 response-loss后同 operation/key/payload replay证据。

证据：`internal/channelworker/webhook_test.go:22-49`，`internal/storage/channel_batch.go:35-119`；测试搜索无 `ba180536…`。

### P1-3 — OPEN：`0023` 编号与部分 trigger正确，durable target matrix仍不完整

`0023_channel_target_matrix` 编号唯一且 migration gate已更新。新增 INSERT trigger会把 member所属 Run的 project与完整 Forge target同 batch比较；member authority除 `excluded_at_ms` 外不可更新。这些是有效修复。

但完整冻结矩阵仍未成立：

1. collecting batch已有 member后仍可改写 project/channel/snapshot/Forge target；sealed-only trigger不会阻止该 retarget，`0023` 也没有 batch UPDATE校验。
2. `attention_batches.project_id` 仍不是 project FK。
3. `INSERT OR IGNORE` 后只重读比较 `channel_id/channel_snapshot_json/interrupt_version/nonce`，未核验 `admission_id/member_key/delivery_id/headline/reason/severity/links/options/joined_at_ms` 的碰撞行。
4. 没有新增 migration trigger测试，亦无不同 host/project/target并发分批、碰撞、重启、threshold、stale completion或sealed mutation matrix。

证据：`internal/storage/migrations/0013_advance_interrupt_closure.sql:31-61`，`0020_channel_authority_invariants.sql:29-44`，`0023_channel_target_matrix.sql:1-30`，`internal/storage/advance_interrupt.go:330-379`。

### P1-4 — OPEN：HTTP-date确定性已修，terminal reclaim exact vectors仍缺

`HTTPWebhookSender.Now` 使未来 HTTP-date相对时间可确定测试，新增用例验证固定时钟下2秒 `Retry-After`；该窄项已关闭。

但 #534 没有增加 attempt 3 lease expiry、immutable旧 result、旧 completion stale、无 attempt 4、threshold同 CAS、重启恢复、双 worker、alert失败、第三次 transient/rate-limited和成功清零测试。现有 Channel adapter测试不能替代 storage §6.6 的 terminal/reclaim matrix。

证据：`internal/channelworker/webhook.go:147-187`，`internal/channelworker/webhook_test.go:68-82`；Channel相关测试未调用 failure episode/reclaim production路径。

### P1-5 — OPEN：migration门禁关闭，diagnostics/config证据仍缺

schema version/count已更新到23，PR #538 的 `vet + test`、schema drift与四平台build全部通过；本轮 targeted packages也通过。#529 的确定性 migration gate失败已关闭。

但没有测试调用 `ChannelDiagnostics`，也没有通过 `ops.ps`/`ops.doctor` 断言 next retry、failure count、episode、alert state及 generated-not-delivered在重启后可见。#534 同样没有新增 Channel config positive/negative vectors。实现存在不等于验收证据闭合。

证据：`internal/storage/channel.go:116-151`，`internal/controlplane/server.go:251-290`；全仓测试搜索无 `ChannelDiagnostics` 调用。

### P1-6 — CLOSED，未回退：secret/error-summary边界保持

payload仍只保存 `secret_ref:` handle；adapter仍在 attempt时解析 endpoint；新增时钟只影响 HTTP-date退避计算。现有 query secret与sender error负向测试保留，未发现 endpoint、credential、response body或原始错误泄漏。

## 3. 关闭条件对账

| #541 条件 | 结果 | 说明 |
|---|---|---|
| P1-1 Report quota Channel生产接线 | **NO** | caller已存在，但production config不传 Channels；无纵向测试。 |
| P1-2 §6.6 sealer/alert exact与replay | **NO** | 仍是adapter手写 `ae3dba…`；无production sealer与 `ba1805…`。 |
| P1-3 authority/target matrix | **NO** | `0023` member trigger有增量；collecting batch可retarget、碰撞重读与测试矩阵不完整。 |
| P1-4 reclaim/restart/stale-worker/terminal vectors | **NO** | HTTP-date确定性已修；storage exact vectors仍缺。 |
| P1-5 ps/doctor/config/Ledger diagnostics | **NO** | migration gate已修；diagnostics与config定向测试仍缺。 |
| P1-6 secret边界不得回退 | **YES** | handle-only payload与closed error summary保持。 |
| migration为 `0023_channel_target_matrix` | **YES** | 文件名/version/count均为23且唯一。 |
| 禁止自修自审 | **YES** | #534由Cursor实现；本轮Sol只写复审报告。 |

## 4. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 541`、`gh issue view 541 --comments`，并回溯 #529/#534及comments、PR #538 body/files/checks与完整实现diff。
- `git diff 73c7476^..73c7476 --check`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/channelworker ./internal/storage ./internal/config ./internal/controlplane ./internal/daemon ./internal/gate`：**通过**。
- `go test -race ./internal/storage ./internal/channelworker -run 'Test(MigrationRecordedAndIdempotent|RecordReportQuotaExhaustionUsesSystemEventIdentity|HTTPWebhookSenderHTTPDateRetryAfterUsesInjectedClock)$' -count=1`：**通过**。
- `go test ./...`：除 `internal/launchworker/TestLaunchWorkerKilledAfterRealWrapperSpawn/control-initial-write` 等待 `control.json` 超时外均通过；该既有时序/资源失败与本轮 Channel diff无直接关联。
- PR #538 checks：`vet + test`、schema drift、darwin/linux × amd64/arm64 build均 **PASS**。
- 全仓测试搜索：无 `ba180536…`，无 `PrepareDueAttentionBatches`/`ChannelDiagnostics` 测试调用，无 Channel failure episode/reclaim exact matrix。

## 5. Issue #541 验收清单

- [x] 获取并阅读 #541 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #529 FAIL / #534 复审 P1-1..P1-6：**YES**
- [x] 严格核验实现方“P1未完全关闭”的自述：**YES，属实**
- [x] 核验 migration为 `0023_channel_target_matrix`：**YES**
- [x] 确认 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #534可核销：**NO**

## 6. 最终裁决

**FAIL。** 先把冻结 Channel registry从 production Report snapshot传入 quota owner；补 production sealer `ae3dba…`、canonical alert `ba1805…`及response-loss replay；令 batch authority从 collecting创建起不可retarget并补完整碰撞核验；覆盖 reclaim/terminal/restart/stale-worker/alert failure exact vectors，以及 `ChannelDiagnostics`、`ops.ps`、`ops.doctor`和config定向测试，再由不同代理复审。P1-6继续保持关闭。
