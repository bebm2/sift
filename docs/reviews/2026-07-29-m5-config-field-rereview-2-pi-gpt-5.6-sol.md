FAIL

# config.md M5 增补字段第二次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/config.md`](../specs/config.md) 当前稿（#328 / PR #331，commit `8e5ed9f`，merge `5a2fa3b`）
> 前轮结论：[`2026-07-29-m5-config-field-rereview-pi-gpt-5.6-sol.md`](2026-07-29-m5-config-field-rereview-pi-gpt-5.6-sol.md) 的 RMC1-A…RMC4-C
> 交叉核对：[`interrupt.md`](../specs/interrupt.md)、[`storage.md`](../specs/storage.md)、[`outbox.md`](../specs/outbox.md)、[`ledger.md`](../specs/ledger.md)

## 1. 结论

**FAIL（4×P1）。** PR #331 已补齐 CAS 并发/回滚 vectors，统一淘汰了 `interrupt_*` batch 分叉，并把 daily identity 改为同一 zone/scheduled occurrence 一个对象；`quota_batched → critical` 的 nullable charge 也已能落表。

但本轮仍不能核销全部门槛：升级后的指标去重身份与两条 admission 身份冲突；日历表的 DST 行没有声明其非默认摘要时刻，且没有午夜两侧 vector；batch identity 无法容纳同一 occurrence 下不同冻结 Channel，outbox 的 `scope_id` 示例也仍使用旧日键；critical 窗口的权威查询仍采用会在 expiry 毫秒计数的 `>=`。这些矛盾会让实现生成不同 schema/key、epoch 结果或边界判断。

因此不得把 `config.md` 的 M5 增补视为通过。文件现有 `status: active` 仍只代表既有 M1–M4 基线，本轮不回退该基线，也不表示 M5 已实现或通过。

## 2. RMC1-A…RMC4-C 对账

| 门槛 | 判断 | 核验结果 |
|---|---|---|
| RMC1-A | **YES** | config §3.9 已固定 `limit=2`、`limit=1` 的并发结局，并要求 CAS 重试耗尽、无法重读及 SQLite/事务故障将 Interrupt、admission、counter、entry、member、operation 全部回滚，且不得伪装为 `quota_batched`。 |
| RMC2-A | **部分关闭，仍有 P1-1** | nullable charge、admitted/fused 和重放三条路径已闭合；但 Ledger 声称升级不得再次计权，实际升级使用第二个 admission ID，无法以 admission ID 达成该去重。 |
| RMC3-A | **部分关闭，仍有 P1-2** | zone、输入 epoch、due epoch、严格晚于以及 gap/fold 文字已补；DST vectors 与默认配置不相容，且前轮要求的午夜两侧/完整 quota day 结果仍缺。 |
| RMC4-A | **部分关闭，仍有 P1-3** | 被淘汰的 `interrupt_batches`、`interrupt_batch_memberships`、`PrepareInterruptBatchDelivery`、`interrupt-batch:` 已在五份交叉规格中清零；但 attention batch 的 Channel identity 与 outbox scope 仍未形成唯一协议。 |
| RMC4-B | **YES（受 P1-3 阻断）** | quota day 已从 daily batch ID 移除，成员各自保留 quota day，前日摘要后与次日摘要前的成员被要求复用 `daily:<zone>:<due_at_ms>`。该规则本身关闭原拆双批问题，但在多 Channel 情形尚不可实现。 |
| RMC4-C | **NO，见 P1-4** | config/storage 的半开窗口与 due 恢复文字已补，但 storage §9.3 的权威 SQL 谓词仍保持闭边界。 |

## 3. 剩余可执行 P1

### P1-1 — 升级的 Ledger/指标去重身份不能同时成立（RMC2-A）

[`storage.md` §6.3](../specs/storage.md) 固定初发 admission key 为 `<interrupt_id>:initial`、首次 critical transition 为 `<interrupt_id>:critical`。因此一条已递送 daily summary、随后从 `quota_batched` 升级并成功 critical 递送的 Interrupt，会合法产生两个不同的 `attention_admission_id`。

[`ledger.md` §2.4](../specs/ledger.md) 又同时规定 `attention_admission_id` 是“唯一的指标去重身份”，且同一 member 后续升级不得再计一次。按 admission ID 去重会把上述两次 delivery 计为两次；按 Interrupt 去重才会计一次，但当前字段和规则没有冻结这种 lineage。nullable charge 已闭合，指标分母仍未闭合。

**关闭条件：** 在 storage/ledger/outbox 中冻结升级 lineage 与唯一 metric identity（明确它是 initial admission、Interrupt identity 或新增稳定字段），逐项给出 `quota_batched` 已递送后再 critical admitted、critical fused、重放的 delivery 行和计数结果；不得再同时声称两个 admission ID 与“按 admission ID 不重复计权”。

### P1-2 — 日历 vectors 与生效配置不相容，且午夜边界缺失（RMC3-A）

