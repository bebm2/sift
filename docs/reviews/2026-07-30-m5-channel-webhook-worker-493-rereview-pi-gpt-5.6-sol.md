FAIL

# M5 #493 Channel webhook after #486 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#486 / PR #490，实现提交 `8ba852093542c9ac70e8e9d4e625d63ff5b78c82`，合入提交 `dc69cd5a8676bc14cc7232982ade4503d817571b`
> 评审基线：`main` / `origin/main` `dc69cd5`
> 判定基准：[#480 FAIL](2026-07-30-m5-channel-webhook-worker-480-rereview-pi-gpt-5.6-sol.md)、[`channel.md`](../specs/channel.md)、[`outbox.md` §10](../specs/outbox.md#10-channel-publish)、[`storage.md` §6.3/§6.5–§6.6](../specs/storage.md#66-channel-batch-and-failure-episode-exact-vectors)

## 1. 结论

**FAIL。** #486 把 production Channel consumer、HTTP adapter、配置化 reclaim 和若干 batch/查询投影接缝向前推进，但实现方自述 Partial 的 P1-2/P1-3/P1-5 确实仍未关闭；此外 P1-1 的 production producer、P1-4 的 terminal reclaim result 语义与 exact matrix 也未闭合。此前关闭的 P1-6 未回退。

本轮只新增评审报告，不修改被评审代码或规格。#480 的 **P1-1..P1-5 仍 OPEN，P1-6 保持 CLOSED**；#486 不可核销。

## 2. #480 P1 逐项复审

### P1-1 — OPEN：consumer 已接线，但 production producer 仍未形成闭环

`daemon.assemble` 现在安装了真实 `channelworker.Worker`、resolver、HTTP sender 与有效 retry/alert 参数（`internal/daemon/daemon.go:63-78`），所以前次“生产无 consumer”的部分已关闭。

producer 仍未闭合。全仓 `EnqueueChannelPublish` 仍只有定义；不存在 `PrepareAttentionBatch` 或 `attention_batch_members` 实现。#485 的相邻合入虽在 `AdvanceInterrupt` 增加了单条 `enqueueInterruptChannelTx`，但 production `EmitInterruptCmd` 构造点没有填 `Channels`，`EmitInterrupt` 初发提交也只创建 Forge comment；测试之外的 `InterruptChannel`/`Channels` 引用为空。因此运行中的生产链仍不会从有效 Channel registry 生成初发 `channel_publish`，batch 更无 sealing producer。没有 commit wake → production claim → HTTP → complete 的纵向测试。

证据：`internal/storage/channel.go:213-265`，`internal/storage/interrupt.go:580-599`，`internal/storage/advance_interrupt.go:165-183`，全仓引用搜索。

### P1-2 — OPEN：closed 校验增强，但仍未使用 §6.6 exact bytes/digest/replay

adapter 现在校验 batch/member delivery identity、部分 enum 和成员顺序，这是有效增量（`internal/channelworker/webhook.go:180-203`）。但唯一测试仍把规范 identity 的 base64url host/project 段改成字面量 `host:project`，把两条中文 headline/rendered text 改成 `a`/`b`，也没有复算规范 SHA-256 `ae3dba…`、canonical alert `ba1805…` 或 response-loss 同 bytes/key replay（`internal/channelworker/webhook_test.go:20-38`）。

closed/binding 也仍不完整：`links`/`options` 只是 `[]json.RawMessage`，嵌套 schema 未校验；daily/critical 的 kind、scope 与 identity grammar 没有交叉约束；adapter 不可能与尚不存在的 sealed batch/member authority 对账。因此近似 payload 可 decode 不等于 §6.6 唯一 fixture 与 sealed binding 已实现。

### P1-3 — OPEN：阈值实现有增量，规定矩阵与 durable target 证据仍缺

reclaim 已从 `SetChannelPolicy` 读取有效 threshold，跨阈值分支仍以 operation key 保证唯一 alert，并保留固定 Diagnostics 行（`internal/storage/outbox.go:249-270`，`internal/storage/channel.go:161-198`）。但 #486 没有新增任何 storage test；threshold 前后、重启、stale completion、alert 自身失败、单条/批次 target 以及 `ba1805…` digest 均未取证。

单条 alert 仍在失败时 join `interrupts/runs` 当前投影（`internal/storage/channel.go:179-185`），不是从创建 delivery 时冻结并约束的 verified target 读取；batch 所依赖的 sealed authority也未实现。故实现方的 Partial 自述准确，不能关闭 durable target 与 exact alert matrix。

### P1-4 — OPEN：不再创建 attempt 4，但 terminal result 与 §6.6 相反，且无测试

expired attempt 达 `max_attempts` 时现在会终结 operation、清 lease 并返回，不再创建下一 attempt（`internal/storage/outbox.go:249-280`）；普通 completion 的 max-attempt 裁决也保留。这关闭了前次最直接的 attempt 4 机制错误。

但 terminal reclaim 把旧 attempt result 的 `outcome` 写成 `failed`（`:261-266`），而 storage §6.6 exact vector要求旧 attempt 保留 immutable `retry/transient:lease_expired` result，同时 operation/delivery/episode 收敛为 failed。并且 #486 没有 attempt 3 expiry、旧 completion stale、无 attempt 4、阈值同 CAS 或 Retry-After 测试。`channelPolicy` 还在通用 reclaim 中对所有 operation kind 生效，而非只对 `channel_publish`（`:255-280`），会把 Channel max-attempt policy施加到其他 outbox consumer。故 P1-4 不能核销。

### P1-5 — OPEN：新增投影不是 storage §6.3/§6.5 schema，`ps` 仅展示单条子集，`doctor` 未接

#486 在运行时 `CREATE TABLE IF NOT EXISTS` 一个精简 `attention_batches`，但它缺 active storage 要求的 `kind`、`delivery_id`、`scope/scope_id`、`due_at_ms`、`payload_json/payload_digest`、sealed/delivered timestamps及完整约束；state 还额外发明 `failed`。`attention_batch_members` 完全不存在，`EnqueueChannelPublish` 以 `INSERT OR IGNORE` 直接制造 sealed batch，既不是 `PrepareAttentionBatch`，也不能证明 collision 的 frozen bytes 相等（`internal/storage/channel.go:13-36,234-245`）。

batch success 从 outbox payload 临时解析成员；查询/scan 错误被忽略，Ledger 也不通过持久 admission/member identity（`:132-157`）。没有相应测试。

`ChannelDiagnostics` 只查 `interrupt_deliveries`，不含 batch、episode、consecutive count、next retry、alert key/state 或 Channel snapshot（`:63-84`）；`ops.ps` 仅挂上这一子集，而 `ops.doctor` 仍直接调用原 doctor，完全没有 Channel join（`internal/controlplane/server.go:247-274`）。这不满足“已生成、未送达”及重启后 durable 查询面。

### P1-6 — CLOSED，未回退：secret/error summary 边界保持

adapter 仍只向 worker返回 closed error classes；resolver/sender 原始错误、endpoint/query credential 不进入持久化 summary。新增 production sender也把 HTTP/transport结果映射到 closed classes，不读取或传播响应 body。既有恶意 query token 负向测试通过。

## 3. 关闭条件对账

| #493 / #486 条件 | 结果 | 说明 |
|---|---|---|
| P1-1 生产接线/producer | **NO** | consumer 已接；production 初发/batch producer及纵向测试仍缺。 |
| P1-2 §6.6 exact vectors | **NO** | 仍是改写 fixture；无 `ae3dba…` / `ba1805…` / replay，closed sealed binding 不完整。 |
| P1-3 阈值 alert | **NO** | 配置读取有增量；完整矩阵、冻结 target 与 exact digest 无证据。 |
| P1-4 max-attempt / terminal reclaim | **NO** | 无 attempt 4 的机制已补；旧 result 写成 `failed` 与 exact vector相反，且无 terminal/stale 测试。 |
| P1-5 projections + `ps`/`doctor` | **NO** | batch schema/authority、member admission/Ledger、episode/batch 查询和 doctor均未闭合。 |
| P1-6 secret 边界不得回退 | **YES** | closed error summary 与恶意 secret 负向测试保留。 |
| 禁止自修自审 | **YES** | #486 实现方为 GPT-5.6 Luna；本轮 Sol 只写复审报告。 |

## 4. 执行证据

- 从检测到的 GitHub forge 获取并阅读 #493 与 comments（无评论）、#480/#486 与 comments、PR #490 元数据/checks及完整合入 diff。
- `git diff 3d52d21..dc69cd5 --check`：**通过**。
- `go vet ./internal/channelworker ./internal/storage ./internal/daemon ./internal/controlplane`：**通过**。
- `go test ./internal/channelworker ./internal/storage ./internal/daemon ./internal/controlplane`：**通过**。
- `go test ./...`：**通过**。
- PR #490 的 build、vet/test 为 SUCCESS，但 `schema drift check` 为 **FAIL**（生成的 `raw_config.schema.json` 未提交）；该失败主要来自同基线相邻配置变更，记为额外未清 CI 风险，不替代上述 Channel P1 判定。
- 全仓引用搜索确认：`EnqueueChannelPublish` 无调用者、`PrepareAttentionBatch`/`attention_batch_members` 不存在、production `EmitInterruptCmd` 不填 `Channels`、`ops.doctor` 无 Channel 投影。

## 5. Issue #493 验收清单

- [x] 获取并阅读 #493 全文、Agent 建议、关闭条件、约束与 comments：**YES**
- [x] 对照 #480 FAIL / #486 严格复审 P1-1..P1-6，并核验 P1-2/P1-3/P1-5 Partial 自述：**YES**
- [x] 确认 P1-6 不回退：**YES**
- [x] 结论写入 `docs/reviews/`，仅当前 conventional worktree：**YES**
- [x] 未自修自审、未 push/MR/merge：**YES**
- [ ] P1-1..P1-5 全部关闭：**NO**
- [ ] #486 可核销：**NO**

## 6. 最终裁决

**FAIL。** #486 不能关闭 #480。需补 production 初发与 batch producer、直接使用 §6.6 exact bytes/digests/replay 的 adapter/storage tests、符合 exact result 的 terminal reclaim、权威 batch/member/Ledger schema，以及完整 `ps`/`doctor` durable episode 查询，再由不同代理复审；P1-6 继续保持关闭。
