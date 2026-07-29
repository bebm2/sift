FAIL

# M5 interrupt.md 字段级第二次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[上轮定向复审](2026-07-29-m5-interrupt-field-rereview-pi-gpt-5.6-sol.md) 的 P1-1…P1-5
> 声称关闭：#327 / PR #332（merge commit `47ad76a`，实现 commit `957911a`）
> 交叉核对：[`brain.md`](../specs/brain.md) §11–§12、[`config.md`](../specs/config.md) §3.7.1/§3.9、[`storage.md`](../specs/storage.md) §6/§9/§11/§15、[`outbox.md`](../specs/outbox.md) §2/§10、[`command.md`](../specs/command.md) §3–§4

## 1. 结论

**FAIL。** PR #332 确实修正了 Report 子配额 `recommended_action`、放开 T4 的安全事件链接输入、增加 V0 Channel registry，并统一了 admission 枚举与无 charge 的 critical 升级；但 P1-1、P1-2、P1-3、P1-5 尚未闭合。当前最严重的剩余问题是：manual hold 的恢复扫描被永久排除，以及 batch 没有可持久化的 Channel/delivery identity，因而合法的多 Channel batch 无法 sealed/replay。

[`interrupt.md`](../specs/interrupt.md) 应继续保持 `status: draft`，不得升为 `active`。本结论只核销 #327 对上轮 P1 的关闭声明，不表示 M3 已冻结行为回退，也不表示 M5 已实现。

## 2. P1-1…P1-5 对账

| 项目 | 结论 | 证据 |
|---|---|---|
| P1-1 · T4/fallback 推荐动作与输入域 | **未关闭** | `failure_review` 的 Report 子配额现已改为 canonical `hold`，`brain.md` §11.1 也接受 `sift://event/...`。但 `interrupt.md` §3 仍未规定所有 fallback facts 的 `recommended_action` 必须命中该 reason 的 canonical option，也未规定 `EmitInterrupt` 对此作统一校验；当前只在 T4 输出和 Report 特例中校验。新增 §3.6 声称 HTML/marker/`/sift` fragment 返回 `interrupt_t4_unsafe_fragment`，而 `brain.md` §11.1 允许由冻结 facts 导出任意无 Cc/换行 fragment，§11.2 对逐字节命中的 fragment 会转义并接纳；没有定义该 unsafe 拒绝分支，vector 与 closed 接纳器矛盾。并且仍无一组具体的“合法 T4 JSON → 最终 persisted headline/brief/options bytes”及对应逐字节 fallback JSON vector。 |
| P1-2 · V0 Channel closed config | **未关闭** | `config.md` §3.7.1 已冻结 `id/type/enabled/target/capabilities/renderer/default`、候选/default/零候选算法和凭据边界；单条 storage/outbox 也新增 Channel snapshot，方向正确。但该节同时要求每个 batch 冻结 Channel snapshot，`storage.md` §6.3 的 `attention_batches` 与 `attention_batch_members` 却均无 `channel_id/channel_snapshot/delivery_id`，无法构造 `outbox.md` §10 所要求的 batch `channel`。单条和 batch payload 也都没有 `delivery_id`，未关闭“可恢复 delivery identity”。 |
| P1-3 · expiry/dispatch storage 权威 | **未关闭** | `storage.md` §6.1 已补大部分 expiry/dispatch 列和 CAS 描述，但 `hold_max_duration_ms` 在同一 `interrupts` 列表中重复定义两次。更关键的是，`command.md` §4 的 manual hold 会更新 expiry 并写 `dispatch_state=held/held_reason=manual`，而 storage 的扫描谓词排除所有 `held`；该对象到期后永远不会再由 `AdvanceInterrupt` 唤醒，manual hold duration 失去可恢复语义。§15 也只有 `interrupts(status, expires_at_ms)`，没有 `next_dispatch_at_ms` 到期扫描索引；新增字段缺少所要求的成对 CHECK/扫描约束。 |
| P1-4 · admission enum 与无 charge critical 升级 | **已关闭** | interrupt/config/storage 现统一使用 `quota_charged | quota_batched | critical_admitted | critical_fused`。`storage.md` §6.3 明确 `quota_batched → critical_*` 的 `attention_charge_entry_id=NULL`，不得补造 charge；`<interrupt_id>:initial|critical`、partial unique indexes 和 config vectors覆盖重复 tick、窗口边界及 global 优先。 |
| P1-5 · 唯一 batch 协议 | **未关闭** | 表名、prepare port、state、payload arm 与 operation key 已统一为 `attention_*`/`PrepareAttentionBatch`/`attention_batch`，第二套命名已删除；但 identity 仍为 `daily:<zone>:<due_at_ms>` 或 `critical:<scope>:...`，不含 Channel。不同 Channel 的成员会碰到同一个 batch，而 batch/member schema 又不保存 Channel，无法满足 outbox 的“成员冻结 Channel identity 一致”。此外 `interrupt.md` §8.3 要求 batch payload 含 delivery ID，实际 `outbox.md` §10 无该字段；其 daily `scope_id` 示例 `Asia/Shanghai:2026-07-29` 也与 `storage.md` §6.3 的 `<zone>:<due_at_ms>` 不一致。故 exact payload、并发入批和响应丢失重放仍无唯一可实现协议。 |

