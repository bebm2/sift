FAIL

# report.md 字段级定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`b08eae3`（#310 / PR #318）
> 评审对象：[`docs/specs/report.md`](../specs/report.md) draft
> 前轮结论：[`2026-07-29-m5-report-field-review-pi-gpt-5.6-sol.md`](2026-07-29-m5-report-field-review-pi-gpt-5.6-sol.md) 的 R1–R5
> 交叉核对：active [`control-plane.md`](../specs/control-plane.md)、[`config.md`](../specs/config.md)、[`storage.md`](../specs/storage.md)，以及 draft [`interrupt.md`](../specs/interrupt.md)、[`command.md`](../specs/command.md)

## 1. 结论

**FAIL（4×P1）。** PR #318 已关闭 R2 的 Run 配置快照来源，并补出了完整 Request v1、Report charge 候选身份、专用 generation domain、golden digest 和结局表；但 R1 的合法配置到整数 wire policy 仍无唯一换算，R3/R4 所需存储表与关联列没有进入 active storage，Report 对 attention 合批的收费描述与 storage 相反，且“Report Interrupt 不转 Run”尚未贯通 `EmitInterrupt` 事务和 Command 动作。

因此 [`report.md`](../specs/report.md) 必须保持 `status: draft`，本轮不得升为 `active`。本结论不表示 M5 已实现，也不回退已通过的 M4 门禁。

## 2. 前轮 P1 对账

| 前轮项 | 本轮判断 | 证据 |
|---|---|---|
| R1 retry wire contract | **部分关闭，仍有 P1-1** | Request v1、closed `retry_policy`、单调时钟与基础 vectors 已补；但 config 仍接受不能无损映射为整数毫秒/百万分倍率的合法值。 |
| R2 配置快照 | **关闭** | report §1.8 统一绑定 `runs.config_snapshot_id`；旧桶参数不一致 fail closed，验收覆盖旧 Run/新 Run/重启。 |
| R3 Report 子配额收费身份 | **未关闭，见 P1-2/P1-3** | report 写出了候选 row/key/link，但 active storage 没有对应列，且 attention quota-batched 分支不存在 attention charge entry。 |
| R4 quota-exhausted generation | **部分关闭，仍有 P1-2/P1-4** | 专用 domain、固定 facts、受控 link 与 golden vector 正确；权威表/唯一约束及可执行 Command binding 仍缺。 |
| R5 原子结果矩阵 | **未关闭，见 P1-3/P1-4** | report 自身已有有序表，但没有同步 storage 的写端口/事务配方，并与现有 admission/Run-transition 契约冲突。 |

## 3. 剩余可执行 P1

### P1-1 — 合法 retry 配置仍不能唯一编码到 wire

[`config.md` §3.6/§3.10](../specs/config.md) 允许 `runtime.retry_multiplier` 为范围内 JSON number，三个 `not_ready_*` 字段则是任意合法 Go duration 字符串；它们没有限制为至多六位小数或整数毫秒。report §4 / control-plane §5.2 却要求精确整数 `multiplier_micros` 和整数毫秒。因此 `retry_multiplier: 1.0000001`、`not_ready_initial_delay: 10.5ms` 都是现行 config 可接受值，却没有唯一合法 response。实现只能自行选择拒绝、floor、round 或 ceil。

**关闭条件：** 在 config + report + control-plane 冻结唯一规则：要么 schema 直接限制 multiplier 可精确表示为 micros、duration 可精确表示为毫秒；要么规定无歧义的十进制定点量化/舍入和越界拒绝。同步冻结 CLI 对 `initial <= max <= total`、额外字段及整数计算溢出的 fail-closed 行为，并加入上述小数值和倍率为 `1` 的长序列 vectors。

### P1-2 — R3/R4 声明的持久身份在 active storage 中不存在

report §5.1/§6.2 使用 `report_receipts.direct_interrupt_id`、`report_interrupt_charge_entry_id` 和 `report_quota_exhaustions.security_event_id`；但 active storage §7.4 的 receipt 没有前两列，全文没有 `report_quota_exhaustions` 表。report 所称“完整” budget row 还含 `bucket_end_ms`，storage §9.3 的 `budget_entries` 没有该列。interrupt §5.1 指向不存在的 storage §12.2.1，并声称一个尚未由权威存储规格定义的主键。故当前 schema 既插不下该 row，也不能用数据库约束证明 `(run, day)` 并发至多一次。

