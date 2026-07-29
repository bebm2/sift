---
status: draft
created: 2026-07-29
summary: 首个通知 Channel、投递标识与 Forge 失败兜底契约
---

# Channel 规格

本文冻结 V0 的首个通知 Channel 及其推送失败兜底，供 M5 实现引用。它是字段与行为契约，不表示 Channel 已实现，也不把本 draft 转为 `active`。

权威边界见 [DESIGN §2.3、§6.4、§8.7](../DESIGN.md)；Interrupt 生成、升级、调度与批次见 [`interrupt.md`](interrupt.md)；配置字段见 [`config.md` §3.7.1、§3.9](config.md)；持久化列与写端口见 [`storage.md` §6.1–§6.6](storage.md)；operation、重试和告警见 [`outbox.md` §2、§10](outbox.md)。本文不复制这些规格的字段表或 golden vector。

## 1. V0 选择与适配层边界

V0 的首个 Channel 实现是 **`webhook`**：由配置的 `secret_ref` 解析目标，将服务端确定性渲染的文本提交到 webhook endpoint。webhook 的具体 HTTP 客户端是适配层实现细节；业务层只依赖 Channel 端口，不依赖 HTTP、供应商 SDK 或 URL 形状。

Channel 是可替换的适配层端口，与 Forge 适配器、SQLite、Agent 后端同层。它不得渗入领域层：

- domain/application 只选择已冻结的 Channel snapshot、创建/推进 delivery 和 outbox operation；不得直接发 HTTP、读环境凭据或拼装供应商 payload；
- Channel adapter 只接收 closed 的 `channel_publish` payload，执行一次推送并返回确定性成功、瞬时失败、限流、认证/能力或契约错误；不得改变 Run、Interrupt、Attention admission、batch membership 或 Command；
- renderer 是确定性代码，消费冻结 payload；不得让 Channel、LLM 或 webhook 响应修改 headline、brief、links、options、nonce、severity 或命令语义；
- webhook target 只通过 [`config.md` §3.7.1](config.md) 的 `secret_ref` 取得。凭据不进入配置 canonical JSON、日志、Brain 输入、payload、operation digest 或诊断摘要。

V0 不把 Forge comment 当作 Channel registry 成员：Forge comment 是 Interrupt 的首发决策面；Channel 是同一 Interrupt 的附加投递面。

## 2. 投递语义与可见标识

Channel 只有**至少一次**语义。webhook 没有可靠的远端查询证据；成功响应只说明本次请求返回成功，不能证明没有重复。进程在远端成功、写回本地 delivery 前崩溃时，worker 必须按同一 immutable operation/payload 重放，重复必须对人可辨认。

必须严格区分：

- **已生成**：Interrupt 已由 `EmitInterrupt` 提交，或已有 sealed batch 已由 `PrepareAttentionBatch` 提交；这只说明本地事实、admission 和待投递 operation 存在；
- **已送达**：对应 `interrupt_deliveries.state=delivered` 或 `batch_deliveries.state=delivered`，且由 `CompleteOutboxAttempt` 记录成功结果；这才说明一次 Channel 调用返回成功。

Channel 失败、重试、held 或 operation pending 都不得把“已生成”标记为“已送达”。delivery 状态、attempt/result 与 `delivered_at_ms` 以 [`storage.md` §6.2、§6.5](storage.md) 为准；不存在“尽力通知所以默认送达”的状态。

每次单条投递的可见标识为 `[sift <operation_key>]`，由 worker 根据 operation key 追加，不能由自然语言输入或 adapter 改写。单条 delivery 的稳定身份与 operation key 为：

```text
 delivery_id:    interrupt:<interrupt_id>:<escalation_no>:<channel_id>
 operation key:  interrupt:<interrupt_id>:publish:<escalation_no>
```

同一 escalation 的重试复用相同 key、delivery ID、payload、Interrupt version、nonce 和 Channel snapshot；不创建第二次 admission 或注意力收费。Channel 的可见 operation 标识只用于识别重复，不宣称去重。

