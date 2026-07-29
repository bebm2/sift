FAIL

# M5 interrupt.md 字段级定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[首次字段级评审](2026-07-29-m5-interrupt-field-review-pi-gpt-5.6-sol.md) 的 I1–I5
> 声称关闭：#308 / PR #316（commit `07651f7`）；同时核对其依赖的 config/storage/outbox 补丁
> 依据：PRD §4.1–§4.4、§5.3、§5.5、§7.1/§7.3；DESIGN §6.4、§8.7、§9.2；WBS M5 §5.1–§5.3
> 交叉核对：[`brain.md`](../specs/brain.md) §11–§12、[`config.md`](../specs/config.md) §3.7–§3.9、[`storage.md`](../specs/storage.md) §6/§9/§11–§12、[`outbox.md`](../specs/outbox.md) §2/§10、[`command.md`](../specs/command.md) §2–§4

## 1. 结论

**FAIL。** #308 确实统一了 T4/T6 枚举与部分 renderer 语义，并补写了超时、admission 和 batch 的目标行为；但这些行为没有落成一份跨 interrupt/config/storage/outbox 的字段权威。首次评审 I1–I5 均仍有可执行 P1，其中 I2/I3/I5 仍使合法路径无法构造或恢复，I4 使「配额拒绝后再升级 critical」无法落库。

因此 [`interrupt.md`](../specs/interrupt.md) 必须保持 `status: draft`，不得升为 `active`。本结论不表示 M3 已冻结的七种 reason、generation key、forge comment 首发或 `startup_stall` 隔离发生回退，也不表示 M5 已实现。

## 2. I1–I5 复审对账

| 原项 | 结论 | 复审证据 |
|---|---|---|
| I1 · T4 与 renderer 契约不唯一 | **未关闭** | `brain.md` §11.2 已统一 closed output、canonical option 顺序和逐字节 brief 模板，`interrupt.md` §8.1 也补了带 run/nonce 的命令模板；但 `interrupt.md` §3 未约束 fallback `recommended_action` 必须命中当前 option。新增 §5.1 反而给 `failure_review` 固定 `recommended_action=review_report_interrupt_quota`，而其合法 option 只有 `retry/reject/hold`，形成直接反例。另有合法 `sift://event/...` link，却被 `brain.md` §11.1 的 T4 link 输入（仅 HTTPS/绝对路径）拒绝；正文也仍只有测试要求，没有首次评审要求的合法 T4→最终 bytes、拒绝回退和当前 nonce 具体 vectors。 |
| I2 · T6、Channel 与确定性裁决无法对接 | **未关闭** | T6 已统一为 `delivery=immediate|batch|next_window`，最终 high/critical 强制 immediate，零候选不调用 T6；但 `interrupt.md` §8.1 指向的 `config.md` §3.7.1 不存在。当前 config 在 §3.7 直接定义 outbox、§3.8 forge、§3.9 attention，没有 Channel registry、ID/type/target/capabilities/renderer/default/enabled 字段。`brain.md` 的 `channel_candidates/default_channel_id` 无法从 closed config 生成，outbox payload 也没有 channel/target。 |
| I3 · 升级/hold 缺少可恢复时钟与冻结字段 | **未关闭** | `interrupt.md` §4.1/§7.2/§8.2 声明持久化 `expires_after_ms`、`on_max_escalations`、downgrade、Channel、delivery、`next_dispatch_at_ms`、`held_reason`；`command.md` 还声明 `hold_max_duration_ms` 随 Interrupt 冻结。但 `storage.md` §6.1 的 `interrupts` 一列都没有，只有 `expires_at_ms/on_expire/max_escalations/dispatch_state`；§6.2 delivery 也没有 channel、escalation、interrupt version 或 nonce snapshot。并且 `on_expire=hold` 后仍保留已到期 `expires_at_ms`，正文却继续按 `expires_at_ms <= now` 扫描，未冻结排除已自动 hold 对象的 eligibility，重启或下一 tick 可反复命中。 |
| I4 · 配额拒绝与 critical 转入没有可表达的记账事实 | **未关闭** | storage 已增加 append-only `attention_admissions` 并允许 `charged_budget_entry_id=NULL`，方向正确；但 interrupt 使用 `initial_quota_charged|initial_quota_rejected_batched|initial_critical_admitted|escalation_critical_admitted|critical_fuse_rejected`，storage closed enum 却是 `quota_charged|quota_batched|critical_admitted|critical_fused`，没有唯一映射。更关键的是，`quota_batched` 明确没有 charge，而 storage 又要求任一后续 `critical_*` 必须引用该 Interrupt 的实际 `charged_budget_entry_id`；所以一条配额拒绝入批、之后 normal/high→critical 的合法 Interrupt 无法写 admission。 |
| I5 · 合批/critical 汇总仍是未定义协议 | **未关闭** | storage/outbox 已补一套协议，但 `interrupt.md` §8.3 又定义了另一套：`interrupt_batches/interrupt_batch_memberships` 对 `attention_batches/attention_batch_members`；`PrepareInterruptBatchDelivery` 对 `PrepareAttentionBatch`；`batch` arm 对 `attention_batch` arm；`interrupt-batch:` key 对 `attention-batch:` key；`suppressed` 对 `cancelled`。daily/critical batch identity 也分别使用 channel/day-start/fuse-start 与 zone/quota-day/due-at/episode-admission。两套实现会生成不同主键、状态和 payload；outbox batch payload还没有 interrupt 所要求的 delivery ID/channel binding。 |

