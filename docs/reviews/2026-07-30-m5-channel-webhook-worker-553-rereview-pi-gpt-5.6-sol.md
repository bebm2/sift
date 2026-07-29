FAIL

# M5 #553 Channel webhook after #546 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#546 / PR #550，实现提交 `1917acc8756193ad11adfce3006d532335e61e2c`，合入提交 `4587f03`
> 评审基线：`main` / `origin/main` `4587f03`
> 判定基准：[#541 FAIL](2026-07-30-m5-channel-webhook-worker-541-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.2–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #546 有实质修复：Report 的冻结 config snapshot 现可解出 Channel 并传入普通 blocker 与 quota-exhaustion 两条 production owner 路径；`0026_channel_closure` 在 collecting batch 已有 member 后冻结 authority，并拒绝不存在的 project；碰撞重读扩大到 member 的全部冻结列；新增测试命中 direct `ChannelDiagnostics`、terminal lease reclaim、stale completion、无 attempt 4、基础 Channel config 正反例。migration 编号与 conductor 手工合并后的 26 一致，P1-6 未回退。

但 #553 已明确提示实现方自述 P1-2 exact sealer fixture 仍为 NO，严格核验结果属实。全仓唯一 `ae3dba99…` 断言仍在 webhook adapter 的手写 payload 测试；`ba180536…` 完全没有断言。新增 storage 测试直接调用 `EnqueueChannelPublish` 注入另一份手写单成员 payload，未创建两名真实 member、未调用 `PrepareDueAttentionBatches`，也未核验 production sealer 的 exact bytes/digest、canonical alert bytes/digest或 response-loss replay。因此 P1-2 明确未关闭，已足以阻断核销。

其余证据也没有把 #541 要求全部闭合：Report 测试只测 `reportQuotaCmd` helper 而非 `RecordReport` quota 纵向路径；failure 测试未 reopen DB、未覆盖 threshold reclaim 后继续 lease、并发双 worker、第三次 completion、成功清零或 alert 自身失败；`ops.ps`/`ops.doctor` 仍无 Channel projection 断言。故 P1-1、P1-4、P1-5 至多部分关闭，不能把 P1-1..P1-5 整体判为完成。

本轮只新增评审报告，不修改被评审实现或规格。

## 2. #541 P1 逐项复审

### P1-1 — PARTIAL / OPEN：production 参数已接线，纵向证据仍缺

`reportRuntimeConfig.Attention.Channels`、`reportChannels` 及两处 production `EmitInterrupt`/`RecordReportQuotaExhaustion` 调用已接通，解决了 #541 指出的空 Channel 参数缺陷。

但新增 `TestReportQuotaCommandRetainsFrozenChannels` 只直接构造私有 runtime struct 并调用 `reportQuotaCmd`；它不调用 `RecordReport`，不从持久化 config snapshot 解码，也不证明 quota exhaustion 建立 batch/member、sealer operation及 wake/worker/completion。#541 要求的 Report quota → Channel production 纵向证据仍不存在。

证据：`internal/storage/report.go:180-192,246-258,355-406`；`internal/storage/channel_closure_test.go:9-16`。

### P1-2 — OPEN：production sealer/alert exact fixture仍为 NO

`PrepareDueAttentionBatches` 没有新增测试调用。新增 failure 测试在 `internal/storage/channel_closure_test.go:50-52` 手写 payload 后直接调用 `EnqueueChannelPublish`，绕过 production member admission和 sealer。它既不是 §6.6 的两成员 fixture，也没有断言 sealed operation payload/digest。

全仓测试搜索结果保持：

- `ae3dba99e23daaf742abfeb13526da4afe0cd4ecb3b082471274e0cacfc5ac6e` 只出现在 `internal/channelworker/webhook_test.go` 的手写 adapter payload；
- `ba180536811392f1bdf607d2afc27c42dde08d6b5d3a597e0838e705effd32f2` 无任何测试命中；
- 无测试调用 `PrepareDueAttentionBatches`。

因此没有证据证明两名真实成员经 `internal/storage/channel_batch.go:38-120` 生成 §6.6 exact bytes，也没有 canonical alert exact digest或 response-loss后同 operation/key/payload replay。该项与实现方自述一致，明确未关闭。

### P1-3 — PARTIAL：已关闭已知 retarget/collision缺陷，完整矩阵证据不足

`0026_channel_closure` 编号正确且唯一。trigger 在 collecting batch已有 member时冻结 project/channel/snapshot/完整 Forge target/kind/delivery/scope/episode/due authority，并在 INSERT 时要求 project存在。production `INSERT OR IGNORE` 后也已逐字节比较 admission、member key、delivery、version、nonce、展示字段与 joined time。新增测试证明单一 `forge_host` retarget被拒绝。

这些修改关闭了 #541 指出的两个具体代码缺陷。但测试没有逐列覆盖 authority trigger、project拒绝、member碰撞、不同 host/project/target并发分批或 reopen 后约束保持；故完整 target matrix 的验收证据仍不充分。

证据：`internal/storage/migrations/0026_channel_closure.sql:1-22`；`internal/storage/advance_interrupt.go:450-480`；`internal/storage/channel_closure_test.go:18-42`。

### P1-4 — PARTIAL / OPEN：terminal reclaim窄路径已测，§6.6矩阵未闭合

新增测试确实覆盖两次 retry completion后 attempt 3 lease expiry、reclaim terminal、旧 completion stale、无 attempt 4，并断言三条 immutable attempt result及单 episode/alert operation数量。这是有效增量。

但测试始终使用同一个打开的 DB；没有重启/reopen恢复。它不覆盖 threshold由 reclaim跨越但尚未 terminal并创建下一 lease、双 worker并发 reclaim/completion、第三次 transient/rate-limited completion、success清零、alert payload exact bytes/digest、alert operation失败不递归。它也只以总 operation数间接证明 alert存在，未断言 key、target、episode `ended_failed`、delivery failed及 immutable旧 result内容。故 #541 所列 reclaim/restart/stale-worker/terminal exact vectors仍未闭合。

证据：`internal/storage/channel_closure_test.go:44-98`；`internal/storage/outbox.go:235-299`；`internal/storage/channel.go:153-281`。

### P1-5 — PARTIAL / OPEN：direct diagnostics/config窄测已补，ps/doctor与重启仍缺

新增测试直接调用 `ChannelDiagnostics` 并断言第一次失败的 count及 generated-not-delivered；config测试覆盖一个有效 webhook、空 secret ref及未知顶层字段。这些关闭了 #541 的“完全无定向调用”缺口。

但没有测试经 `ops.ps` 或 `ops.doctor` 断言 Channel ID、operation key、next retry、episode/alert state及 generated-not-delivered，controlplane测试也未构造 Channel delivery。diagnostics测试没有 reopen DB，未证明重启可见；config测试没有覆盖 duplicate ID/default、closed Channel对象、type/renderer/capability或 isolation/default选择矩阵。因此该项仍未达到 #541 的 diagnostics/config验收范围。

证据：`internal/storage/channel_closure_test.go:44-65`；`internal/config/config_test.go:44-62`；`internal/controlplane/server_test.go:33-75`。

### P1-6 — CLOSED，未回退：secret/error-summary边界保持

payload仍使用 `secret_ref:` handle，adapter仍在 executing attempt解析 endpoint；#546 没有把 resolver结果、endpoint、credential、response body或原始错误写入 payload、digest、诊断或 alert。既有 query-secret与sender-error负向测试保持通过。

## 3. 关闭条件对账

| #553 条件 | 结果 | 说明 |
|---|---|---|
| 对照 #541 FAIL / #546 复审 P1-1..P1-5 | **YES** | 已逐项复审；P1-2明确 OPEN，其余有部分增量但证据未全闭合。 |
| P1-1 Report production Channel接线 | **PARTIAL** | production参数已接；无 `RecordReport` quota纵向 fixture。 |
| P1-2 exact sealer/alert/replay fixture | **NO** | 仍绕过 production sealer；无 `ba180536…`。 |
| P1-3 authority/target matrix | **PARTIAL** | retarget/collision代码修复有效；完整迁移/并发/restart矩阵未测。 |
| P1-4 reclaim/restart/stale-worker/terminal vectors | **PARTIAL** | terminal reclaim窄路径通过；restart/并发/alert failure等缺失。 |
| P1-5 diagnostics/config | **PARTIAL** | direct storage/config窄测存在；ps/doctor/restart与完整config矩阵缺失。 |
| P1-6不得回退 | **YES** | secret/error-summary边界保持。 |
| migration为 `0026_channel_closure` | **YES** | 文件名、schema version/count均为26。 |
| 禁止自修自审 | **YES** | #546由Cursor实现；本轮Sol只写复审报告。 |
| P1-1..P1-5全部关闭 | **NO** | 至少P1-2明确未关闭。 |
| #546可核销 | **NO** | 验收未完成。 |

## 4. 执行证据

- 使用检测到的 GitHub forge获取并阅读 `gh issue view 553`、`gh issue view 553 --comments`；回溯 #541/#546全文、comments和实现diff。
- `git diff 4587f03^..4587f03 --check`：**通过**。
- `go test ./internal/storage ./internal/channelworker ./internal/config ./internal/controlplane -count=1`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./... -count=1`：除 `internal/controlplane/TestDoctorBaselineChecksConfiguredDependencies` 的 fixture命令被 `signal: killed` 外均通过；该资源/时序失败不指向本轮 Channel diff。
- `go test -race ./internal/storage ./internal/channelworker -count=1`：在 180 秒 harness期限内未完成；不能记为通过。
- 全仓测试搜索：`ae3dba99…` 仅 adapter手写 fixture；无 `ba180536…`；无 `PrepareDueAttentionBatches` 测试调用；controlplane测试无 Channel projection断言。

## 5. Issue #553 验收清单

- [x] 获取并阅读 #553 全文、Agent建议、关闭条件、约束与comments：**YES**
- [x] 对照 #541 FAIL / #546严格复审 P1-1..P1-6：**YES**
- [x] 核验实现方“P1-2 exact sealer fixture仍NO”：**YES，属实**
- [x] 核验 migration为 `0026_channel_closure`：**YES**
- [x] 确认 P1-6不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5全部关闭：**NO**
- [ ] #546可核销：**NO**

## 6. 最终裁决

**FAIL。** 最小阻断项是补一条从真实两成员 admission到 `PrepareDueAttentionBatches` 的 production sealer fixture，逐字节断言 §6.6 `ae3dba99…` payload，并在同一 durable operation上证明response-loss replay；随后以第三次 rate-limit/terminal路径逐字节断言 canonical alert `ba180536…`。同时应把 Report quota纵向路径、reopen后 diagnostics、`ops.ps`/`ops.doctor`及剩余 reclaim/alert failure矩阵补齐，再由不同代理复审。P1-6继续保持关闭。