批次不另起键空间：它只能由 storage 的 `PrepareAttentionBatch` 从已密封成员生成，并复用 [`outbox.md` §10](outbox.md) 的 `attention_batch` arm：

```text
 delivery_id:    <batch_id>:publish:1
 operation key:  attention-batch:<batch_id>:publish:1
 member delivery: <batch_id>:<interrupt_id>
```

`batch_id`、sealed members、channel snapshot、delivery ID、rendered text 与 payload digest 必须逐字段来自 [`storage.md` §6.3、§6.6](storage.md)。Channel adapter 不得自行合并、拆分或重建 batch。

## 3. Channel 输入与 Attention 接缝

Channel 只消费已密封的 delivery/batch payload：

- 单条 payload 来自创建或 `AdvanceInterrupt` 冻结的 Interrupt snapshot；
- batch payload 来自 `PrepareAttentionBatch` 的 immutable sealed payload；
- adapter 不读取当前 Run、Forge、配置文件、当前 nonce 或当前 Channel registry 来“刷新” payload；
- batch renderer 必须逐成员展示稳定 ID、headline、links、canonical options 和该成员 nonce 下的完整命令；摘要没有 batch 级动作，不能批量执行 Command。

`PrepareAttentionBatch` 是唯一的 sealing 入口。Channel worker 不得改写其 sealed payload、排除成员、增加成员、改变 batch identity 或再次调用 Attention admission。Channel 不扣配额、不写 `attention_admissions`、不创建 Interrupt，也不关闭/升级 Interrupt；这些动作只能经 [`interrupt.md`](interrupt.md) 与 [`storage.md` §12.2、§12.4](storage.md) 的既有写端口。

Forge 首发、Channel delivery、batch sealing 和失败告警都是同一领域事实的不同 outbox 副作用；不能用 Channel 成功替代 Forge comment，也不能用 Forge comment 成功伪造 Channel delivery。

## 4. 投递、重试与错误收敛

Channel worker 遵守 [`outbox.md` §1、§4、§10](outbox.md)：先由同领域事务提交 `channel_publish` operation，再 claim/lease，随后记录 immutable attempt/result；若同一 worker 还执行 Forge API/CLI 调用，该调用仍须经 Forge 唯一收费入口，但 webhook 本身不是 Forge API，不伪造 Forge API charge。payload 创建后不可改；过期 worker 的完成结果拒绝。

- transient / rate-limited：按 [`config.md` §3.7](config.md) 的 outbox `retry_initial_delay`、`retry_max_delay`、`retry_multiplier` 与 Retry-After 退避，继续原 operation；
- auth/capability：按 outbox 错误分类标记 operation 失败并隔离/诊断适用的 Channel 或项目；不得切换到一个未冻结的目标；
- contract/semantic conflict：fail closed，保留可见故障，不把响应解释为成功；
- operation 达到 `channel_failure_alert_after` 的连续失败阈值：在同一 complete 事务中，以唯一 `forge_alert` operation 发出 forge 告警，但原 Channel operation 仍继续重试；告警失败不递归创建告警。

阈值唯一来自 [`config.md` §3.9](config.md) 的 `attention.channel_failure_alert_after`，默认值和范围不在本文重述。超时、lease、重试和最大尝试次数唯一来自 outbox/runtime 配置；本文不建立第二套 Channel 配置源。

forge 告警的 `purpose=channel_failure`，稳定 key 遵守 [`outbox.md` §2、§5.1](outbox.md) 的通用格式：

```text
alert:channel_failure:<subject_id>:<generation>
```

`subject_id` 必须稳定地标识失败的单条 delivery 或 batch delivery（实现应使用其持久化 delivery identity，不使用 worker、时间或可变错误文本）；同一连续失败 episode 只创建一个告警 operation。告警通过该 Run 已验证的 Issue、Change 或 manual Run 冻结的 discussion target 发表评论，使用 Forge 适配层和已有 `forge_alert`/comment worker，不能把 webhook 当作告警的第二通道。

告警评论至少应明确：Channel delivery 的可见 operation key、失败次数/episode、最近安全的错误分类、当前状态为“已生成但未确认送达”或“重试中”，以及 `sift ps`/`sift doctor` 的故障标记。凭据、原始 stderr、URL query secret 和 payload 中的敏感值必须按 outbox 错误摘要规则剥离。

