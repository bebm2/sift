FAIL

# command.md 字段级第四次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`32c0d44`（#356 / PR #358 合入提交 `68437e0`，并含 #361 / PR #361 冲突标记 hotfix）
> 评审对象：[`docs/specs/command.md`](../specs/command.md) draft
> 前轮结论：[`2026-07-29-m5-command-field-rereview-3-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-3-pi-gpt-5.6-sol.md) 的 P1/P2

## 1. 结论

**FAIL（2×P1）。** #356 恢复了 attempt `failure_review_attempt` 的字段外形，也新增了 `GateReEvaluationOperationV1` 与 terminal outcome union 的骨架；#361 已按 Issue #360 的 conductor 注记清除 `interrupt.md` 中误合入的 rebase 标记。但两项前轮阻断都只部分关闭：new-attempt recipe 没有冻结 terminal pair 与 binding attempt 的相等关系/failed 约束；Gate re-evaluation 仍缺可唯一执行的 failed generation、逐 verdict 后继和无循环身份的 terminal result。

[`command.md`](../specs/command.md) 应继续保持 `status: draft`，不得按现稿开始完整 Command 实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1/P2 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| P1 attempt recipe | **部分关闭，仍为 P1** | storage §6.4 现有 closed tag、两套 required/null 字段、canonical digest 与 FK/CHECK 总则；但 `new_attempt` 只说 terminal pair 是“被终结 attempt 的身份”，没有像回退前契约那样要求它逐字段等于 binding 的 `(attempt_no,generation)`，也没有要求该组合 FK 命中同 Run 的 `failed` attempt。实现仍可合法终结另一个失败 attempt。 |
| P2 `GateReEvaluationOperationV1` | **部分关闭，仍为 P1** | operation payload 和 `succeeded|failed|conflict` 名称已出现；但 failed arm、succeeded arm及 verdict 后继仍未形成前轮要求的 closed terminal protocol，见 P2。 |

## 3. 剩余可执行 P1

### P1 — `new_attempt` 仍未唯一绑定要终结的 attempt

[`storage.md` §6.4](../specs/storage.md#64-command-targeteffect-与-outcome) 的 `failure_review_attempt` 同时携 `attempt_no/generation` 与 `terminal_attempt_no/terminal_generation`。当前文字只要求前一对等于 Interrupt 绑定的 attempt，后一对等于未进一步定义的“被终结 attempt 身份”；结尾的“组合 FK（包括 attempt/run）”也没有声明两对必须相等，或 terminal pair 必须命中同 Run、状态为 `failed` 的 attempt。

因此 `ApplyCommandEvent(new_attempt)` 仍有两种符合现稿的实现：终结 Interrupt 绑定的失败 attempt，或终结 recipe 指向的另一个失败 attempt。后者会让合法 Command 改写错误 generation，正是冻结 recipe 要排除的漂移。

**关闭条件：** 在 closed arm 中明确 `terminal_attempt_no=attempt_no`、`terminal_generation=generation`，并以 `(run_id,terminal_attempt_no,terminal_generation)` 组合 FK/CHECK 命中该 Interrupt 所绑定的 failed attempt；给错 Run、错 generation、非 failed、两对不等及 quota/attempt 字段交叉污染的拒绝 vectors。

### P2 — Gate terminal union 仍有循环身份且缺逐 verdict 唯一后继

[`storage.md` §8.1](../specs/storage.md#81-outbox_operations) 新增的 terminal union 尚有三处不可唯一实现：

1. `succeeded{evaluation_id,...}` 要求提交持久 `evaluation_id`，下一段却规定 `CompleteOutboxAttempt` 才“插入/复用 Gate evaluation”。现稿没有冻结由 worker 预分配 ID、按 snapshot/digest 查既有 evaluation，还是由 complete 创建 ID；两个实现可提交不同 result bytes。
2. `failed{failure_class,failure_digest,failure_event_id}` 同样先要求 `failure_event_id`，随后又规定 complete “插入 failure event”；并且没有冻结 `failure_evidence_ref`、`recommended_action`、attempt/generation 与 Interrupt generation key 如何从 operation/result 派生。该 arm无法唯一构造它承诺的 `failure_review_attempt`。
3. succeeded 只写“按 verdict 的唯一 Gate 映射推进 Run”。Gate §2.3 的后继表包含 13 个 verdict，但没有在本 terminal matrix 中穷尽每个 verdict 的 Run CAS、Interrupt/outbox/event 结果；例如 `wait_checks`、各 `hitl`、`retry_checks`、`ready/merge` 不能由这句话唯一落库。

此外 terminal outcome 没有自己的 schema version，所谓 worker 提交“同 shape”的 verified result 也未定义 envelope、canonical bytes、unknown-field 与 digest 规则。

**关闭条件：** 冻结 versioned terminal-result envelope，消除 evaluation/event ID 的先有后写循环；为 failed/no-replacement 给出完整 canonical failure facts、attempt identity、generation preimage/key 与事件 key；逐 `VerdictV1` 行列出唯一 Run CAS、source Interrupt close、事件及 successor operation/Interrupt，并补 claim、lease loss 和 complete 各提交点 vectors。

## 4. 已确认通过的改动

- attempt 与 Report quota 已使用不可互换的 `failure_review_attempt` / `report_quota_failure_review` tags；quota arm继续无 retry。
- `gate_recheck` 已冻结 exact `change_id/head_sha`，terminal-attempt 字段为 NULL，不再要求执行时查询当前 Change。
- gate re-evaluation 已有稳定 `gate:<source_interrupt_id>:<head_sha>:reeval:1` key、verified replacement-head conflict arm，并把 terminal owner 收敛到 `CompleteOutboxAttempt`。
- #361 已清除 #358 在 `interrupt.md` 留下的冲突标记；本报告未以中间提交 `68437e0` 的破损文本作为最终基线。

## 5. 验收判断

- 获取并核对 Issue #360 全文、Agent 建议、范围、comment 与约束：**YES**
- 核销 command rereview-3 P1/P2：**YES**
- P1 attempt recipe 完全关闭：**NO**
- P2 Gate terminal contract 完全关闭：**NO**
- 只产出评审报告、不修改规格：**YES**
- `command.md` 转 `active`：**NO**
- 允许开始完整 Command 实现：**NO**
