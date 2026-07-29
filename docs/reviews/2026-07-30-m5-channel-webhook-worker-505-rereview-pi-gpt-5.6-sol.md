FAIL

# M5 #505 Channel webhook after #498 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#498 / PR #502，实现提交 `d7b69f63874292b7e1d46e722a8bac8a8a3c5906`，合入提交 `b871b562981a7c7d6e2578d3709f207685a094a6`
> 评审基线：`main` / `origin/main` `b871b56`
> 判定基准：[#493 FAIL](2026-07-30-m5-channel-webhook-worker-493-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.3/§6.5–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #498 合入了生产 Gate 的 Channel registry、初发 immediate producer、batch authority/sealer、冻结单条告警目标、terminal reclaim result 修正和 `doctor` 查询接缝，但当前 `main` 含两个 version `0014` migration；`storage.Open` 必然以 `duplicate embedded migration version 0014` 拒绝启动，PR 的 `vet + test` 也因此失败。这是生产阻断，不是测试 flake。

即使先排除该阻断，P1-1..P1-5 仍未闭合：生产 normal/batch 没有计算 `BatchAtMS`，其他生产 EmitInterrupt caller 不传 Channels；新 sealer 生成的 payload 缺必填 `delivery_id`，既不等于 §6.6 exact bytes，也会被 adapter 自身拒绝；failure/reclaim exact matrix 没有 storage test；新增 schema、Ledger 和 `ps`/`doctor` 投影仍缺约束、错误传播与 next-retry 字段。P1-6 的 secret/error-summary 边界未回退。

本轮只新增评审报告，不修改被评审代码或规格。#493 的 **P1-1..P1-5 仍 OPEN，P1-6 保持 CLOSED**；#498 不可核销。

## 2. #493 P1 逐项复审

### P1-1 — OPEN：数据库无法启动，production producer/batch 纵向链也仍断裂

`internal/storage/migrations/0014_channel_authority.sql` 与相邻合入的 `0014_emit_interrupt_bindings.sql` 使用同一 version。migration loader在执行任何 SQL 前拒绝重复 version，因此所有依赖 `storage.Open` 的 daemon、storage 和 control-plane 路径均不可运行。

排除该全局阻断后，#498 的 producer 仍不完整：daemon 只向 Gate reconciler 注入 `Channels`，但 Gate 没有填 `BatchAtMS`，normal severity 的默认 `batch` 会在 `admitInterruptT6` 收敛为 `held/batch_after_expiry`，不会进入 daily batch。`RecordTerminationObservation` 等其他 production `EmitInterruptCmd` caller仍不传 Channels，因而只会形成 held Interrupt。新增 `PrepareDueAttentionBatches` 虽由 supervisor tick 调用，但它生成的 payload 没有顶层 `delivery_id`；`WebhookAdapter.Publish` 要求该字段且 `validChannel` 也要求非空，所以真实 sealed batch 无法投递。

没有 commit wake → production producer/sealer → claim → HTTP → completion 的纵向测试；本轮除 adapter fixture 外未增加任何测试。

证据：`internal/storage/migrate.go:43-84`，两个 `0014_*.sql`，`internal/daemon/daemon.go:114,134-140`，`internal/gate/interrupt.go:11-12`，`internal/storage/interrupt.go:320-361,661-668`，`internal/storage/termination.go:51-59`，`internal/storage/channel_batch.go:90-100`。

### P1-2 — OPEN：adapter exact fixture 有增量，但 production sealer 不是 §6.6 exact bytes

adapter test现在逐字节采用 §6.6 batch fixture并核验 `ae3dba…`，也以同一 bytes/key 调用两次，这是有效增量。但它只证明手写 fixture可被 adapter接受，不证明权威 storage sealer产生该 fixture。

实际 `prepareAttentionBatch` 的 payload map漏掉必填 `delivery_id`，故 digest不可能是 `ae3dba…`，且 adapter会在 side effect 前返回 contract violation。没有 storage sealing/replay test，也没有 canonical alert `ba1805…` test。adapter 的 `links`/`options` 仍只是 `[]json.RawMessage`，不校验嵌套 closed schema；也未约束 daily 必须 `scope=day`、critical 必须匹配 global/run grammar，且不与 sealed batch/member authority逐字节对账。

证据：`internal/channelworker/webhook_test.go:22-51`，`internal/channelworker/webhook.go:37-66,180-205`，`internal/storage/channel_batch.go:90-99`；全仓测试搜索只有 adapter exact fixture。

### P1-3 — OPEN：冻结 target 有增量，但 durable threshold/alert matrix仍无证据

单条 delivery现在保存 Forge target，告警不再 join 当前 Run；batch sealer也从 batch行携带冻结 target。这关闭了前次最直接的 target 漂移路径。

但新增 target列均 nullable且无 Channel 行必填/immutability约束；completion 对 batch 仍相信 operation payload中的 target，没有验证它与 sealed `attention_batches` authority逐字节一致。threshold 前后、重启、stale completion、alert自身失败、单条/批次 target和 canonical `ba1805…` digest均无测试；数据库重复 migration 使这些路径实际上不能执行。故唯一 alert、durable frozen target和 exact matrix不能核销。

证据：`internal/storage/migrations/0014_channel_authority.sql:2-6`，`internal/storage/channel.go:166-215`，全仓 storage tests搜索。

### P1-4 — OPEN：terminal reclaim代码方向修正，但 exact matrix未建立且主干不可执行

#498 将 expired attempt 3 的 immutable result保留为 `retry/transient:lease_expired`，只对 `channel_publish` 应用 Channel max-attempt policy，并在达到上限时不创建 attempt 4；这些改动与 §6.6 terminal reclaim方向一致。

然而没有 attempt 3 expiry、旧 completion stale、无 attempt 4、阈值同 CAS、重启或 Retry-After测试，且 `storage.Open` 全面失败。HTTP sender仍不解析 `Retry-After`，worker的 rate-limited completion也不设置 `RetryAfterMS`。因此规定的 terminal/reclaim matrix没有可执行证据，P1-4 不能关闭。

证据：`internal/storage/outbox.go:249-291,317-323`，`internal/channelworker/webhook.go:121-143,246-278`。

### P1-5 — OPEN：新增 authority/query仍不满足 schema、Ledger和可见性契约

重复 migration首先使新增 authority schema完全不可用。其内容本身也只是对旧精简表追加 nullable列：`attention_batches` 仍允许规格外 `failed` state，缺 project/FK、target命名与非空约束、quota/day/episode字段及 sealed/append-only约束；`interrupt_deliveries` 的冻结 target同样可为空。

batch success改为从 `attention_batch_members` 写逐成员 Ledger是正确方向，但 query/scan错误仍被吞掉；因此 delivery可提交成功而缺成员 Ledger evidence。`ChannelDiagnostics` 加了 batch/episode/alert join，却仍不返回 Channel ID和 outbox `next_attempt_at_ms`；对应 `ps`/`doctor` 没有测试。配置接缝也偏离 active spec：raw/effective Channel缺 `enabled`、`renderer`，ID/capability/secret_ref未按 closed grammar验证或排序去重，生成 schema未提交。

证据：`internal/storage/migrations/0013_advance_interrupt_closure.sql:25-59`，`0014_channel_authority.sql`，`internal/storage/channel.go:65-104,143-164`，`internal/controlplane/server.go:247-282`，`internal/config/raw.go:160-171`，`internal/config/normalize.go:581-603`。

### P1-6 — CLOSED，未回退：secret/error summary边界保持

adapter仍只跨 worker边界返回 closed error class；resolver/sender原始文本、endpoint/query credential和HTTP body不进入持久化 summary。已有恶意 query-token负向测试保留，新增 exact fixture replay也只持久化/重放 `secret_ref:` handle。本轮未发现 P1-6 回退。

## 3. 关闭条件对账

| #505 / #498 条件 | 结果 | 说明 |
|---|---|---|
| P1-1 production producer/batch sealing | **NO** | 重复 migration阻断启动；normal batch/其他 caller未接齐；sealed payload缺 delivery ID。 |
| P1-2 §6.6 exact fixtures | **NO** | adapter手写 exact fixture通过，但 production sealer bytes错误；无 storage/alert exact test。 |
| P1-3 threshold alert / durable target | **NO** | 冻结单条 target有增量；约束、authority binding和完整矩阵无证据。 |
| P1-4 terminal reclaim | **NO** | result/attempt-4代码方向已修；主干不可执行且 exact/stale/Retry-After矩阵无测试。 |
| P1-5 durable projections + `ps`/`doctor` | **NO** | schema不可迁移且约束不足；Ledger吞错；缺 next retry/Channel ID及查询测试。 |
| P1-6 secret边界不得回退 | **YES** | closed error summaries与 handle-only payload保持。 |
| 禁止自修自审 | **YES** | #498 实现为 Cursor；本轮 Sol只写复审报告。 |

## 4. 执行证据

- 从检测到的 GitHub forge获取并阅读 #505 与 comments（无评论）、#493/#498 与 comments、PR #502元数据/checks和完整合入 diff。
- `git diff 6798ebb..d7b69f6 --check`：**通过**（PR #502 实现提交相对其父提交）。
- `go vet ./...`：**通过**。
- `go test ./internal/channelworker`：**通过**（只覆盖手写 adapter fixture与安全边界）。
- `go test ./internal/storage`：**失败**，所有 DB-backed tests在 `storage.Open` 报 `duplicate embedded migration version 0014`。
- `go test ./...`：**失败**，同一 duplicate migration使 daemon/storage/control-plane等大量 package失败。
- PR #502：四个平台 build为 SUCCESS；`vet + test` 为 **FAIL**（duplicate migration），`schema drift check` 为 **FAIL**（`RawAttention`变更后未提交生成的 `raw_config.schema.json`）。两项均在本地/forge可复现或有完整日志，不是时序 flake。
- 全仓测试搜索确认：#498 未新增 storage、daemon、control-plane测试；没有 `ba1805…`、terminal reclaim、batch authority/Ledger或 `ps`/`doctor` exact vectors。

## 5. Issue #505 验收清单

- [x] 获取并阅读 #505 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #493 FAIL / #498 严格复审 P1-1..P1-6：**YES**
- [x] 确认 P1-6 不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5 全部关闭：**NO**
- [ ] #498 可核销：**NO**

## 6. 最终裁决

**FAIL。** #498 不能关闭 #493。首先必须消除重复 migration version并恢复全量绿测；随后补齐 production normal/其他 EmitInterrupt producer、使 storage sealer逐字节生成 §6.6 payload、增加 failure/reclaim/alert exact matrix和 production纵向测试，并完善 batch/target约束、Ledger错误传播及 `ps`/`doctor`字段，再由不同代理复审。P1-6 继续保持关闭。
