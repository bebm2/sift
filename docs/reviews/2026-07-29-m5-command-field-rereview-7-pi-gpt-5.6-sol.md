FAIL

# command.md 字段级第七次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`7316c58`（#380 / PR #381 合入；实现提交 `7269a25`）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft 与 [`docs/specs/storage.md` §6.4、§8.1](../specs/storage.md)
> 前轮结论：[`2026-07-29-m5-command-field-rereview-6-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-6-pi-gpt-5.6-sol.md) 的 1×P1

## 1. 结论

**FAIL（1×P1）。** #380 已关闭 HITL binding 与 succeeded event identity 两个互斥点：`code_review` / `merge_conflict` 的 binding 现在逐字段等于 §6.4 closed arms，且 terminal event 明确为 `idempotency_key=K`、`id=event:<K>`，所有 successor 的 `created_from_event_id` 也引用后者。

但 conflict replacement 仍不是可连续执行的唯一契约。新增文字列出了完整 `GateReEvaluationOperationV1` 字段并正确指定 post-CAS `source_run_version=8`，却没有从 closed `GateReEvaluationResultV1` 唯一派生 replacement `gate_input_snapshot_id`、`gate_version` 与 `effect_binding_digest`；exact vector 直接给出 `snap-replacement`、`3`、`eeee…`，这些值既不在 result 中，也没有可复算算法。更直接地，vector 的 `replacement_input_json` 只有 `{change_id,head_sha,mergeability}`，不符合 [`gate.md` §2.2](../specs/gate.md#22-gateinputv1-闭合形态) 的 closed `GateInputV1`，按 §8.1 的 invalid replacement 规则应转 failed，不能产生其声称的 successor。因此它不是“可连续 Complete”的 exact vector。

[`command.md`](../specs/command.md) 应继续保持 `status: draft`，不得按现稿开始完整 Command 实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮关闭条件 | 本轮判断 | 证据 |
|---|---|---|
| `code_review` / `merge_conflict` binding 与 §6.4 一致 | **关闭** | §8.1 已删除两个 binding arm 内多余的 `run_id`；`run_id` 只保留在 `GateReEvaluationInterruptV1` input / Interrupt row，closed binding bytes 唯一。 |
| event `idempotency_key` 与 `id` 字节唯一 | **关闭** | §8.1 明确 `idempotency_key=K`、`id=event:<K>`，并明确后续 operation 的 `created_from_event_id` 逐字节引用 event `id`。 |
| replacement 完整 payload、post-CAS version 与连续 Complete exact vector | **部分关闭，仍为 P1** | payload 字段集合与 `source_run_version=8` 已冻结；但三个 replacement 字段没有 closed 来源/派生，且 vector 的 replacement input 不是合法 `GateInputV1`，首个 conflict Complete 按契约不能创建该 successor。 |

## 3. 剩余可执行 P1

### P1 — replacement vector 与 closed result 无法生成所声明的 successor

1. §8.1 规定 worker 只能提交 closed conflict result `{replacement_head_sha,replacement_input_json,replacement_input_hash,replacement_facts_digest}`。新增 successor 却还需要 `gate_input_snapshot_id`、`gate_version`、`effect_binding_digest`。文字仅称它们“come from the verified replacement input/facts”，没有规定 snapshot 的 insert-or-return/ID 分配、gate version 的来源，或 effect binding bytes 与 digest 的算法；exact vector 中的 `snap-replacement`、`3`、`eeee…` 不能由给出的 result bytes 独立复算。不同实现仍可产生不同 operation payload。
2. exact vector 的 `replacement_input_json` 为 `{"change_id":"change-01","head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","mergeability":"mergeable"}`。它缺少 `GateInputV1` 必需的 `schema_version`、`identity`、`change`、`checks`、`effective_policy`、risk/certification 等 closed fields。按 conflict 段“missing/invalid replacement is converted to the closed failed arm”与 Gate §2.2，该 result 必须走 failed，而不是创建 replacement operation。
3. 结尾只证明下一 operation 的 Run CAS version 命中；它没有给出一份由合法 replacement input/result 派生、能通过下一次 Complete 的 succeeded/failed/conflict result。故“therefore succeeds”只证明一个 precondition，不构成连续 Complete exact vector。

**关闭条件：** 用合法、完整、canonical `GateInputV1` 给出 replacement result；冻结 `gate_input_snapshot_id` 的事务分配/复用、`gate_version` 和 `effect_binding_digest` 的唯一来源及校验；给出这些 bytes/digests 可独立复算的完整 successor operation，并继续给出第二次 Complete 的合法 closed result 与 Run `8→9`（或其他明确 terminal 后态），证明整条 vector 实际可执行。现有 binding/event 修复、post-CAS version、不同 replacement-head 与 claim/lease/crash 约束应保留。

## 4. 非阻断注记

- §8.1 successor 表的 `merge_conflict` 行缺少结尾 `|`。多数 Markdown renderer 仍可显示，但修订 P1 时应一并恢复表格语法。
- 本轮独立复算给出的 replacement input SHA-256 `3dff99…` 与 conflict result SHA-256 `bec44e…` 均与文档一致；问题是输入 schema 与缺失派生，不是这两个摘要的算术错误。

## 5. 验收判断

- 获取并核对 Issue #382 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #380、PR #381、合入提交与完整相关 diff：**YES**
- 核销 rereview-6 剩余 1×P1：**YES**
- HITL binding 与 §6.4 唯一一致：**YES**
- event `idempotency_key` / `id` 与 successor 引用唯一：**YES**
- replacement operation 完整且可由 closed result 唯一生成：**NO**
- exact vector 可连续 Complete：**NO**
- 只产出评审报告、不修改规格或自修：**YES**
- `command.md` 保持 draft：**YES**
- 允许开始完整 Command 实现：**NO**