## 5. 升级与强提醒

升级只推进既有 Interrupt，不生成新的 reason。`AdvanceInterrupt` 按 [`interrupt.md` §8.2](interrupt.md) 使用创建时冻结的 Channel 选择、delivery downgrade 和 expiry snapshot：

- 强提醒档位仍使用同一个 Channel，并为该 escalation 创建新的 `interrupt:<id>:publish:<escalation_no>` operation；
- high/critical 的最终 severity 强制 immediate；较低 severity 的调度遵守冻结的 batch/窗口结果；
- 一个 Channel 不支持优先级时，不寻找第二 Channel，而是在原通道重推同一 escalation 的强提醒档位；renderer 仍展示当前完整命令；
- 升级重推复用既有 attention charge，不再次扣配额；熔断/合批仍只由 Attention 写端口裁决。

V0 没有第二 Channel 备用。Channel 隔离、零兼容 Channel 或 Channel delivery 失败都不能被描述为已转移到备用通道；本节唯一兜底是 Forge 告警评论。

## 6. 可见性与运维投影

连续失败 episode 必须在本地投影中可查询，并由 `sift ps` 与 `sift doctor` 显示：Channel ID、delivery/batch identity、operation key、当前 state、attempt/连续失败次数、下次重试时间、最近安全错误分类、告警 operation 状态和是否仍为“已生成、未送达”。

告警评论只是第二渲染面，不改变 delivery 的失败状态；只有 Channel worker 的成功结果才能把 delivery 标记为 delivered。重启后，未完成 operation、retry deadline 和 failure episode 从 DB 恢复，不依赖内存计数或当前配置重建历史事实。

## 7. 非目标与明确缺口

V0 不包括：

1. TTS、`sift speak` 或语音专用实现；`voice` 只是已有 modality/capability 契约，非本 Channel 的实现承诺；
2. 第二个 Channel、备用 Channel、跨 Channel 自动 failover 或把 Forge 告警误称为备用 Channel；
3. 让 Brain T6 成为收费口、状态机入口、配额入口或 delivery 真相源；T6 只能按 [`interrupt.md` §7.2](interrupt.md) 给冻结快照提供建议；
4. Channel 自定义 Command、批量审批、批次级动作或自然语言决策解析；
5. 为失败阈值、HTTP timeout、retry 或 target 创建第二个配置来源。

当前配置已冻结失败告警阈值与 outbox retry 默认值；若实现还需要 webhook 请求 timeout 的独立字段，应先在 [`config.md`](config.md) 增补并评审，不能在 Channel 代码或本 draft 静默发明配置。Forge 告警的状态/展示字段若尚未在 `ps`/`doctor` 规格中落位，属于实现前缺口，不得以临时内存状态代替持久化事实。

## 8. 验收派生

- [ ] webhook adapter 只接受 closed、已密封 payload；不读取漂移配置、不改写 payload，不接触 domain/storage；
- [ ] 单条 Channel 为至少一次：响应丢失重放同一 operation/payload，重复带可见 `[sift <operation_key>]`；
- [ ] 已生成与已送达分别由 Interrupt/delivery 状态证明，失败不会写 delivered；
- [ ] 单条与 attention batch 均使用 `outbox.md`/`storage.md` 的既有 `channel_publish` 键，不产生第二套 key；
- [ ] batch sealing 前关闭/version/nonce 变化成员被排除，sealed payload 后 worker 不可改写，空 batch 不发；
- [ ] 连续失败达到配置阈值只建一个 `forge_alert(channel_failure)`，原 Channel operation 继续重试，告警失败不递归；
- [ ] `sift ps`/`sift doctor` 显示 delivery 故障与“已生成/未送达”，重启可恢复；
- [ ] escalation 在原 Channel 以强提醒档位重推，不新增配额扣费，不启用第二 Channel；
- [ ] 未实现 TTS/第二 Channel/Brain T6 收费口，且不改 `internal/brain/**`。