[`config.md` §3.9](../specs/config.md) 的默认 `daily_summary_at` 是 `09:00`。表中 New York spring-forward 行却期望 `06:59Z → 07:00Z`（本地 `01:59 → 03:00`），只有另设落入 gap 的摘要时刻才成立；fall-back 行期望第一次本地 `01:30`，也要求另一摘要配置。表没有给每组 vector 的 `daily_summary_at`，按当前默认值执行会得到不同 due。

两条 Shanghai 行分别是本地 `2026-07-28 16:59:59` 与 `2026-07-29 08:59:59`，用于证明同一 due 的双 quota day，但不是前轮要求的本地午夜两侧。DST 两行也没有冻结期望 `quota_day`。因此实现无法把表直接转换为完整的确定性测试。

**关闭条件：** 为每组 vector 明示完整生效输入（至少 `day_timezone` 与 `daily_summary_at`），补本地 `23:59:59.999/00:00:00.000` 两侧的 `quota_day/due_at_ms`，并为 gap/fold 行同时给出 quota day；独立重算所有 ISO instant、epoch 与 local wall-clock 后保持一致。

### P1-3 — daily batch 的唯一 identity 无法承载冻结 Channel（RMC4-A）

[`config.md` §3.7.1](../specs/config.md) 要求每个 attention batch 冻结 Channel snapshot；不同 Run/config snapshot 或 T6 选择可在同一 zone/due occurrence 得到不同 Channel。[`outbox.md` §10](../specs/outbox.md) 又要求一个 batch 的所有成员冻结 Channel identity 一致。但 [`storage.md` §6.3](../specs/storage.md) 的唯一 ID 只有 `daily:<zone>:<due_at_ms>`，没有 Channel 或可表示多个 Channel delivery 的子对象。两个合法成员若冻结不同 Channel，既不能加入同一个 batch，也不能创建第二个 batch。

同时 outbox 示例仍写 `scope_id="Asia/Shanghai:2026-07-29"`，而 storage 已将 daily scope ID 改为 `<zone>:<due_at_ms>`。旧 quota-day scope 与新 occurrence scope 仍有两个 wire 结果。

**关闭条件：** 选择并贯通一种协议：要么 occurrence 对象下按冻结 Channel 建立 versioned delivery 子对象，要么在不破坏“每日一次摘要”产品语义的前提下明确 Channel 维度身份；同步 config/interrupt/storage/outbox/ledger 的 batch、member、scope、operation 与 payload schema。将 outbox 示例改为真实非零 due epoch，并补同一 occurrence 两个 Run 冻结同/不同 Channel 的 vectors。

### P1-4 — critical expiry 的权威查询仍是闭边界（RMC4-C）

[`storage.md` §6.3](../specs/storage.md) 已声明 evidence 生命周期为半开区间 `[created_at_ms, created_at_ms + window)`，config vector 也要求 `now=t+window` 时不再计入；但 [`storage.md` §9.3](../specs/storage.md) 的权威计数仍是：

```text
created_at_ms >= now-window
```

在 `now=t+window` 时该谓词仍包含 `created_at_ms=t`，正好复现前轮指出的 1ms 自相矛盾。episode 到期重裁决会因选用 §6.3 或 §9.3 得到不同的 seal/successor 结果。

**关闭条件：** 将唯一权威查询改成与半开生命周期等价的严格边界（并明确整数毫秒比较），全文清除相反谓词；补 `window-1ms/window/window+1ms` 的实际计数、due 重裁决与 successor identity 结果。

## 4. 已确认关闭的部分

- CAS contention 不再等同额度耗尽；并发额度和整笔回滚已有可执行结果。
- `quota_batched → critical` 可保持 NULL charge，admitted/fused 均有唯一 admission，重放不补造 entry/member/operation。
- `interrupt_*` batch 表、状态、prepare port 和 operation key 分叉已清除，统一采用 `attention_*` 名称族。
- daily identity 不再按 batch 级 quota day 拆分同一 scheduled occurrence。
- critical episode 已写明 due 不原地修改、仍饱和时创建 successor、恢复扫描复用既有 batch/operation；仅权威窗口谓词尚未同步。

## 5. 关闭检查清单

- [x] 获取并阅读 #336 完整描述、Agent 建议与约束
- [x] 获取并核对 #328、评论、PR #331 与合入 diff
- [x] 逐项复核 RMC1-A…RMC4-C
- [x] 交叉核对 config/interrupt/storage/outbox/ledger
- [x] 独立换算文档中的 timezone/epoch vectors
- [x] 只新增评审报告，未修改规格或实现
- [ ] 全部 P1 关闭
- [ ] `config.md` M5 增补通过

## 6. 验收判断

- 遗留 P1：**4**
- RMC1-A：**YES**
- RMC2-A：**PARTIAL**
- RMC3-A：**PARTIAL**
- RMC4-A：**PARTIAL**
- RMC4-B：**YES（受 RMC4-A 阻断）**
- RMC4-C：**NO**
- M5 增补字段第二次定向复审：**FAIL**
- `config.md` M5 增补升 active：**NO**
- `config.md` 既有 M1–M4 active 基线：**不回退**
- 允许按当前跨文档契约实现 M5 配额/critical 熔断/汇总：**NO**
