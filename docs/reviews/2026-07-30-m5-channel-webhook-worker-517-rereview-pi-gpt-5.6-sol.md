FAIL

# M5 #517 Channel webhook after #510 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#510 / PR #514，实现提交 `a217a112d2d71ab453dc3f679667a64f32d7f2c1`，合入提交 `a0fa644e2ba78871827bdcc191ea0dc47e2f4880`
> 评审基线：`main` / `origin/main` `a0fa644`
> 判定基准：[#505 FAIL](2026-07-30-m5-channel-webhook-worker-505-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.3/§6.5–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #510 消除了重复 migration version，补上 Gate/termination 的部分 Channel producer、batch 顶层 `delivery_id`、numeric `Retry-After`、Ledger 查询错误传播及若干配置/诊断字段；主干与 CI 已恢复绿测。这些是有效增量，但不足以关闭 #505 的 P1-1..P1-5。

production producer 仍漏 Report quota 路径，且 Gate 在摘要时刻使用 `nowMS-1`，与“恰在摘要 instant 也取下一 occurrence”的 active config 契约相反。实现方自述 §6.6 failure/reclaim vectors 未新增属实：#510 没有新增任何测试，storage sealer、canonical alert、threshold/reclaim/重启/stale/terminal、durable target、`ps`/`doctor` 均无 exact evidence。schema 仍主要依赖启动期 runtime DDL，保留规格外 batch `failed` state、nullable authority 列和不足的 sealed 约束；诊断的 `next_attempt_at_ms` 还错误地来自 alert operation，而不是 Channel operation。P1-6 的 secret/error-summary 边界未回退。

本轮只新增评审报告，不修改被评审代码或规格。#505 的 **P1-1..P1-5 仍 OPEN，P1-6 保持 CLOSED**；#510 不可核销。

## 2. #505 P1 逐项复审

### P1-1 — OPEN：启动阻断已关闭，但 production producer 仍漏接且 daily 边界错误

migration 已改为唯一的 `0014_channel_authority.sql`、`0015_emit_interrupt_bindings.sql`，`storage.Open` 和全量测试不再因重复 version 失败。Gate 现在传 `BatchAtMS`，termination/recovery 传 Channels，sealer payload 也有 `delivery_id`。

但 `RecordReportQuotaExhaustion` 是 production `EmitInterrupt` owner，其 command 没有 `Channels`、`DailySummaryAt` 或 `BatchAtMS`，故该 failure-review Interrupt 仍只能形成 held Channel 投影。Gate 又以 `NextDailySummaryAt(nowMS-1, ...)` 计算 batch instant；config §3.9 明定恰在摘要 instant 及后一毫秒都必须取下一 occurrence，该减一会在恰好 instant 时选择当前 occurrence并可立即 seal。没有 commit wake → 任一 production producer → authority sealer → claim → HTTP → completion 的纵向测试；#510 的 diff 没有测试文件。

证据：`internal/storage/report_quota.go:15-23,74-82`，`internal/gate/interrupt.go:11-15`，`cmd/siftd/main.go:55-69,114-123`，`internal/storage/channel_batch.go:88-108`。

### P1-2 — OPEN：payload 字段已补，但 production exact bytes/binding 仍无证据

`prepareAttentionBatch` 现写顶层 `delivery_id`，手写 adapter fixture继续验证 `ae3dba…`；这是正确修正。但 #510 没有从 storage authority 构造 §6.6 两成员 fixture、断言 `payload_json`/digest、再 response-loss 重放相同 bytes/key 的测试，也没有 canonical alert `ba1805…` 测试。

adapter 仍只把 member `links`/`options` decode 为 `[]json.RawMessage`，不校验嵌套 closed schema；daily/critical 的 kind/scope/identity grammar也没有完整交叉校验。更关键的是 adapter只校验 payload内部 identity，不与 `attention_batches`/members authority逐字节对账。因此手写 fixture可投递，不能证明 production sealer及 sealed binding满足唯一 exact vectors。

证据：`internal/storage/channel_batch.go:50-68,88-103`，`internal/channelworker/webhook.go:71-83,202-215`，`internal/channelworker/webhook_test.go:22-48`；`git diff 11bc9c5..a217a11` 无测试文件。

### P1-3 — OPEN：冻结 target 有增量，durable threshold/target matrix仍不存在

新增 trigger要求新 Channel delivery带冻结 target，Gate/termination production写口会在 delivery事务中保存 target；batch identity也增加完整 target unique index。这改善了直接 target 漂移。

但 migration 中五个 delivery target列及 batch `delivery_id/scope/scope_id/due/payload/timestamps` 仍 nullable；`attention_batches` 仍允许规格外 `failed` state、缺 project FK、quota/day/episode authority和完整状态约束。runtime sealed trigger只禁止四列变化，未冻结 project/channel/完整 Forge target/kind/scope/due/state-member authority。`INSERT OR IGNORE` 后也不逐字节验证碰撞行与 candidate snapshot一致。threshold 前后、不同 host 并发、重启、stale completion、alert失败和 canonical target/digest均没有测试。

证据：`internal/storage/migrations/0014_channel_authority.sql:2-20`，`internal/storage/channel.go:18-50`，`internal/storage/advance_interrupt.go:290-304`。

### P1-4 — OPEN：terminal reclaim方向正确，但 exact matrix确实未新增

terminal reclaim目前保留 attempt result 为 `retry/transient:lease_expired`，同 CAS终结 operation/delivery/episode且不创建 attempt 4；普通 completion也在写 result前按 Channel max attempts转 terminal。HTTP sender新增 numeric-seconds `Retry-After` 并传入 completion。这些方向与规格一致。

但 #510 没有新增实现方明确承认缺失的 §6.6 vectors：attempt 3 expiry、immutable旧 result、旧 completion stale、无 attempt 4、阈值同 CAS、重启恢复、双 worker和 alert失败均无定向测试；HTTP-date `Retry-After` 也未解析。仅凭未执行 exact matrix 的代码路径不能关闭 P1-4。

证据：`internal/storage/outbox.go:249-296,320-343`，`internal/channelworker/webhook.go:165-175,282-289`；全仓测试只有 adapter手写 batch fixture，没有 Channel reclaim/failure episode storage test。

### P1-5 — OPEN：Ledger错误传播已修，但 authority、config和查询仍不合约

batch member查询/scan错误现在会回滚 completion，修正了前次吞错；config补上 `enabled`/`renderer`、capability排序去重和生成 schema；`ps`/`doctor`也新增 Channel ID与名为 next retry 的字段。

但诊断查询只用一个 `outbox_operations o`，且 join 条件是 `o.operation_key=e.alert_operation_key`；所以 `next_attempt_at_ms` 来自 alert operation，alert尚未创建时固定为 0，而不是当前 Channel operation的 retry deadline。它需要分别 join delivery operation与 alert operation。该路径没有 `ps`/`doctor`测试。

authority仍不是 storage §6.3/§6.5 schema；成功 Ledger用 `INSERT OR IGNORE`，不验证既有同 ID evidence逐字节一致。config仍未校验 Channel ID正则、capability closed enum或 `secret_ref` grammar，生成 schema中的 `target`只是开放 `object`。因此 durable projections/config闭包不能核销。

证据：`internal/storage/channel.go:99-128,186-208`，`internal/controlplane/server.go:247-280`，`internal/config/normalize.go:589-611`，`internal/contract/schema/raw_config.schema.json:72-103`。

### P1-6 — CLOSED，未回退：secret/error summary边界保持

payload仍只保存 `secret_ref:` handle；resolver结果、endpoint、query credential和HTTP body不进入 storage。worker只持久化 closed error class/summary，已有恶意 query-token负向测试保留。新增 `RateLimitedError` 只携毫秒数，不携响应文本。本轮未发现 P1-6 回退。

## 3. 关闭条件对账

| #517 / #510 条件 | 结果 | 说明 |
|---|---|---|
| P1-1 production producer/batch sealing | **NO** | migration与部分 producer已修；Report quota漏 Channels/batch fields，exact daily instant错误，缺纵向测试。 |
| P1-2 §6.6 exact fixtures | **NO** | sealer补 delivery ID；仍无 storage exact bytes/digest/replay、canonical alert或authority binding测试。 |
| P1-3 threshold alert / durable target | **NO** | target trigger/index有增量；schema/immutability/collision与完整 exact matrix未闭合。 |
| P1-4 terminal reclaim | **NO** | 代码方向正确且 numeric Retry-After已接；实现方自述的 failure/reclaim vectors确实一个未增。 |
| P1-5 durable projections + `ps`/`doctor` | **NO** | Ledger错误传播已修；next retry join错误，authority/config闭包及查询测试仍缺。 |
| P1-6 secret边界不得回退 | **YES** | handle-only payload与 closed error summaries保持。 |
| migration 从 0017+ / 不再重复 version | **YES** | #510未新增 migration；主干现有 version唯一，后续相邻 migration为 0016/0017。 |
| 禁止自修自审 | **YES** | #510 实现为 Cursor；本轮 Sol只写复审报告。 |

## 4. 执行证据

- 从检测到的 GitHub forge获取并阅读 #517 与 comments（无评论）、#505/#510 与 comments、PR #514元数据/checks和完整实现 diff。
- `git diff 11bc9c5..a217a11 --check`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./internal/channelworker ./internal/storage ./internal/daemon ./internal/gate`：**通过**。
- `go test ./internal/controlplane ./internal/launchworker`：**通过**。
- `go test -race ./internal/storage ./internal/daemon ./internal/controlplane ./internal/gate`：**通过**。
- `go test ./...` 并行首轮：**失败**于已知时序/资源型 doctor 与 launchworker用例；上述失败包单独复跑均通过，未记为 Channel 阻断。
- PR #514：四平台 build、schema drift、`vet + test` 均为 **SUCCESS**。
- 全仓测试搜索与 commit diff确认：#510 未新增测试；无 `ba1805…`、Channel terminal reclaim、failure episode、storage batch authority/Ledger或 `ps`/`doctor` exact vectors。

## 5. Issue #517 验收清单

- [x] 获取并阅读 #517 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #505 FAIL / #510 严格复审 P1-1..P1-6：**YES**
- [x] 严格核验实现方“§6.6 failure/reclaim vectors 未新增”：**YES，确实未新增**
- [x] 确认 P1-6 不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5 全部关闭：**NO**
- [ ] #510 可核销：**NO**

## 6. 最终裁决

**FAIL。** #510 不能关闭 #505。需补齐 Report quota producer与 daily instant边界，建立 storage sealer/alert exact bytes及完整 threshold/reclaim/restart/stale matrix，迁移并约束权威 schema，修正 Channel operation next-retry join，补 config closed grammar和 production纵向/`ps`/`doctor`测试后，再由不同代理复审。P1-6 继续保持关闭。
