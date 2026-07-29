FAIL

# M5 #480 Channel webhook after #474 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#474 / PR #478，实现提交 `803936de92666d6c6b4582d54f17e6dd11a2a1fd`，合入提交 `6612bc273d394f482c94a576720ea00ba663ac6c`
> 评审基线：`main` / `origin/main` `6612bc2`
> 判定基准：[#469 FAIL](2026-07-29-m5-channel-webhook-worker-469-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.5–§6.6](../specs/storage.md#65-batch-delivery-投影)

## 1. 结论

**FAIL。** #474 确实修复了 closed union 的一部分、普通 completion 的阈值比较、单条 delivery identity 写入、remote ref 写回和 sender error 文本越界，但 #469 的 P1-1 至 P1-5 均未闭合。生产 `siftd` 没有构造或安装 Channel worker，领域写口仍没有 producer；测试没有使用 storage §6.6 exact fixture/digest；reclaim 仍硬编码阈值并无条件创建下一 attempt；batch/Ledger 与 `ps`/`doctor` 投影仍缺失。P1-6 的安全错误摘要边界已关闭。

本轮只新增评审报告，不修改被评审代码或规格。遗留 **5 个 P1**，#474 不可核销。

## 2. #469 P1 逐项复审

### P1-1 — OPEN：生产 producer/consumer 仍未接线

`Daemon.OutboxTick` 现在会遍历 `Channels`，但 `daemon.Assemble` 不构造 Channel worker，`cmd/siftd/main.go` 也从未调用 `AddChannelWorker`。全仓 `AddChannelWorker` 只有定义；`channelworker.Worker` 没有生产实例。配置实现甚至尚无 `attention.channels[]` effective/raw model、resolver 或 webhook transport 可供生产构造。

producer 同样未闭合：`EnqueueChannelPublish` 仍只有定义，无 `EmitInterrupt`、`AdvanceInterrupt` 或 `PrepareAttentionBatch` 调用；后者本身也没有实现。因此 commit wake → claim → adapter → complete 只能停留在未调用的 seam，不能形成生产闭环；本次也没有生产接线集成测试。

证据：`internal/daemon/daemon.go:69-110,117-124,188-192`，`cmd/siftd/main.go:69-90`，全仓引用搜索。

### P1-2 — OPEN：不是 §6.6 exact fixture，closed/binding 校验仍不完整

adapter 已接受 `forge_alert_target`、拒绝顶层/成员未知字段和尾随 JSON，这是实质进展。但新增测试把规范 fixture 的完整 base64url batch identity、headline 与 rendered text 改成了简化值，未复算规范 digest `ae3dba…`，也没有 response-loss replay bytes/key 测试。

实现只要求 member delivery ID 具有 `batch_id + ":"` 前缀，没有要求逐字节等于 `<batch_id>:<interrupt_id>`；也未要求 batch delivery ID 等于 `<batch_id>:publish:1`，未校验 links/options 的 closed member schema、version 范围、batch kind/scope/target enum，且不与 sealed storage identity 对账。因此“可 decode 一个近似 fixture”不能关闭 exact fixture、identity 与 sealed binding 条件。

证据：`internal/channelworker/webhook.go:133-155`，`internal/channelworker/webhook_test.go:20-39`。

### P1-3 — OPEN：普通阈值比较已修，但规定矩阵与 durable target 未闭合

`old < threshold && count >= threshold` 修复了原先默认阈值 3 永不插入 alert 的直接 bug，markdown 也包含固定 Diagnostics 行。然而 reclaim 路径仍把 threshold 写死为 3；生产 worker不存在，故有效 config 从未传入。单条 alert 不是从 delivery/command-target 的冻结绑定读取，而是在失败时重新 join 当前 `interrupts/runs` 投影；没有证明它逐字节等于创建 delivery 时冻结的 verified target。

没有 threshold 前后、重启、并发 stale completion、alert 自身失败、单条/批次 canonical alert digest `ba1805…` 中任一存储测试。故唯一 alert、durable frozen target 与 §6.6 vectors 均未建立证据。

证据：`internal/storage/channel.go:79-131`，`internal/storage/outbox.go:257-259`；新增测试仅两条 adapter test。

### P1-4 — OPEN：completion 只修一半，terminal reclaim 仍明确违反规格

普通 retryable completion 在调用方传入 `MaxAttempts` 时会转 failed；worker 也新增了 backoff/max-attempt 字段。但 production config 未接入。更关键的是 `ClaimOutboxOperation` reclaim 没有接收或读取有效 policy：attempt 3 lease expiry 后仍先按 retryable 更新投影，再无条件设置 `oldCount+1`、写新 lease 和 attempt 4。它还硬编码 alert threshold 3。

`rate_limited` 仍不产生 `RetryAfterMS`，worker 在 backoff 未设置时继续使用硬编码 `1000/60000/2`。没有 attempt 3 expiry、旧 completion stale、无 attempt 4 或 Retry-After 测试。因此 storage §6.6 terminal reclaim vector仍失败。

证据：`internal/storage/outbox.go:249-275,298-313`，`internal/channelworker/webhook.go:186-224`。

### P1-5 — OPEN：durable schema/投影与查询面只部分补齐

migration 为单条 delivery 增加了 identity 列，但全是 nullable；没有规格要求的 FK/CHECK、Channel 行必填约束、sealed/append-only trigger。`EnqueueChannelPublish` 仍是孤立 helper，不是 `EmitInterrupt` / batch sealing 的同领域事务边界。

completion 现在检查恰好一条 delivery 并写 `remote_ref`，但 batch success 只更新 `batch_deliveries`：没有推进 `attention_batches`，没有逐 member Ledger `attention_delivery`。全仓 `ps`/`doctor` 没有查询 `channel_failure_episodes`、`batch_deliveries` 或 Channel `interrupt_deliveries`，所以 attempt/count/next retry/error/alert 及“已生成、未送达”仍不可见。没有 durable projection、重启或查询测试。

证据：`internal/storage/migrations/0012_channel_delivery_projections.sql`，`internal/storage/channel.go:55-77,145-186`，`internal/controlplane/doctor.go` 与 `cmd/sift` 查询路径搜索。

### P1-6 — CLOSED：secret/error summary 边界已修

adapter 不再向 worker传播 resolver/sender 原始错误文本，而只返回 closed 分类；worker 持久化固定安全摘要。新增负向测试以含 query token 的 sender error 证明 adapter error 不含 secret。endpoint、resolver error 与原始 sender body/stderr 不再进入 `last_error_summary`、delivery `last_error` 或 episode diagnostics。

证据：`internal/channelworker/webhook.go:157-181,206-224`，`internal/channelworker/webhook_test.go:41-53`。

## 3. 关闭条件对账

| #480 / #474 条件 | 结果 | 说明 |
|---|---|---|
| 生产接线 | **NO** | Daemon 有消费循环 seam，但生产无 worker 实例；producer 无调用点。 |
| storage §6.6 exact fixture/replay/digest | **NO** | 测试使用改写后的近似 payload，无 `ae3dba…` / `ba1805…` 或 replay。 |
| 阈值唯一 alert + Diagnostics | **NO** | 普通 completion 比较已修；reclaim 硬编码阈值、矩阵/target/digest 无测试。 |
| max-attempt / terminal reclaim | **NO** | completion 部分实现；expiry reclaim 仍创建 attempt 4。 |
| durable projections + `ps`/`doctor` | **NO** | identity/remote ref 部分实现；batch/Ledger、约束和查询面缺失。 |
| secret 不泄漏到 error summaries | **YES** | 原始 resolver/sender error 被 closed 分类和固定摘要截断。 |
| 不扩 Command/Report | **YES** | diff 未改 Command/Report。 |
| 禁止自修自审 | **YES** | #474 实现方为 GPT-5.6 Terra；本轮 Sol 仅写复审报告。 |

## 4. 执行证据

- 从检测到的 GitHub forge 读取 #480 与 comments（无评论）、#469/#474 全文与 comments，以及 PR #478 元数据、提交、文件和 checks。
- `git diff 85e2530..6612bc2 --check`：**通过**。
- `go vet ./internal/channelworker ./internal/storage ./internal/daemon`：**通过**。
- `go test ./internal/channelworker ./internal/storage ./internal/daemon`：**通过**。
- `go test ./...`：**失败**；`internal/controlplane` doctor 时序和 `internal/wrapper` 两条 process integration 超时。它们不由本报告引入，也不能替代缺失的 Channel storage/production tests。
- PR #478 的 forge checks 均为 SUCCESS；无 review/comment。绿 CI 不覆盖上述 exact/reclaim/production/query条件。

## 5. Issue #480 验收清单

- [x] 获取并阅读 #480 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #469 FAIL / #474 逐项复审 P1-1..P1-6：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-6 全部关闭：**NO（P1-1..P1-5 OPEN；P1-6 CLOSED）**
- [ ] #474 可核销：**NO**

## 6. 最终裁决

**FAIL。** #474 不能关闭 #469。需由实现方补齐生产 producer/consumer、使用 §6.6 原样 bytes/digests 的测试、配置化 terminal reclaim、完整 batch/Ledger 与 `ps`/`doctor` durable projection，再由不同代理复审。