## 3. 剩余可执行 P1

### P1-1：闭合 T4/fallback 的推荐动作与输入域

1. 在 `interrupt.md` 的 reason 契约和 `EmitInterrupt` 校验中规定 `recommended_action` 必须逐字节命中该 reason 的 canonical option ID；修正 Report 子配额 `failure_review` 的非 canonical 固定值，或拆出不承担动作语义的独立事实字段。
2. 统一 `interrupt.md` 与 `brain.md` 对 `sift://event/<32 lowercase hex>` 的 T4 输入处理：要么纳入 closed link union，要么定义不调用 T4的确定性 fallback 分支和 trace reason。
3. 增加至少一组具体 JSON/UTF-8 golden vectors：合法 T4 输出→最终 persisted headline/brief/options；options 重排、未知 fragment、Markdown/HTML/marker/动作语法→逐字节 fallback；单条和摘要使用当前 run ID/nonce 的最终命令行。

### P1-2：补 V0 Channel 的唯一 closed config

在 `config.md` 增加实际存在且可引用的 Channel schema，至少冻结 `id/type/enabled/target/capabilities/renderer/default` 及凭据引用边界；规定从 config snapshot 生成兼容候选/default、运行期 isolation 和零候选的唯一算法。同步让单条与 batch delivery/outbox payload携带可恢复的 channel snapshot/ID，关闭 PRD §12 #7 与字段规格之间的悬空引用。

### P1-3：把 expiry/dispatch 声明落入 storage 权威

1. 在 `storage.md` §6.1/§6.2 增加并约束 `expires_after_ms`、`on_max_escalations`、`suggested_downgrade`、最终 channel/delivery、`next_dispatch_at_ms`、`held_reason`、`hold_max_duration_ms`，以及 delivery 的 escalation/version/nonce snapshot；补必要 CHECK、唯一约束与扫描索引。
2. 冻结 initial、manual hold、expiry hold、每次升级、max=0/封顶后的完整 CAS 前后值；明确自动 hold 后旧 `expires_at_ms` 如何失去扫描资格，保证下一 tick/重启不会重复推进。

### P1-4：统一 admission enum 并覆盖无 charge 的 critical 升级

选定一套 admission kind/source 命名并在 interrupt/config/storage 中逐字一致；为「初发 quota-batched、后续首次进入 critical」定义可落库的 charge/admission 约束，不得要求复用不存在的 budget entry，也不得借机补扣或重复收费。补该路径、初发 critical fused、重复 tick、窗口边界与 global/per-Run 同时命中的事务 vectors。

### P1-5：删除第二套 batch 协议

在 interrupt/storage/outbox 中选定唯一的表名、member 名、prepare 端口、state enum、payload arm、operation key 和 daily/critical identity；批次 identity 必须包含足以区分 Channel/scope/window 的冻结字段。随后冻结含 channel/delivery identity 的 exact payload，覆盖并发入批、version/nonce 变化排除、空批收敛、响应丢失重放和 critical episode 恢复。

## 4. 已确认未回退

- 七种 fallback reason、canonical options、M3 renderer 子对象和 generation preimage 仍在。
- M3 首发仍是 `forge_comment` / `comment:interrupt:<interrupt_id>:1`，Channel key 未取代首发 key。
- `startup_stall` 仍禁止 `auto_reject`，封顶保持 open + hold + isolation，事实/决定仲裁仍指向统一入口。
- T4/T6/Channel 失败仍不得新增 reason 旁路、关闭原 Interrupt、退款或重复收费。

## 5. 关闭检查清单

- [x] 相对首次评审逐项复核 I1–I5
- [x] 交叉核对 brain/config/storage/outbox/command 的当前字段权威
- [x] 报告写入指定路径，首行结论为 `FAIL`
- [x] 列出剩余可执行 P1 与复审 vectors
- [ ] `docs/specs/interrupt.md` 升为 `active`（**NO：P1-1–P1-5 未关闭**）
