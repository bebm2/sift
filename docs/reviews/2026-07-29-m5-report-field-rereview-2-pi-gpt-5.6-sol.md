FAIL

# report.md 字段级第二次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`3343598`（#330 / PR #334；内容提交 `34dd21b`）
> 评审对象：[`docs/specs/report.md`](../specs/report.md) draft
> 前轮结论：[`2026-07-29-m5-report-field-rereview-pi-gpt-5.6-sol.md`](2026-07-29-m5-report-field-rereview-pi-gpt-5.6-sol.md) 的 P1-1…P1-4
> 交叉核对：active [`control-plane.md`](../specs/control-plane.md)、[`config.md`](../specs/config.md)、[`storage.md`](../specs/storage.md)，以及 draft [`interrupt.md`](../specs/interrupt.md)、[`command.md`](../specs/command.md)

## 1. 结论

**FAIL（2×P1）。** PR #334 已关闭 retry 配置到 wire 的唯一编码、补齐 Report 收费与 quota-exhaustion 的存储身份，并把 attention admission/charge 分离同步进逐行结局；但 Report quota `failure_review` 仍没有一份可由 storage/command 表示的唯一 effect binding，且 quota-exhaustion 在 `EmitInterrupt` 结构拒绝时究竟保留还是整笔回滚仍有相反契约。

因此 [`report.md`](../specs/report.md) 必须继续保持 `status: draft`，本轮不得升为 `active`，也不得按现稿开始 Report 纵向实现。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| P1-1 retry 配置到 wire | **关闭** | config 已拒绝 exponent、超过六位小数、非整数毫秒与溢出；report/control-plane 冻结精确 millionths/milliseconds、closed response、整数公式及 multiplier=1 长序列。 |
| P1-2 持久身份与权威存储 | **关闭** | storage §7.4 增加两个 nullable UNIQUE FK，§7.5 定义 quota-exhaustion 表、复合主键、安全 event FK 与 append-only 要求，§12.2.1 明确 `RecordReport` 归属；entry 不再伪造 `bucket_end_ms`。 |
| P1-3 quota-batched 收费模型 | **关闭** | report §5–§7 与 storage §6.3/§12.2.1 均区分 Report charge、attention admission 与实际 attention charge；`quota_batched`/`critical_fused` 的 attention FK 为 NULL。 |
| P1-4 no-transition 与 Command | **未关闭，见 P1-1/P1-2** | no-transition 模式已进入 storage/interrupt，但 quota `failure_review` 的 facts、binding、hold 和失败事务仍互相冲突。 |

## 3. 剩余可执行 P1

### P1-1 — Report quota `failure_review` 仍不是唯一、可存储、可执行的 Command 对象

当前交叉契约同时存在四个不相容定义：

1. report §6.2 固定 `recommended_action=review_report_interrupt_quota`，interrupt §5.1 的 canonical facts 与调用参数却固定为 `recommended_action=hold`；二者会生成不同 `failure_digest`，前者也不满足 interrupt 所称的 canonical option。
2. interrupt §5.1、report §7 和 command §4 仍只说在 `gate_recheck|new_attempt|移除 retry` 中择一，没有为 v1 选择一个结果。实现无法确定 canonical options、binding JSON 或 `retry` 的事务效果。
3. active storage §6.4 的 closed arm 只有 `failure_review(run_id,attempt_no,generation,retry_kind=...)`；Report quota 对象固定 `attempt_no=NULL`，其 generation identity 也不含 attempt generation。该 arm 既无法表达此对象，也没有 command 新增文字所要求的 exact-head operation 或 new-attempt recipe 字段。
4. command §4 的公共规则仍规定“`hold` always retains `waiting_human`”，report §7 与 interrupt §6 却规定 running Report Interrupt 的 hold 保持 Run `running`。表中 `agent_blocked`/`failure_review` 仍只引用这个 common hold，没有 reason/source-specific 分支。

