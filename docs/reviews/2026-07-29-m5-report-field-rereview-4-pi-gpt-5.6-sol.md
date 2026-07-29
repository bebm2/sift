FAIL

# report.md 字段级第四次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`32c0d44`（#356 / PR #358 合入提交 `68437e0`，并含 #361 / PR #361 冲突标记 hotfix）
> 评审对象：[`docs/specs/report.md`](../specs/report.md) draft
> 前轮结论：[`2026-07-29-m5-report-field-rereview-3-pi-gpt-5.6-sol.md`](2026-07-29-m5-report-field-rereview-3-pi-gpt-5.6-sol.md) 的 P1-1/P1-2

## 1. 结论

**FAIL（2×P1）。** #356 已把 Interrupt 的 canonical-options 选择改为按来源 variant 分派，并恢复 attempt 与 Report quota 并存的两个 storage tags；#361 也清除了 PR #358 误合入的冲突标记。但前轮两个关闭条件均未完整满足：Report quota 仍没有自身的完整 fallback/T4 exact vectors；共享 attempt arm 的 new-attempt identity 仍不闭合，且没有要求的两类 arm 交叉拒绝 vectors。

因此 [`report.md`](../specs/report.md) 必须继续保持 `status: draft`，不得按现稿开始 Report 纵向实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1-1/P1-2 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| P1-1 quota canonical options | **部分关闭，仍为 P1** | interrupt §3.6 已明确 attempt 使用 `retry,reject,hold`、quota 使用 `reject,hold`，公共接纳器不再必然拒绝 quota；但前轮要求的 quota 完整 fallback JSON、T4 accept/reject exact vectors没有出现。现有唯一完整 T4 input/output/options 与 mismatch vectors仍全部是 attempt variant。 |
| P1-2 恢复 attempt recipe | **部分关闭，仍为 P1** | storage §6.4 已恢复 `failure_review_attempt` 字段并保留独立 quota arm；但 new-attempt 的 terminal pair 未被约束为 binding attempt 本身，也未冻结两个 tag 的逐字段交叉拒绝 vectors。 |

## 3. 剩余可执行 P1

### P1-1 — quota variant 仍没有可逐字节验收的 fallback/T4 契约

[`interrupt.md` §3.6](../specs/interrupt.md) 的 variant dispatch 方向正确，也足以消除“公共三项 options 必然拒绝 quota 两项 options”的直接矛盾；但该节随后唯一的 canonical input、output、persisted `options_json` 和 options mismatch 都使用 attempt `failure_review` 的 `retry,reject,hold`。

[`interrupt.md` §5.1](../specs/interrupt.md) 以 prose 给出 quota 的两个 option，且冻结了 failure digest/generation key，却没有给 quota fallback renderer 的完整 JSON bytes，也没有 quota T4 `candidate_options=[reject,hold]` 的合法 output、重排、添加 retry、unknown option及 fallback persisted bytes。§3.1 的固定对象行现在明确只属于 attempt variant，因此实现不能再把该行的完整 renderer 对象直接当作 quota golden。

**关闭条件：** 按前轮要求补出 Report quota v1 的完整 fallback JSON（headline、brief、links、min modality 和四字段 `reject,hold` options），以及 T4 接受 `reject,hold`、拒绝添加 `retry`/重排/错 recommended option 的 canonical input/output 与 persisted fallback bytes；重复 Command 和 stale nonce继续命中 command §6–§7 的唯一结果。

### P1-2 — 共享 attempt arm 仍允许漂移到另一 terminal attempt

[`storage.md` §6.4](../specs/storage.md#64-command-targeteffect-与-outcome) 的 `failure_review_attempt` 同时携 `attempt_no/generation` 与 `terminal_attempt_no/terminal_generation`。`new_attempt` 只说 terminal pair 是“被终结 attempt 的身份”，没有明确等于 binding 的 attempt/generation，也没有要求该组合命中同 Run 的 failed attempt。笼统的“组合 FK（包括 attempt/run）”不能替代这两个交叉约束。

这仍破坏 Report 与 Command 共用 closed union 的可执行性：一个实现可以只终结当前 binding attempt，另一个可以接受指向同库另一失败 attempt 的 recipe。并且 #356 没有加入前轮明确要求的 attempt 字段混入 quota arm、quota 字段混入 attempt arm、options/tag 交叉错配的具体拒绝 vectors。

**关闭条件：** 明确 new-attempt terminal pair逐字段等于 Interrupt 绑定的 `(attempt_no,generation)`，并组合约束到同 Run 的 failed attempt；补两类 tag 的 required/null、options 与 FK 交叉拒绝 vectors。Report quota arm继续保持 attempt-less、无 retry 的 `report_quota_failure_review(run_id,start,end,security_event_id)`。

## 4. 已确认通过的改动

- Report quota v1 继续保持 no-retry：`report_quota_failure_review` + `reject|hold` + running Run hold。
- interrupt §3.6 的接纳算法已按 source variant 选择 canonical option 集，不再无条件套用 attempt 三项集合。
- storage §6.4 已恢复 attempt recipe 的 `change_id/head_sha/terminal_attempt_no/terminal_generation` 字段，并与独立 quota arm并存；canonical digest与 unknown/extra-field拒绝总则已写出。
- exhaustion/rate-token 第一笔事务与 best-effort generation-key 发射第二笔事务未被 #356 回退。
- #361 已按 Issue #360 conductor 注记清除 #358 的 rebase 冲突标记。

## 5. 验收判断

- 获取并核对 Issue #360 全文、Agent 建议、范围、comment 与约束：**YES**
- 核销 report rereview-3 P1-1/P1-2：**YES**
- P1-1 quota canonical contract 完全关闭：**NO**
- P1-2 attempt recipe 完全关闭：**NO**
- 只产出评审报告、不修改规格：**YES**
- `report.md` 转 `active`：**NO**
- 允许按现稿开始 Report 纵向实现：**NO**
