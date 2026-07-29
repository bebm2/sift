FAIL

# M5 #577 Channel webhook after #570 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#570 / PR #574，实现提交 `7eb3da2f73cb1b29c8a5c0a88c2b20872bced7a1`，合入提交 `c243c181a63e6709b57cc0bf61d7ed308e6da091`
> 评审基线：`main` / `origin/main` `c243c18`
> 判定基准：[#565 FAIL](2026-07-30-m5-channel-webhook-worker-565-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #570 / PR #574 的合入 diff 只有四个文件：为 blocker Report 补 `BatchAtMS`，扩大 membered batch UPDATE 不可变测试，新增 `0032_channel_member_authority` INSERT trigger，以及 schema version/count 更新。migration 已按 conductor 要求重编号为 `0032_channel_member_authority`，且 rebase 后 `emitReportInterruptHooks + BatchAtMS` 接缝存在；但实现方自述 P1 未全关属实。

该 diff 对 P1-1 和 P1-3 有窄增量：blocker Report 的生产命令现在同时携 frozen Channels 与下一摘要时刻；新 trigger 也拒绝向非 collecting、不同 project/Forge target/Channel snapshot 的 batch 插入 member。可是新增测试没有从 `RecordReport` 携 Channel snapshot 跑到 delivery/batch/sealer/worker；migration 本身也没有 mismatch INSERT 测试。P1-2 的 production sealer exact fixture与 response-loss replay、P1-4 的完整 failure/reclaim矩阵、P1-5 的 restart及 `ops.ps`/`ops.doctor` 验收完全没有出现在 #570 diff 中。

全仓搜索仍只有 webhook adapter手写 payload命中 `ae3dba99…`，没有测试命中 canonical alert digest `ba180536…`，也没有测试调用 `PrepareDueAttentionBatches`。现有 diagnostics测试仍只在同一个打开的 DB 上直接调用 storage口。P1-6 的 secret/error-summary边界未回退。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #565 P1 逐项复审

### P1-1 — PARTIAL / OPEN：Report接线增强，但 production vertical仍不存在

`internal/storage/report.go` 现在为 blocker Report计算下一 daily summary，并把 `Channels: reportChannels(cfg)` 与 `BatchAtMS` 一起交给 `emitReportInterruptHooks`。这是有效代码增量。

但证据仍断裂：

- `TestReportQuotaCommandRetainsFrozenChannels` 只直接构造 `reportQuotaCmd` 并检查一项内存字段；
- `TestRecordBlockerReportKeepsRunningRun` 的 config snapshot没有 `attention.channels`，只断言 Interrupt和 quota exhaustion；
- `TestRecordReportQuotaExhaustionUsesSystemEventIdentity` 直接调用 `RecordReportQuotaExhaustion`，不是 `RecordReport` production入口，且只检查单条 Channel delivery；
- 没有任何一条上述测试继续调用 `PrepareDueAttentionBatches`、检查 sealed exact payload/outbox wake、执行 webhook并完成 delivery。

因此 Report quota → frozen Channel → batch/member → production sealer → worker completion 的要求仍为 NO。

### P1-2 — OPEN：exact sealer、canonical alert及 replay仍未交付

#570 没有修改 `channel_batch.go`、Channel worker或相关 exact tests。当前证据仍是：

- `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 只由 `internal/channelworker/webhook_test.go` 的手写 adapter payload命中；
- `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 没有测试命中；
- storage测试没有调用 `PrepareDueAttentionBatches`；
- 没有从两名真实 admission/member生成 §6.6 exact bytes/digest，也没有远端成功、本地 response-loss 后以同 operation key和同 payload重放；
- 没有由真实第三次 `rate_limited` completion生成并逐字节核验 canonical `forge_alert`。

P1-2 明确未关闭。

### P1-3 — PARTIAL / OPEN：authority guard增强，完整 matrix仍缺

`0032_channel_member_authority.sql` 增加有效的 INSERT guard：batch必须仍为 `collecting`，并与 member所属 Run 的 project、五列 Forge target、Channel ID及 snapshot逐字节相同。`TestMemberedBatchCannotBeRetargeted` 也把既有 `0026` UPDATE guard从单列 `forge_host` 扩到 project/channel/snapshot/target/kind/delivery/scope/episode/due列。

但新测试只验证 UPDATE；没有直接构造错误 member证明 `0032` 对每个 mismatch列和 sealed/cancelled batch fail closed。也没有 #565 要求的并发 `i-a/i-b/i-c` 双 batch、collision reread、重启后 authority及最终 exact sealing矩阵。故该项仅为 PARTIAL。

### P1-4 — PARTIAL / OPEN：没有新增 failure/reclaim/alert-failure矩阵

#570 未修改 failure episode、claim/complete/reclaim代码或测试。既有 `TestChannelDiagnosticsIncludesBatchFailureProjection` 仍使用手写不完整 batch payload和单 DB handle，只覆盖两次 completion后 attempt 3 terminal reclaim、旧 completion stale及无 attempt 4。

仍缺 threshold reclaim后继续 lease、并发双 worker、第三次 rate-limited completion及 canonical alert、restart count=2后唯一 alert、success清零、alert自身失败不递归，以及完整 terminal delivery/episode/alert assertions。P1-4 未关闭。

### P1-5 — PARTIAL / OPEN：无 restart及 operator surface验收

生产 `ops.ps`/`ops.doctor` 已会调用 `ChannelDiagnostics`，但 #570 没有增加 controlplane/CLI测试。唯一具名 Channel diagnostics测试仍直接调用 storage口、没有关闭重开数据库，也不经过 `ops.ps` 或 `ops.doctor` RPC/CLI。

故重启后持久 count/state/error/alert key/alert state及“已生成、未送达”的 operator surface验收仍为 NO。

### P1-6 — CLOSED，未回退：secret/error-summary边界保持

#570 不修改 webhook resolver/sender、closed payload、completion或安全错误摘要。payload仍只保存 `secret_ref:` handle；现有 query-secret和 sender-error负向测试通过。新增 migration、Report字段和测试没有引入 endpoint、credential、response body或原始 sender error。P1-6继续 CLOSED。

## 3. migration与 rebase核验

- `internal/storage/migrations/0032_channel_member_authority.sql` 存在且编号唯一；embedded schema version和 migration row count均更新为 32，reopen幂等测试通过。
- 仓库中 `0030_advance_interrupt_p1_closure`、`0031_emit_interrupt_binding_t4_closure`、`0032_channel_member_authority` 顺序无冲突。
- `report.go` 的 rebase结果使用 `emitReportInterruptHooks`，并同时传入 `Channels` 和新增 `BatchAtMS`；没有把 #569 的 T4 hook接线覆盖回旧入口。
- `git diff c243c18^1..c243c18 --check` 通过。

以上只关闭 issue特别指定的编号/rebase核验，不等于 P1-1..P1-5关闭。

## 4. 关闭条件对账

| #577 条件 | 结果 | 说明 |
|---|---|---|
| 对照 #565 FAIL / #570复审 P1-1..P1-5 | **YES** | 已逐项复审；五项均未全部关闭。 |
| P1-1 Report quota Channel production vertical | **NO / PARTIAL** | `BatchAtMS`生产接线有效，但没有 RecordReport→sealer→worker纵向证据。 |
| P1-2 exact sealer/alert fixtures + replay | **NO** | 无 production sealer测试、canonical alert digest或 response-loss replay。 |
| P1-3 authority/target matrix | **NO / PARTIAL** | INSERT guard和 UPDATE列覆盖增强；并发/restart/exact矩阵及 trigger负测缺失。 |
| P1-4 reclaim/alert-failure matrices | **NO / PARTIAL** | 既有 terminal reclaim窄测保留；其余硬性 vectors缺失。 |
| P1-5 restart diagnostics、ops.ps/doctor | **NO / PARTIAL** | 只有 direct storage projection；无 reopen和 operator surface测试。 |
| P1-6不得回退 | **YES** | handle-only payload与安全错误边界保持。 |
| migration为 `0032_channel_member_authority` | **YES** | 编号、顺序、version/count、reopen幂等均正确。 |
| rebase保留 emitReportInterruptHooks + BatchAtMS | **YES** | 当前 `report.go` 同时携 hook入口、Channels和 batch时刻。 |
| 严格核验实现方“P1未全关” | **YES** | 自述属实。 |
| 禁止自修自审 | **YES** | #570由Cursor实现；本轮Sol只写复审报告。 |
| P1-1..P1-5全部关闭 | **NO** | P1-2是明确最小阻断，其他项也仍有硬缺口。 |
| #570可核销 | **NO** | 关闭标尺未满足。 |

## 5. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 577`、`gh issue view 577 --comments`；回溯 #565/#570全文、comments、PR #574正文、commit/file列表与完整实现diff。
- `git diff c243c18^1..c243c18 --check`：**通过**。
- 定向 storage测试（七个 Report/Channel/migration测试，`-count=10`）：**通过**。
- `go test ./internal/channelworker -run 'TestWebhook' -count=10`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：**通过**。
- `go test ./... -count=1`：除已知 `TestDoctorBaselineChecksConfiguredDependencies` fixture命令 `signal: killed` 外其余包通过；全量不能记为绿。
- `go test -race ./internal/storage ./internal/channelworker -count=1`：300秒 harness期限内未完成，不能记为通过。
- 全仓证据搜索：canonical alert digest无测试命中；storage测试无 `PrepareDueAttentionBatches`调用；Channel diagnostics只有 direct storage测试，无 operator surface断言。

## 6. Issue #577 验收清单

- [x] 获取并阅读 #577 全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #565 FAIL / #570严格复审 P1-1..P1-6：**YES**
- [x] 核验 migration为 `0032_channel_member_authority`：**YES**
- [x] 核验 rebase保留 `emitReportInterruptHooks + BatchAtMS`：**YES**
- [x] 严格核验实现方“P1未全关”：**YES，属实**
- [x] 确认 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #570可核销：**NO**

## 7. 最终裁决

**FAIL。** `0032_channel_member_authority` 和 Report `BatchAtMS` 是有效窄修复，但不构成 #565 的完整关闭证据。下一实现必须至少补真实 `RecordReport` quota → frozen Channel → 两成员 production sealing → webhook completion纵向测试，命中 `ae3dba99…` exact payload并覆盖同 key/payload response-loss replay；再由真实第三次 failure路径命中 `ba180536…` canonical alert，并补齐 authority并发/restart、failure reclaim/concurrency/success/alert-failure及 reopen后 `ops.ps`/`ops.doctor` 矩阵。P1-6继续保持关闭。