这不是措辞注记：按 storage closed schema 写入 quota binding 会因 NULL/缺 generation 拒绝；绕过它则违反一对一 immutable binding；即使对象写成，`retry` 与 `hold` 也没有唯一合法效果。

**关闭条件：** 为 Report quota v1 明确选择一种 canonical 契约。若保留 retry，冻结唯一 `retry_kind`、所需 exact-head/新 attempt 身份、可表达 nullable attempt 的独立 storage arm、FK/CHECK 与完整 Command 事务；若移除 retry，同步 versioned options、interrupt canonical JSON、command 表和 storage arm。统一 `recommended_action`，重算 digest/generation golden，并在 command 明写 no-transition Report Interrupt 的 hold 分支及 Run/version CAS。加入 NULL attempt binding、retry/hold、重复 command 与 stale nonce vectors。

### P1-2 — quota-exhaustion 的结构拒发原子结果仍相反

report §6.2/§7 规定：子配额满先消费 rate token并提交 `report_quota_exhaustions`/security event；若专用 `failure_review` 被 `EmitInterrupt` 结构拒发，只保留该安全记录，不留下 Interrupt。storage §12.2.1 第 5 步把 exhaustion 与专用 Interrupt 放在单一 `BEGIN IMMEDIATE`，第 6 步又规定任一 `EmitInterrupt` 结构拒绝会回滚 rate CAS **和 exhaustion**，仅另交拒绝诊断。

因此同一次请求按 report 会“token 已消费 + 当日 exhaustion 身份已占用”，按 storage 会“token 未消费 + exhaustion 主键未占用”。后续重试可能再次尝试生成诊断，四并发触顶也无法得到跨实现一致的唯一安全事实。

**关闭条件：** 在 report + storage 选择一个有序结果，并明确事务拆分点。若保留 exhaustion，先以独立可重放事务线性化安全事实，再以 generation key best-effort 创建 Interrupt，冻结失败响应和 token 结局；若要求全回滚，则 report 删除“保留安全记录/已消费 token”的相反描述。对 publish target 缺失、binding 结构拒绝、attention 存储错误逐项给出 crash/replay/四并发断言。

## 4. 已确认通过的改动

- `runtime.retry_multiplier` 现只能精确编码为 millionths；三个 `not_ready_*` duration 只能精确编码为整数毫秒。CLI/daemon 对额外字段、顺序关系与整数溢出 fail closed。
- `report_receipts.direct_interrupt_id`、`report_interrupt_charge_entry_id` 及 `report_quota_exhaustions` 已进入 active storage；Report entry 的桶尾只从 counter 取得。
- Report charge 与 attention charge 已拆开；attention 合批/熔断不再伪造零额 entry。
- storage §12.2 已定义 Report-only `EmitInterrupt` no-transition 模式，并要求 Run status/version 不变；§12.2.1 已给出 `RecordReport` 主配方。
- 以 interrupt §5.1 当前 `recommended_action=hold` 的 canonical JSON 独立复算：`failure_digest=59da82e3…6509`、generation key `cf9ab880…a083`，与 interrupt 文稿一致；这也反证 report 中另一 recommended action 未同步。

## 5. 验证证据

- 获取并核对 #338 全文、#330 全文与结案评论，以及 PR #334 元数据和合入 diff。
- PR #334 的六个 required checks 均为 **SUCCESS**；无 review/comment 补充证据。
- `git diff 3343598^..3343598 --check`：**通过**。
- 六份交叉规格的 Markdown 相对文件链接均存在。
- 独立复算 Report quota golden digest/generation key：**通过**（仅对 interrupt 当前的 `hold` facts）。

## 6. 验收判断

- 对照前轮 P1-1…P1-4：**完成**
- config/control-plane/storage/interrupt/command 交叉核对：**完成**
- 前轮 P1-1/P1-2/P1-3：**关闭**
- 前轮 P1-4：**未关闭**
- 剩余可执行 P1：**2**
- 仅产出 review、不修改规格：**YES**
- `report.md` 转 `active`：**NO**
- 允许按现稿开始 Report 纵向实现：**NO**
