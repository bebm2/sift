---
status: draft
created: 2026-07-29
summary: 首个通知 Channel、投递标识与 Forge 失败兜底契约
---

# Channel 规格

本文冻结 V0 首个通知 Channel 和推送失败兜底的字段及行为。它是实现契约，不表示 Channel 已实现，也不把本 draft 转为 `active`。字段权威分别在 [`config.md` §3.7.1](config.md)、[`storage.md` §6.2–§6.6](storage.md) 和 [`outbox.md` §10](outbox.md)；本文只规定 Channel 的边界和跨规格语义。

## 1. V0 选择、目标与适配层边界

V0 的首个 Channel 是 **`webhook`**。业务层只选择冻结的 Channel snapshot、创建/推进 delivery 和 outbox operation；它不得发 HTTP、读取环境凭据或拼装供应商请求。adapter 接收 closed 的 `channel_publish` payload，解析其中的 `target_ref` 后执行一次推送并返回确定性结果；不得改写 Run、Interrupt、admission、batch 或 Command。renderer 是消费冻结 payload 的确定性代码。

`target_ref` 是非秘密 resolver handle，唯一 schema 为 `secret_ref:<name>`，其中 `<name>` 逐字节等于 config `target.secret_ref`。它不是 URL、endpoint 或凭据。adapter 是唯一 resolver owner：每次 executing attempt 仅可用 sealed payload 的 handle 向 secret resolver 读取当前 endpoint；不读取当前配置或 Channel registry。启动探测也可解析 config 的 handle 以验证可用性，但不保存解析值。

因此 secret rotation 在下一 attempt 生效；已密封 operation 永远重放同一 handle，而非冻结旧 endpoint。缺失/拒绝的 handle 是 `auth_or_capability`，非 webhook endpoint 或不合契约的解析结果是 `contract_violation`。二者均不得尝试别的 target。解析 endpoint、凭据及其 URL query 不得进入 config canonical JSON、payload、payload digest、数据库诊断、日志或 Brain 输入。完整 fixture 只在 [`storage.md` §6.6](storage.md)；其中 `target_ref` 必须使用该 handle，不能出现 URL。

V0 不把 Forge comment 当作 Channel registry 成员：它是 Interrupt 的首发决策面，Channel 是附加投递面。

## 2. 投递语义与可见标识

Channel 只有**至少一次**语义。没有可靠远端查询证据；进程在远端成功、本地写回 delivery 前崩溃时，重放同一 immutable operation/payload，重复由 worker 追加的 `[sift <operation_key>]` 供人识别。该标识不能由自然语言输入或 adapter 改写，也不宣称去重。

“已生成”是 `EmitInterrupt` 或 `PrepareAttentionBatch` 已提交本地事实及待投递 operation；“已送达”仅是 `CompleteOutboxAttempt` 成功写入 `interrupt_deliveries.state=delivered` 或 `batch_deliveries.state=delivered`。失败、retry、held 或 pending 不得冒充送达。

单条 identity 为：

```text
delivery_id:   interrupt:<interrupt_id>:<escalation_no>:<channel_id>
operation key: interrupt:<interrupt_id>:publish:<escalation_no>
```

同一 escalation 重试复用同一 key、delivery、payload、version、nonce 和 Channel snapshot，不新增 admission 或收费。batch 只能由 `PrepareAttentionBatch` 密封，并复用 storage/outbox 规定的 batch ID、`<batch_id>:publish:1` delivery 与 `attention-batch:<batch_id>:publish:1` operation；adapter 不得合并、拆分或重建 batch。

## 3. Attention 接缝与 batch Forge 告警目标

单条 payload 来自创建或 `AdvanceInterrupt` 冻结的 snapshot；batch payload 来自 `PrepareAttentionBatch` 的 sealed payload。adapter 不读取当前 Run、Forge、配置、nonce 或 registry 来刷新它。batch renderer 逐成员展示 stable ID、headline、links、options 与该 nonce 下完整命令；没有 batch 级 Command。

