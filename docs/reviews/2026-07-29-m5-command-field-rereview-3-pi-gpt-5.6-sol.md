FAIL

# command.md 字段级第三次定向复审

评审基线：`486667e`，分支 `docs/issue-350-rereview-3-command-after-terra-r3-345`。本次按 Issue #350 只核销上一份 [`2026-07-29-m5-command-field-rereview-2-pi-gpt-5.6-sol.md`](2026-07-29-m5-command-field-rereview-2-pi-gpt-5.6-sol.md) 的 N1–N4，并对照 #345 / PR #347（合入提交 `c69bf58`）及后续 #344 / PR #348（合入提交 `486667e`）交叉核对当前 main；只产出报告，不修改规格。

## 1. 结论

**FAIL（2×P1）。** N1、N4 已关闭；N2 中 Report quota 的冲突已由当前 main 明确定为 no-retry `report_quota_failure_review` 并关闭，但 PR #348 在整合该结论时把 PR #347 已写出的 attempt `failure_review` exact-head/new-attempt recipe 又压回只有 `retry_kind` 的简写，导致 Command 所声称的 recipe 无 storage schema。N3 的数据库不可变保障已关闭，但 `gate_re_evaluation` 仍没有 closed payload/result schema，失败分支还引用当前 binding 枚举中不存在的 `failure_review_attempt(...)` arm，不能据此唯一实现 `CompleteOutboxAttempt`。

[`command.md`](../specs/command.md) 应继续保持 `draft`。

## 2. 剩余可执行 P1

| 项 | 对应前轮 | 剩余阻断 | 可执行关闭条件 |
|---|---|---|---|
| P1 | N2 | **attempt `failure_review` 的 executable binding 在后续 Report 合并中回退。** command §4 要求 attempt variant 携 exact-head / new-attempt recipe；storage §6.4 当前却只列 `failure_review(run_id,attempt_no,generation,retry_kind=gate_recheck\|new_attempt)`，没有 `change_id/head_sha`、terminal attempt/generation、tagged required/null 字段或相应组合 FK。`ApplyCommandEvent` 仍须自行查询“当前” Change/head 或猜测要 terminalize 的 attempt。Report quota arm 本身已正确改成独立、无 retry 的 `report_quota_failure_review(...)`，不再是阻断来源。 | 恢复并保留与当前 no-retry Report arm并存的 attempt tagged variants：逐 `gate_recheck` / `new_attempt` 冻结完整 JSON 字段、required/null 规则、Change/head 与 attempt/generation 组合 FK、options CHECK 和 canonical digest；同步 command 的命名，证明执行无需可漂移查询。 |
| P2 | N3 | **Gate re-evaluation 的 terminal contract 仍不是 closed 可写协议。** storage §8.1/§12.4 只有叙述性 succeeded/failed/conflict 句子，没有 `gate_re_evaluation` payload/result 的 schema version、required/null 字段、worker 可提交的 verified facts/digest、逐 verdict 的 Run/Interrupt/outbox 后继映射。failed 分支写作 `failure_review_attempt(...,gate_recheck{same change_id,head_sha})`，但当前 §6.4 既没有该 arm，也未说明 failure facts、attempt/generation、generation key 和创建事件如何派生；因此所谓原子 `CompleteOutboxAttempt` 仍无法落到 closed schema。 | 定义唯一 `GateReEvaluationOperationV1` 与 terminal outcome union，冻结 source binding、run/change/head、snapshot/evidence digest、replacement-head conflict facts及 required/null 规则；逐 succeeded verdict、failed、conflict/no-replacement 分支列出事件、Run CAS、Interrupt binding/generation、后继 operation 与幂等键，并使名称/字段真实命中 §6.4。补 claim、lease loss 与各提交点 crash vectors。 |

## 3. N1–N4 对账

| 前轮项 | 本轮判断 | 说明 |
|---|---|---|
| N1 | **关闭** | storage §6.1 已删除重复 `hold_max_duration_ms`；§11/§12.2.2 已冻结唯一 `SetApprovalLabelCutoff` 端口、只写 cutoff 的 version/nonce/NULL CAS、空流 `0`、unavailable/stale/already-cut-over、创建/轮换/扫描/迟到扫描调用图及崩溃重放。 |
| N2 | **部分关闭，仍为 P1** | 当前 main 的 Report quota v1 已采用 no-retry `report_quota_failure_review(run_id,start,end,security_event_id)`，与 interrupt/report/command 的 `reject\|hold`、running Run hold 一致；但同次整合删掉了 attempt arm 的 exact recipe，见 P1。 |
| N3 | **部分关闭，仍为 P1** | 三张 immutable Command 表已进入 storage §13 append-only trigger 清单；Gate 文档也明确 `CompleteOutboxAttempt` 为 terminal owner。但 closed operation/outcome payload、真实 failure binding 与逐 verdict 后继仍缺，见 P2。 |
| N4 | **关闭** | command §6.1 已给 non-retry initial 及四个 retry final 分支唯一完整 key；storage §6.4 冻结 initial/final FK 与一次 pending→final CAS，§13 加入列级 trigger 并校验 `final_for_event_id`、event key/type。 |

## 4. 当前 main 的 Report quota 交叉核对

- interrupt §5.1、report §6.2/§7、command §4 与 storage §6.4/§12.2.1 均将该来源定义为 attempt-less、无 retry、canonical options 仅 `reject|hold`。
- `reject` 唯一失败 Run；`hold` 只 hold/rotate Interrupt 并保持 running Run/version，不再伪造 attempt 或 Gate/new-attempt recipe。
- exhaustion/rate-token 与 best-effort 专用发射的两段边界、closed conflict、attention storage error 及 generation-key 重放在当前文档中一致。
- 因此不得沿用上一轮 N2 的“Report quota 必须携 gate/new-attempt recipe”前提；本轮阻断仅是 attempt arm 被后续合并回退。

## 5. 验收判断

- 对照 N1–N4 核销 #345 / PR #347：**YES**
- 按当前 main 交叉核对 #344 / PR #348 的 no-retry Report quota binding：**YES**
- 只产出评审报告、不改规格：**YES**
- `command.md` 转 `active`：**NO**
- 剩余 P1 清零：**NO（2 项）**
- 允许开始完整 Command 实现：**NO**

## 6. 结论

**FAIL。** cutoff 与 retry outcome key/trigger 已达到可实现程度，Report quota v1 的 no-retry 决策也已在当前 main 对齐；但 attempt binding recipe 在 #344 整合后回退，Gate re-evaluation 仍缺 closed operation/outcome 协议。关闭上述两项并再次定向复审前，[`command.md`](../specs/command.md) 保持 `draft`。