**关闭条件：** 同步 active storage：冻结 `report_receipts` 的 nullable/unique FK 关联、Report charge 可实际插入的精确 `budget_entries`/counter 形状、`report_quota_exhaustions` 全表与 `(run_id,daily_bucket_start_ms)` 主键、安全 event FK、append-only trigger/索引和 `RecordReport` 写入归属；若 `bucket_end_ms` 只属于 counter，则从所谓 entry row 删除并明确以 counter FK/identity 取得。把 interrupt 引用改到真实小节，并给 DDL 级 FK/UNIQUE、同 operation key、崩溃重放和四并发触顶断言。

### P1-3 — attention quota-batched 分支被错误写成必有第二笔 charge

report §5.1 称 attention 合批后“两个 entry”仍保留，§6.2 要求 `direct_interrupt_id.charged_budget_entry_id` 指向唯一 attention entry，§7 两行又都要求“一笔 attention entry”。active storage §6.1/§6.3 明确规定 `quota_batched` Interrupt 的 `charged_budget_entry_id=NULL`、`attention_charge_entry_id=NULL`，不能以零额或虚构 entry 充数。`attention_admission` 会保留，但 attention budget entry 不会存在。两个实现按两份规格会分别伪造收费或留下 nullable 关联。

**关闭条件：** 以 storage 的 admission/charge 分离为单一模型同步 report：Report 子配额 entry 始终按成功直接致扰写入；attention admission 始终写入；attention charge 及两个 charge FK 只在实际收费时非空。逐行拆开 `quota_charged`、`quota_batched`、`critical_admitted`、`critical_fused`，冻结每行的 report charge、attention admission、nullable attention entry、Interrupt、batch member、forge outbox 和 delivery 状态，并同步崩溃 vectors。

### P1-4 — Report 的“不转 Run”尚未成为可执行事务与 Command 契约

report §1/§5/§7 与 interrupt §1 要求 `agent_blocked` 和 Report 专用 `failure_review` 保持 Run 原状态；active storage §12.2 仍把 `EmitInterrupt` 的第一步写成 `Run → waiting_human`，§11 的 `RecordReport` 也只列 token/receipt/event/“异常 Interrupt”，没有承接普通 `agent_blocked`、两层预算和 quota-exhaustion 分支。与此同时 command §4 的公共 `hold` 要求保持 `waiting_human`；Report quota `failure_review` 又固定 `attempt_no=null`，却没有 command 所强制的 `failure_review.retry_kind=gate_recheck|new_attempt` effect binding。对象虽可生成，`hold/retry` 无法按 closed Command 表执行。

**关闭条件：** 在 storage 定义 `RecordReport` 的真实事务配方及 Report-only `EmitInterrupt` no-transition 模式，逐行承接 report §7 的 token/receipt/event/两层预算/安全记录/Interrupt/outbox 提交或回滚；再在 report + interrupt + command + storage 冻结两类 Report Interrupt 的 immutable command-effect binding。明确 running Run 上 `hold` 保持什么状态，以及 quota `failure_review` 的 `retry` 究竟执行何种合法、幂等效果；若该异常不允许 retry，则必须用版本化 reason/options 契约移除该 option，不能留下永远被 Command 拒绝的 canonical option。

## 4. 已确认通过的改动

- 完整 Request v1 示例包含 protocol/client/request 字段；CLI 不读 `config.yaml`，只对 `not_ready` 重试。
- Report 参数统一读取 Run 创建时配置快照；旧令牌桶不会被 daemon 当前配置重置。
- Report quota charge 使用 receipt 派生 operation key，日桶采用冻结 IANA zone 的半开本地日并覆盖 DST 23/25 小时语义。
- Report quota `failure_review` 使用独立 domain/version，固定 facts 不吸收 Agent 文本；本轮复算 golden `failure_digest=bce8…d90`、generation key `8489…197`，与文稿一致。
- report §7 已选择“子配额满仍消费 rate token”，并区分领域回滚与独立安全诊断；方向明确，待 P1-2～P1-4 落到权威 storage/command 契约。

## 5. 验证证据

- 核对 PR #318 合入提交 `b08eae3` 的完整 diff；六个 required checks 均为 **SUCCESS**，PR 无 review/comment 补充证据。
- `git diff b08eae3^..b08eae3 --check`：**通过**。
- report/interrupt Markdown 相对文件链接存在；但 interrupt 的语义引用 `storage §12.2.1` 对应小节不存在。
- 以规范 typed-NUL preimage 独立复算 Report quota golden digest/key：**通过**。

## 6. 验收判断

- 定向复审 R1–R5：**完成**
- 前轮 R2：**关闭**
- 剩余可执行 P1：**4**
- `report.md` 转 `active`：**NO**
- 允许按现稿开始 Report 纵向实现：**NO**