V0 选择**单一冻结 batch 运维目标**策略。跨项目 batch 禁止：batch identity 纳入 `project_id`，并按 [`storage.md` §6.3](storage.md#63-attention_admissionsattention_batches-与成员) 将 `forge_kind`、`forge_host`、`forge_project_key`、`target_kind`、`target_id` 各自编码进稳定 key；成员还必须具有逐字节相同的已验证 Forge discussion target（这五个字段）。因此同 project/kind/id 而 host 或 project key 不同的并发入批不会碰撞，也不得依赖并发先后作决定。该 target 在建 batch 时冻结到 `attention_batches` 和 sealed payload；不满足绑定的 candidate 不得进入该 batch，保持其既有 held/单条处理路径，worker 绝不猜测“第一成员”。故一个 batch delivery 只有一个 Forge 告警目标。

`PrepareAttentionBatch` 是唯一 sealing 入口。worker 不得改 sealed payload、排除/增加成员、改变 batch identity 或再次 admission；也不扣配额、创建/关闭/升级 Interrupt。这些动作只经 [`interrupt.md`](interrupt.md) 与 [`storage.md`](storage.md) 的既有写端口。Forge 首发、Channel delivery、batch sealing 和失败告警是同一领域事实的不同 outbox 副作用；Channel 成功不能替代 Forge comment，反之亦然。

## 4. 重试、failure episode 与 Forge 告警

worker 遵守 [`outbox.md` §1、§4、§10](outbox.md)：同领域事务先提交 operation，随后 claim/lease 和 immutable result；payload 不可改，过期 worker completion 拒绝。transient/rate-limited 按 config 的 outbox retry 和 Retry-After 继续原 operation；auth/capability 和 contract/semantic conflict 按 outbox 收敛，不能切换未冻结 target。`max_attempts>0` 达限即 `failed`，不再声称“继续重试”。

每个 immutable Channel operation 只有一个 durable failure episode，`generation=1`：单条的 `subject_id` 是其 `interrupt_deliveries.delivery_id`，batch 的 `subject_id` 是其 `batch_deliveries.delivery_id`。`CompleteOutboxAttempt` 与 `ClaimOutboxOperation` reclaim 分支是唯一写端口；二者都必须在各自的 owner/lease CAS 同一事务写 delivery、episode 和可能的 alert operation。reclaim 先为旧 attempt 写入 immutable `lease_expired` result，并在同一事务把该 result 计入 episode；不得由 worker 另起事务补写。

- `transient`、`rate_limited` 及 reclaim 写入的 `lease_expired` result 使 `consecutive_failures` 加一；reclaim 的计数、阈值 alert 与 operation/delivery/episode 终结必须同一 CAS 提交；只有 result 后未达 `max_attempts` 才在该 CAS 创建新 attempt lease，达限绝不创建新 lease/attempt；
- `auth_or_capability`、`contract_violation`、`semantic_conflict` 或 `max_attempts` 达限的失败 result 同样计入一次并终结为 `ended_failed`；
- success 将计数清零并终结为 `ended_delivered`；已终结 episode 不会因旧 worker 或 alert completion 重开；
- 从 0 到达 `attention.channel_failure_alert_after` 的事务创建且只创建一次 `forge_alert`，key 为 `alert:channel_failure:<subject_id>:1`。单条 alert 用该 Run 已冻结的 verified discussion target；batch alert 用 §3 冻结的 batch target。alert 自身失败绝不递归创建 alert。

lease CAS 保证同一 operation completion/reclaim 串行；重启从 `channel_failure_episodes` 恢复，不能由内存或 attempt 文本重算。threshold 前后、lease expiry/reclaim、重启、并发 stale completion、alert 失败和 max-attempt terminal 的 exact vectors 及投影在 [`storage.md` §6.6](storage.md) 与 [`outbox.md` §10](outbox.md)。

告警评论至少含 Channel operation key、episode/generation、连续失败数、最近安全错误分类、“已生成但未确认送达”或 terminal 状态，以及固定的 `Diagnostics: sift ps; sift doctor` 行；其 closed `forge_alert` body 与 [`outbox.md` §5.1](outbox.md#51-payload) 共用必填 `markdown`，并由持久化 operation/episode 安全字段和该固定诊断行确定性渲染，canonical exact payload/digest 见 [`storage.md` §6.6](storage.md#66-channel-batch-and-failure-episode-exact-vectors)。凭据、原始 stderr、endpoint 与 query secret 按 outbox 错误摘要规则剥离。

## 5. 升级、可见性与非目标

升级只推进原 Interrupt。`AdvanceInterrupt` 使用创建时冻结的 Channel：强提醒创建新 escalation operation，high/critical 强制 immediate；不支持 priority 时仍在原 Channel 重推。升级不另扣注意力配额，V0 没有第二 Channel 或自动 failover；唯一兜底是本规格的 Forge alert。

`sift ps`/`sift doctor` 查询 durable episode/delivery 投影，显示 Channel ID、delivery/batch identity、operation key、delivery state、attempt/连续失败数、next retry、最近安全错误、episode state、alert operation state 及“已生成、未送达”。告警评论不改变 delivery；只有 Channel success 可送达。

V0 不包括 TTS/`sift speak`、第二 Channel、批量 Command、自然语言决策解析或 Brain T6 收费口。不得为 timeout、retry 或 target 发明第二配置来源；不得改 `internal/brain/**`。

## 6. 验收派生

- [ ] webhook adapter 只消费 closed payload handle；不读取漂移配置、不持久化 endpoint、不改写领域事实；
- [ ] 单条与 batch 均至少一次，响应丢失重放同一 immutable operation/payload 且带可见标识；
- [ ] batch 禁止跨项目，密封单一 verified Forge target；无法绑定者不入 batch；
- [ ] `secret_ref:<name>` 是唯一 payload target 模型，rotation/解析失败语义可确定；
- [ ] 每个 operation 的 durable `subject_id/generation=1` episode 在阈值只建一个正确目标的 alert；terminal retry 如实停止；
- [ ] `ps`/`doctor` 可在重启后投影失败 episode，alert 失败不递归；
- [ ] sealed payload 不改写、不自扣配额；升级在原 Channel 强提醒；未实现 TTS/第二 Channel/Brain 收费口。
