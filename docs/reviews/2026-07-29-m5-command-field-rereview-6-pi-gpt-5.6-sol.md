FAIL

# command.md 字段级第六次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`082102d`（#373 / PR #375 合入；实现提交 `980288b`）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft
> 前轮结论：[`2026-07-29-m5-command-field-rereview-5-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-5-pi-gpt-5.6-sol.md) 的 1×P1

## 1. 结论

**FAIL（1×P1）。** #373 已补出 13 个 `VerdictV1` 分支的 terminal event type/key、公共 event payload、Run 后态及主要 successor 配方，也为 failed arm 冻结了 evidence union、event/facts 派生和两组可独立复算的 digest vectors。但 Gate terminal protocol 仍不是唯一可执行契约：两个 HITL successor 的 binding 与 storage §6.4 closed union 直接冲突；replacement-head conflict successor 也没有冻结完整 `GateReEvaluationOperationV1`，尤其无法决定下一次 Complete 所需的 Run version。

因此 [`command.md`](../specs/command.md) 应继续保持 `status: draft`，不得按现稿开始完整 Command 实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮关闭条件 | 本轮判断 | 证据 |
|---|---|---|
| succeeded 每个 verdict 的 terminal event、Run CAS 与 successor | **部分关闭，仍为 P1** | [`storage.md` §8.1](../specs/storage.md#81-outbox_operations) 已列齐 13 行及公共 event payload；但 `code_review`、`merge_conflict` binding 多出 closed arm 不允许的 `run_id`。event identity 文字还同时声称 `id=K` 与 `id=event:<K>`。 |
| failed 的 closed evidence/event 与唯一派生 | **关闭** | 三类 failure class/evidence shape、`O:failed` event、facts/failure digest/generation/binding 算法均已冻结；两组 result/event/facts vectors 的 SHA-256 独立复算一致，binding digest 也与冻结的 gate-recheck binding bytes 一致。 |
| replacement-head 与 claim/lease/crash | **部分关闭，仍为 P1** | replacement head 不同于原 head、无合法 replacement 转 failed、lost lease 回滚和 crash cuts 均保留；但 successor operation 只写成“same source identity plus replacement input/facts”，没有给完整 payload 或更新后的 `source_run_version`。 |

## 3. 剩余可执行 P1

### P1 — succeeded/conflict successor 仍存在互斥或缺失身份

1. [`storage.md` §6.4](../specs/storage.md#64-command-targeteffect-与-outcome) 的 closed arms 是 `code_review(change_id,head_sha,review_policy_snapshot_digest)` 与 `merge_conflict(change_id,head_sha,conflict_digest)`，都不含 `run_id`；§8.1 的逐 successor 表却分别要求 `{run_id,change_id,...}`。按 §6.4 的 unknown/extra-field 拒绝规则，这两个合法 Gate verdict 无法调用 `EmitInterrupt`；删掉 `run_id` 与保留它的实现又会产生不同 binding bytes/digest。
2. succeeded event identity 同一句要求“`idempotency_key`、`id` 都为 K”，随即又要求 ID 固定为 `event:<K>`。后续 successor 全部使用 `created_from_event_id=event:<K>`，因此实现无法从现稿唯一判断 events 主键是 `K` 还是 `event:<K>`。
3. conflict Complete 先把 Run version 加一，却只规定 replacement operation 使用“same source identity”。若沿用原 `source_run_version`，下一次 Complete 的 precondition 必然因当前 Run 已递增而失败；若改成递增后的 version，则不再是相同 source identity。该段还没有逐字段给出 operation v1 的 `gate_input_snapshot_id/gate_input_hash/gate_version/effect_binding_digest` 如何由 replacement result 填充。两个实现可写不同 payload，或写出永远不可完成的 successor。

**关闭条件：** 统一 §6.4 与 §8.1 的 `code_review`/`merge_conflict` canonical binding shape；明确 event `id` 与 `idempotency_key` 的各自字节；为 conflict 给出完整 replacement `GateReEvaluationOperationV1` canonical payload，冻结每一字段及 post-CAS `source_run_version`，并给出可连续 Complete 的 exact vector。现有逐 verdict 表、failed vectors、不同 replacement-head、claim/lease/crash 约束应保留。

## 4. 已确认通过的改动

- 13 个 Gate verdict 均已有独立 terminal event type/key 和明确 Run 后态。
- `retry_checks` 与 `ready/merge` 已冻结 operation key、closed payload 和 terminal event linkage。
- 四类 failure-review verdict 已冻结 canonical facts；guardrail/code-review/merge-conflict 也给出 generation 算法方向。
- failed result 不再预携 complete-time ID；两组 exact vector 的 result digest、facts digest、generation key 与 binding digest均可复算。
- source Interrupt 只断言既有 `closed/responded`，Complete 不再二次关闭；lease loss与回放仍收敛于单一事务 owner。

## 5. 验收判断

- 获取并核对 Issue #377 全文、Agent 建议、范围、comment 与约束：**YES**
- 获取并核对 #373、PR #375、合入提交与完整相关 diff：**YES**
- 核销 command rereview-5 剩余 P1：**YES**
- failed terminal protocol 完全关闭：**YES**
- succeeded/conflict 逐分支 terminal protocol 完全关闭：**NO**
- 只产出评审报告、不修改规格或自修：**YES**
- `command.md` 保持 draft：**YES**
- 允许开始完整 Command 实现：**NO**
