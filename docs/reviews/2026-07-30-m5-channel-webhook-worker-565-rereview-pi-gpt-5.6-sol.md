FAIL

# M5 #565 Channel webhook after #558 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#558 / PR #562，实现提交 `53436f6f15c8908f7ef6f14ee7b0c8d8b5168b3a`，合入提交 `de9cbae`
> 评审基线：`main` / `origin/main` `de9cbae`
> 判定基准：[#553 FAIL](2026-07-30-m5-channel-webhook-worker-553-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #558 / PR #562 的实现 diff 只有一条 `0029_channel_alert_contract` trigger、一个 33 行的手工 `EnqueueOperation` schema test，以及 schema version/count 更新；migration 编号与 conductor 指定的 `0029_channel_alert_contract` 一致且唯一，但实现方自述 evidence matrix 未齐属实。该 diff 没有调用 `PrepareDueAttentionBatches`，没有命中 §6.6 的 `ae3dba99…` 或 `ba180536…`，没有 response-loss replay，也没有修改或新增 Report quota vertical、batch authority matrix、failure/reclaim/restart/alert-failure、`ops.ps`/`ops.doctor` 证据。

新增 trigger 是有效的窄 schema guard：以 Channel failure key 入队的 `forge_alert` 必须是七个非空文本字段且 `purpose=channel_failure`。但其正向测试手写的 `markdown` 只有 marker，不是 §6.6 canonical alert；测试直接调用通用入队口，不经过第三次 failure、episode threshold 或 production renderer。它不能替代 #553 明列的 exact alert bytes/digest，更不能补 sealer fixture。

主干上随 #557 合入的 `TestRecordBlockerReportKeepsRunningRun` 确实从 `RecordReport` 到达 quota exhaustion，但其 config snapshot 没有 `attention.channels`，断言也止于 `report_quota_exhaustions`；没有证明 quota Interrupt 建立 Channel delivery、batch/member、sealed operation或 worker completion。因此 #553 的 P1-1 仍未形成 Channel 纵向证据。P1-3/P1-4/P1-5 的既有窄增量保留但没有被 #558 补齐；P1-6 未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #553 P1 逐项复审

### P1-1 — PARTIAL / OPEN：RecordReport quota 已可达，Channel vertical 仍缺

当前 `internal/storage/report_test.go:8-67` 会经 `RecordReport` 触发 quota exhaustion，是相对 #553 基线的有效交叉增量。但 fixture 的 config JSON 没有 Channel registry，测试只断言 rate token与一条 `report_quota_exhaustions`，没有断言 quota Interrupt 的 Channel delivery、batch/member、`PrepareDueAttentionBatches`、outbox wake/webhook/completion。#558 自身也没有修改 Report 代码或测试。因此要求的 Report quota → Channel production vertical 仍为 NO。

### P1-2 — OPEN：exact sealer、canonical alert及 replay 均未交付

全仓 production/test 搜索仍显示：

- `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 只在 `internal/channelworker/webhook_test.go` 的手写 adapter payload；
- `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 没有测试命中；
- 没有测试调用 `PrepareDueAttentionBatches`。

`internal/storage/channel_alert_contract_test.go:8-32` 直接手写 `forge_alert` 并调用 `EnqueueOperation`；正向 payload 的 target/markdown也不是 §6.6 fixture。它没有两名真实 admission/member、production sealing、逐字节 payload/digest、第三次 rate-limit触发的 canonical alert，或 response-loss 后相同 operation key/payload replay。P1-2 明确未关闭。

### P1-3 — PARTIAL / OPEN：既有 authority 修复保留，完整 matrix 未补

#553 已确认 `0026_channel_closure` 对 membered collecting batch authority 与 member collision 的代码修复有效。#558 新增的 `0029` 只约束 Channel failure alert payload，不涉及 project/host/project-key/target、并发分批、碰撞重读或 reopen 后 authority。既有测试仍仅以单列 `forge_host` UPDATE 验证 retarget拒绝。完整逐列、并发与重启 matrix 仍缺。

### P1-4 — PARTIAL / OPEN：alert schema guard 不是 failure/reclaim matrix

既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍使用手写单成员 payload和同一个打开的 DB，只覆盖两次 completion后 attempt 3 terminal reclaim、旧 completion stale及无 attempt 4。#558 的新测试完全不执行 Channel operation、episode或 claim/complete/reclaim。

因此 threshold reclaim 后继续 lease、并发双 worker、第三次 rate-limited completion、重启 count=2 后唯一 alert、success清零、alert自身失败不递归，以及完整 terminal delivery/episode/alert exact assertions仍缺。P1-4 未关闭。

### P1-5 — PARTIAL / OPEN：无 restart diagnostics 或 ops.ps/doctor 证据

全仓测试中仍只有 storage test 直接调用 `ChannelDiagnostics`；`internal/controlplane/*_test.go`、`cmd/sift/*_test.go` 没有 `channel_deliveries`、episode、alert或 generated-not-delivered断言。`ReadDoctorState` 也只读 hook/attempt 投影。#558 未修改 diagnostics、controlplane、CLI或 config 测试。故 reopen 后持久投影与 `ops.ps`/`ops.doctor` 验收仍未交付。

### P1-6 — CLOSED，未回退：secret/error-summary 边界保持

#558 不修改 Channel payload、worker、resolver、completion或 diagnostics；payload仍保存 `secret_ref:` handle，既有 query-secret/sender-error负向测试保持通过。新增 alert trigger与测试也未引入 endpoint、credential、response body或原始 sender error。P1-6 继续 CLOSED。

## 3. 新 migration 核验

`internal/storage/migrations/0029_channel_alert_contract.sql` 文件名、嵌入顺序、schema version和 migration row count均为 29；仓库中 `0027`、`0028`、`0029` 各唯一。`TestMigrationRecordedAndIdempotent` 与 storage suite通过。故 #565 特别指定的重编号要求为 **YES**。

该 migration只提供窄 closed-field guard，不应被描述为 §6.6 exact fixture：trigger不验证 canonical bytes/digest、operation key与 marker/subject/generation绑定，也不验证 markdown固定行；其正向测试自身即只提供 marker。此不足计入 P1-2，而不否定 migration编号与幂等加载。

## 4. 关闭条件对账

| #565 条件 | 结果 | 说明 |
|---|---|---|
| 对照 #553 FAIL / #558 复审 P1-1..P1-5 | **YES** | 已逐项复审；五项均未全部关闭。 |
| P1-1 Report quota Channel vertical | **NO / PARTIAL** | RecordReport quota可达，但 fixture无 Channels，未到 Channel delivery/sealer/worker。 |
| P1-2 exact sealer/alert fixtures + replay | **NO** | 无 production sealer调用；两条 exact digest未形成 storage evidence；无 response-loss replay。 |
| P1-3 authority/target matrix | **NO / PARTIAL** | #553既有窄修复保留；#558未补矩阵。 |
| P1-4 reclaim/alert-failure matrices | **NO / PARTIAL** | 既有 terminal reclaim窄测保留；restart/concurrency/success/alert failure等仍缺。 |
| P1-5 restart diagnostics、ops.ps/doctor | **NO / PARTIAL** | 只有 direct storage projection；无 reopen与operator surface测试。 |
| P1-6不得回退 | **YES** | handle-only payload与closed safe error边界保持。 |
| migration为 `0029_channel_alert_contract` | **YES** | 编号、文件、version/count均正确且唯一。 |
| 严格核验实现方 evidence matrix未齐 | **YES** | 自述属实，且缺口覆盖全部硬性矩阵。 |
| 禁止自修自审 | **YES** | #558由Cursor实现；本轮Sol只写复审报告。 |
| P1-1..P1-5全部关闭 | **NO** | P1-2最小阻断明确存在，其余残余亦未补齐。 |
| #558可核销 | **NO** | 关闭标尺未满足。 |

## 5. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 565`、`gh issue view 565 --comments`；回溯 #553/#558全文、comments、PR #562正文/checks和完整实现diff。
- `git diff 4587f03..de9cbae --check`：**通过**。
- `go test ./internal/storage -run 'TestChannelFailureAlertRequiresClosedPayload|TestChannelDiagnosticsIncludesBatchFailureProjection|TestRecordBlockerReportKeepsRunningRun' -count=10`：**通过**。
- `go test ./internal/channelworker -run 'TestWebhook' -count=10`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：前三包通过；controlplane 的已知 `TestDoctorBaselineChecksConfiguredDependencies` fixture命令出现 `signal: killed`。
- `go test ./... -count=1`：除同一 doctor fixture失败外其余包通过；该失败与 #558 的三文件diff无关，但全量不能记为绿。
- `go test -race ./internal/storage ./internal/channelworker -count=1`：300 秒 harness期限内未完成，不能记为通过。
- 全仓证据搜索：`ae3dba99…`仍仅 adapter手写 fixture；无 `ba180536…`；无 `PrepareDueAttentionBatches`测试调用；operator/CLI测试无 Channel projection断言。

## 6. Issue #565 验收清单

- [x] 获取并阅读 #565 全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #553 FAIL / #558严格复审 P1-1..P1-6：**YES**
- [x] 严格核验实现方“evidence matrix未齐”：**YES，属实**
- [x] 核验 migration为 `0029_channel_alert_contract`：**YES**
- [x] 确认 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #558可核销：**NO**

## 7. 最终裁决

**FAIL。** `0029_channel_alert_contract` 的编号与窄 schema guard有效，但它没有交付 #558 的主要关闭条件。必须补真实两成员 admission → production sealer 的 `ae3dba99…` exact fixture与同 operation response-loss replay，并由真实第三次失败路径生成 `ba180536…` canonical alert；同时补齐带冻结 Channels 的 `RecordReport` quota vertical、完整 authority/reclaim/restart/concurrency/success/alert-failure矩阵，以及 reopen 后 `ops.ps`/`ops.doctor` 投影测试，再由不同代理复审。P1-6继续保持关闭。
