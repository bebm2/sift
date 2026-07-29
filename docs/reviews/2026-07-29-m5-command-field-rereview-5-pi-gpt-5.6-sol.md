FAIL

# command.md 字段级第五次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`ca8b4e4`（#365 / PR #367 合入；规格提交 `abdf113`）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft
> 前轮结论：[`2026-07-29-m5-command-field-rereview-4-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-4-pi-gpt-5.6-sol.md) 的 2×P1

## 1. 结论

**FAIL（1×P1）。** #365 已完整关闭 `new_attempt` 的 attempt identity P1，也消除了 Gate terminal result 对尚未创建的 evaluation/event ID 的循环，并以 replacement head 必须不同于原 head 排除了 conflict 自环。但 Gate re-evaluation 的逐 verdict terminal protocol 仍不是唯一可执行契约：新表没有冻结各 verdict 的 terminal event type/key/payload，数个 HITL 行也没有把 `VerdictV1` payload 唯一转换为合法 Interrupt facts、generation identity 和 binding；`failed` result 的 evidence/event schema 同样未闭合。

因此 [`command.md`](../specs/command.md) 应继续保持 `status: draft`，不得按现稿开始完整 Command 实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| `new_attempt` 唯一绑定 terminal attempt | **关闭** | [`storage.md` §6.4](../specs/storage.md#64-command-targeteffect-与-outcome) 明确 terminal pair 逐字段等于 binding pair，并要求同一 `(run_id,attempt_no,generation)` 命中 `status=failed`；command §7 与 report §7 已列错 Run/generation、non-failed、pair 不等和双 arm 交叉污染拒绝 vectors。 |
| Gate terminal contract | **部分关闭，仍为 P1** | [`storage.md` §8.1](../specs/storage.md#81-outbox_operations) 已加入 versioned result envelope、无循环的 complete-time ID 分配、不同 replacement head 约束和覆盖 13 个 verdict 名称的表；但该表只给状态与后继类别，没有前轮关闭条件要求的逐 verdict terminal event 与完整 successor identity/facts。 |

## 3. 剩余可执行 P1

### P1 — Gate 逐 verdict complete 仍允许不同持久化结果

[`storage.md` §8.1](../specs/storage.md#81-outbox_operations) 在表前只笼统声明 complete 插入“事件”，各行没有冻结 event type、稳定 event key、payload schema/canonical bytes 或与 evaluation/verdict 的 FK。两个实现可为同一 verified result 写不同 terminal event，仍符合现稿。

后继也仍有未闭合转换：

1. `hitl/checks_timeout|failure_review|mergeability_unknown|input_unknown` 被合并为“该 verdict facts 的 `failure_review` Interrupt”，但这些 verdict payload 形状不同，且后两者没有 [`interrupt.md` §3.1](../specs/interrupt.md) 要求的 `failure_class/failure_evidence_ref/recommended_action`。现稿未给逐分支 canonical facts、failure digest、generation preimage/key 或 `failure_review_attempt` binding，无法唯一调用 `EmitInterrupt`。
2. `hitl/guardrail_violation|code_review|merge_conflict` 只写“对应 reason、exact head/binding”，没有逐分支列出完整 binding 字段与 generation identity；`retry_checks`、`ready/merge` 也没有在 terminal matrix 冻结 successor operation key/payload。链接到既有普通 Gate 规则不能替代 command-triggered complete 的逐行事务配方。
3. 非-verdict `failed` arm只提交 `{failure_class,failure_evidence_digest}`，却未定义 `failure_class` 的 closed domain、被摘要 evidence 的 canonical schema，或 complete 所插入 failure event 的 type/payload。因而“稳定 event key”仍不足以唯一生成承诺的 event bytes、failure facts 与后继 Interrupt。

**关闭条件：** 为 `succeeded` 的每个 `VerdictV1` 行冻结 terminal event type/key/payload、精确 Run CAS 前后态、source close 和完整 successor operation/Interrupt payload/key/binding/generation；为 `failed` 冻结 closed evidence 与 event schema，并给出由 result bytes 到 event、facts digest、generation key 和 binding 的唯一算法及 exact vectors。claim、lease loss、complete 和不同 replacement-head 的现有约束应保留。

## 4. 已确认通过的改动

- `new_attempt` terminal pair 必须等于 binding `(attempt_no,generation)`，且只命中同 Run 的 failed attempt；不能选择另一失败 generation。
- attempt/quota 两个 tagged arm 的 required/null、option 与字段交叉拒绝 vectors 已列出；Report quota v1 继续没有 retry。
- `GateReEvaluationResultV1` 已有 schema version，succeeded/failed 不再预携 complete 才能分配的 evaluation/event ID。
- conflict replacement head 必须与原 operation head 不同；无 verified replacement facts 转 failed，不再产生同 key/head 自环。
- 四个 claim/complete crash cut 与 lease-loss 单 successor约束已写入验收派生。

## 5. 验收判断

- 获取并核对 Issue #369 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #365、PR #367、合入提交与完整相关 diff：**YES**
- 核销 command rereview-4 两项 P1：**YES**
- `new_attempt` identity P1 完全关闭：**YES**
- Gate terminal protocol P1 完全关闭：**NO**
- 只产出评审报告、不修改规格：**YES**
- `command.md` 转 `active`：**NO**
- 允许开始完整 Command 实现：**NO**