## 3. 剩余可执行 P1

### P1-R1：闭合 T4/fallback 接纳器

1. 在 reason 契约和 `EmitInterrupt` 前置校验中统一规定：任意来源的 `recommended_action` 必须逐字节命中当前 canonical option ID。
2. 在 `brain.md`/`interrupt.md` 二选一冻结 frozen fragment 的安全域：要么输入阶段拒绝 HTML/marker/动作语法并定义唯一错误码，要么删除与“逐字节命中后转义接纳”矛盾的拒绝 vector。
3. 增加具体 closed T4 input/output 与最终 persisted JSON/UTF-8 bytes；为重排、未知 fragment、unsafe fragment 和安全事件链接分别给出最终 fallback/accept bytes，而非只列结果名称。

### P1-R2：修复 hold/dispatch 的可恢复状态机

1. 删除 `hold_max_duration_ms` 重复列，并为 dispatch/held 字段补成对 CHECK。
2. 区分 manual timed hold 与 terminal automatic hold 的扫描资格，冻结 manual hold 到期后的唯一 CAS 前后值；重启后必须能按同一时钟恢复，automatic expiry/max hold 不得重复命中。
3. 增加 `next_dispatch_at_ms` 到期扫描索引及其 closed 扫描谓词，覆盖 expiry 与 dispatch 同刻竞争。

### P1-R3：把 Channel/delivery identity 落入唯一 batch 权威

1. 在 batch identity、`attention_batches`/members 与 sealed payload 中冻结一致的 `channel_id/channel_snapshot`；不同 Channel 不得碰撞到同一 daily/critical batch。
2. 为单条与 batch exact payload补不可变 `delivery_id`，并令 storage delivery 投影、operation 与 payload 可逐字段互证。
3. 统一 daily `scope_id` 的 epoch/date 表示；补两个不同 Channel 同 zone/due-at 并发入批、成员 version/nonce 排除、空批、seal 后响应丢失重放的 exact payload vectors。

## 4. 已确认关闭且未回退

- Report 子配额 `recommended_action=hold` 的 digest/generation golden 值可按 canonical bytes 复算一致。
- T4 closed link union 已接受仅限 `failure_evidence_ref` 的服务端安全事件引用。
- V0 Channel registry 已存在，零兼容 Channel 不调用 T6，forge comment 首发仍保留。
- `quota_batched → critical` 不补扣、不伪造 charge；admission 名称已跨 interrupt/config/storage 对齐。
- M3 forge comment key、七种 reason、generation key 与 `startup_stall` 的事实优先/隔离语义未被 #332 改写。

## 5. 关闭检查清单

- [x] 获取并核对 #335 全文与评论
- [x] 核对 #327 / PR #332 已合入及实际 diff
- [x] 逐项复核上轮 P1-1…P1-5
- [x] 交叉核对 interrupt/config/storage/outbox/brain/command
- [x] 报告写入指定路径，首行为 `FAIL`
- [x] FAIL 列出剩余可执行 P1
- [x] `docs/specs/interrupt.md` 保持 `draft`
- [ ] P1-1…P1-5 全部关闭（**NO：P1-1、P1-2、P1-3、P1-5 仍未关闭**）
