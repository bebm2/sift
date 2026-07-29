FAIL

# config.md M5 增补字段第三次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/config.md`](../specs/config.md)
> 基线：[第二次定向复审](2026-07-29-m5-config-field-rereview-2-pi-gpt-5.6-sol.md) 的 P1-1…P1-4
> 声称关闭：#343 / PR #346（merge commit `b754dc5`，实现 commit `9fde57f`）
> 交叉核对：[`interrupt.md`](../specs/interrupt.md)、[`storage.md`](../specs/storage.md)、[`outbox.md`](../specs/outbox.md)、[`ledger.md`](../specs/ledger.md)

## 1. 结论

**FAIL（P1-1、P1-2、P1-4 已关闭；P1-3 未完全关闭）。** PR #346 已用稳定 `metric_identity=<interrupt_id>` 消除双 admission 的指标去重冲突；日历表现在逐行冻结有效 zone/summary time，并补齐午夜、gap/fold 与 quota day；critical 权威 SQL 也已改为整数毫秒严格边界。

剩余阻断集中在 batch vector：Channel 已进入唯一 identity、schema 与 payload，但第二次复审要求的“同一 occurrence 两个 Run 冻结同/不同 Channel”的 exact vectors 尚未落下。当前只有单 Channel 的占位 payload 和验收性文字，无法逐字节核销并发分批及恢复结果。因此 config 的 M5 增补仍不能通过；既有 M1–M4 `status: active` 基线不回退，本结论不表示 M5 已实现。

## 2. P1-1…P1-4 对账

| 项目 | 结论 | 核验结果 |
|---|---|---|
| P1-1 · Ledger/指标去重 identity | **已关闭** | `attention_admissions.metric_identity` 固定为 `<interrupt_id>`，initial 与 critical 两条合法 admission 共享 lineage；`ledger.md` 的 `AttentionDeliveryV1` 同步携带并按该字段去重，而真实 admission/delivery 继续独立审计。文档明确列出已送达 `quota_batched` 后的 admitted、fused（无单条 delivery）与重放计数结果，不再把 admission ID 冒充唯一指标身份。 |
| P1-2 · 日历 vectors | **已关闭** | 每行均显式给出 `day_timezone/daily_summary_at`；Shanghai 本地 `23:59:59.999/00:00:00.000` 两侧含 quota day 与同一 due；New York gap/fold 行含非默认摘要时刻及 quota day。独立重算所列 ISO instant、epoch 和 local wall-clock 均一致。 |
| P1-3 · batch Channel identity | **未完全关闭** | daily/critical ID、batch/member Channel snapshot、delivery ID 和 daily scope 均已贯通，outbox 示例也改为真实 `1785286800000`。但关闭条件还要求同一 occurrence 下两个 Run 冻结相同/不同 Channel 的 vectors；现稿只以验收文字声称同 Channel 合批、不同 Channel 分批，未给两组成员、最终 batch IDs、sealed payload 与 operation 的 exact JSON/bytes。故并发 identity 的规则清楚，但 vector 核销仍缺。 |
| P1-4 · critical 半开窗口 | **已关闭** | `storage.md` §9.3 的唯一权威谓词改为 `created_at_ms > now-window`，并明确 `window-1ms` 计入、`window/window+1ms` 排除；§6.3 的 due 重裁决/successor 语义与 config vectors 一致，全文未再发现相反的 `>= now-window` 权威谓词。 |

## 3. 剩余可执行 P1

### P1-3a：冻结双 Channel exact vectors

给出同一 zone/due occurrence 下至少两个 Run 的完整冻结输入及两组结果：

1. 相同 Channel：同一 batch ID、成员排序、member delivery IDs、唯一 operation 与 sealed payload；
2. 不同 Channel：两个带各自 `channel_id` 的 batch IDs、互不混合的成员、各自 delivery/operation 与两份 sealed payload；
3. 对上述结果至少覆盖并发 insert 重放，并明确 canonical JSON/UTF-8 bytes，而不是只列验收句或 `...` 占位符。

## 4. 已确认关闭且未回退

- initial/critical admission 可保留双审计身份，而北极星分子按 stable Interrupt lineage 恰计一次。
- 上海午夜两侧、New York DST gap/fold 的配置、quota day、epoch 与 wall-clock 可直接转为确定性测试。
- daily batch 不再含 batch 级 quota day，scope 使用 `<zone>:<due_at_ms>`，Channel 已进入 batch identity。
- critical 窗口的 SQL、半开生命周期及 due successor 恢复语义一致。
- CAS/回滚、nullable charge、global 优先与被淘汰的 `interrupt_*` batch 名称未回退。

## 5. 关闭检查清单

- [x] 获取并阅读 #349 全文、Agent 建议、范围与约束
- [x] 获取并核对 #343、评论、PR #346 合入状态及实际 diff
- [x] 逐项复核 P1-1…P1-4
- [x] 交叉核对 config/interrupt/storage/outbox/ledger
- [x] 独立换算 timezone/epoch vectors
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `FAIL`
- [ ] P1-1…P1-4 全部关闭（**NO：P1-3 exact vectors 尚未关闭**）
- [ ] `config.md` M5 增补通过

## 6. 验收判断

- P1-1：**YES**
- P1-2：**YES**
- P1-3：**NO（identity/schema 已闭合，exact vectors 未闭合）**
- P1-4：**YES**
- M5 增补字段第三次定向复审：**FAIL**
- `config.md` 既有 M1–M4 active 基线：**不回退**
- 允许仅凭当前 batch vectors 宣称完整字段门禁通过：**NO**
