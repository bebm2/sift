FAIL

# report.md 字段级第三次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`486667e`（#344 / PR #348；内容提交 `643522e`）
> 评审对象：[`docs/specs/report.md`](../specs/report.md) draft
> 前轮结论：[`2026-07-29-m5-report-field-rereview-2-pi-gpt-5.6-sol.md`](2026-07-29-m5-report-field-rereview-2-pi-gpt-5.6-sol.md) 的剩余 2×P1
> 交叉核对：active [`storage.md`](../specs/storage.md)，以及 draft [`interrupt.md`](../specs/interrupt.md)、[`command.md`](../specs/command.md)

## 1. 结论

**FAIL（2×P1）。** #344 已统一 Report quota facts 的 `recommended_action=hold`，选定无 retry 的 `reject|hold` variant，并把 quota-exhaustion 冻结为先提交安全事实/rate token、再 best-effort 发射的两段事务；前轮 P1-2 因而关闭。但同一合入仍让 quota variant 与 Interrupt 的公共 canonical-options 校验形成相反要求，并把 active storage 中 attempt `failure_review` 的完整 effect recipe 删除成无法承载 Command 所需身份的 arm。

因此 [`report.md`](../specs/report.md) 必须继续保持 `status: draft`，本轮不得升为 `active`，也不得按现稿开始 Report 纵向实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮剩余 P1 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| P1-1 Report quota `failure_review` 唯一 effect binding | **未关闭** | quota 自身已选 `reject|hold` 且新增独立 storage arm，但 Interrupt 公共接纳器仍要求 §3.1 的 `retry|reject|hold`；同时 #344 回退了共享 attempt binding 的可执行 recipe。见 P1-1/P1-2。 |
| P1-2 quota-exhaustion 结构拒发原子结果 | **关闭** | report §6.2/§7、interrupt §5.1、storage §12.2.1 均规定 exhaustion/rate-token 第一笔先提交；第二笔发射失败只回滚发射并幂等记录诊断。publish target、binding/schema、attention storage、崩溃重放和四并发断言均已列入。 |

## 3. 剩余可执行 P1

### P1-1 — quota `reject|hold` 对象仍不能通过 Interrupt 自己的 canonical-options 接纳器

[`interrupt.md` §5.1](../specs/interrupt.md) 明确把 Report quota v1 options 冻结为 `reject|hold`，禁止 `retry`；但同文件 §3.1 仍把 `failure_review` 的固定对象定义为 `retry|reject|hold`，§3.6 又无条件规定“所有来源”的 fallback/T4 options 必须逐字段、同序等于 §3.1 的 canonical 集合。现有 mismatch vector 也只认识公共三项集合。

实现若按 §3.6 校验，quota 对象会因缺 `retry` 被 `interrupt_t4_options_mismatch` 拒绝；若按 §3.1 生成，则会暴露 §5.1、command §4 和 storage §6.4 明令禁止且无 binding 的 `retry`。这正是前轮要求同步 versioned options/canonical vectors 所要消除的二义性，不能靠“§5.1 更具体”留给实现猜测。

**关闭条件：** 在 Interrupt 的 canonical 对象选择与接纳算法中显式以 `failure_review` variant/source 分派；冻结 quota `reject,hold` 的完整 fallback JSON（含四字段 option bytes）及 T4 candidate-options 接受/拒绝 vectors，并让 §3.1/§3.6 的公共规则明确排除或参数化该 variant。重复 Command 与 stale nonce 仍须沿 command §6–§7 得到唯一结果。

### P1-2 — #344 删除了 active storage 中 attempt `failure_review` 的完整 recipe

[`storage.md` §6.4](../specs/storage.md) 现在把 closed arm 写成 `failure_review(run_id,attempt_no,generation,retry_kind=gate_recheck|new_attempt)`，并规定未知字段拒绝；该 shape 没有 `change_id/head_sha`，也没有 `terminal_attempt_no/terminal_generation`。但同一 active storage §8.1 仍要求写 `failure_review_attempt(...,gate_recheck{same change_id,head_sha})`，而 [`command.md` §4](../specs/command.md) 仍要求 binding 携带 exact-head / new-attempt recipe 才能执行 retry。

这不是仅影响别的 reason 的注记：`interrupt_command_effect_bindings` 是 Report quota 与 attempt failure 共用的 closed reason-tagged union，active storage 是实现权威。#344 合入前的文本曾明确列出两种 attempt recipe 的 required/null 字段与组合 FK；本次为加入 quota arm 而整体替换后，共享 union 已无法同时满足 storage 自身和 Command。实现只能拒绝 §8.1 的合法写入，或接受 §6.4 所禁止的额外字段。

**关闭条件：** 恢复 attempt `failure_review` 的完整 tagged arms、字段、FK/CHECK 和 recipe 身份，同时保留独立的 `report_quota_failure_review(run_id,daily_bucket_start_ms,daily_bucket_end_ms,security_event_id)` 无 retry arm；同步 command/storage 命名并加入两类 arm 的交叉拒绝 vectors。不得从当前 Change/attempt 隐式补齐缺失身份。

## 4. 已确认通过的改动

- report/interrupt 已统一 quota facts 为 `recommended_action=hold`；独立复算仍得到 `failure_digest=59da82e3…6509` 与 generation key `cf9ab880…a083`。
- Report quota v1 已明确选择无 retry 的 `reject|hold`，quota binding 不再伪造 nullable attempt 或 generation。
- command 已明确 quota `hold` 只 rotate nonce/hold Interrupt，CAS 保持 Run `running` 及 version；`reject` 才令 Run failed。
- exhaustion 第一笔事务先线性化安全 event、exhaustion 和一次 rate token；第二笔 generation-key 发射失败不再反向回滚第一笔。
- 同日已有 exhaustion 的重放在 rate CAS 前复用事实；并发 INSERT loser 回滚 tentative CAS，故四并发至多消费一次 token、写一条 exhaustion、生成一个 Interrupt。

## 5. 验证证据

- 获取并核对 #351、#344 全文与评论，以及 PR #348 元数据和合入 diff。
- PR #348 的六个 required checks 均为 **SUCCESS**；无 review/comment 补充证据。
- `git diff 486667e^..486667e --check`：**通过**。
- 四份交叉规格的 Markdown 相对文件链接均存在。
- 独立复算 quota canonical facts digest 与 generation key：**通过**。
- 对比 #344 前后 storage §6.4：完整 attempt recipes 被替换为不含 effect identity 的简写 arm；storage §8.1 与 command §4 的引用未同步。

## 6. 验收判断

- 获取 issue #351 全文及 comments：**YES**
- 核销前轮剩余 2×P1：**完成**
- 前轮 P1-1：**未关闭**
- 前轮 P1-2：**关闭**
- 新增/遗留可执行 P1：**2**
- 仅产出 review、不修改规格：**YES**
- `report.md` 转 `active`：**NO**
- 允许按现稿开始 Report 纵向实现：**NO**
