FAIL

# M5 interrupt.md 字段级第五次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[第四次定向复审](2026-07-29-m5-interrupt-field-rereview-4-pi-gpt-5.6-sol.md) 的 P1-R1b、P1-R3b
> 声称关闭：#364 / PR #366（merge commit `f93a728`，实现 commit `7026e35`）
> 当前基线：`main` `ca8b4e4`
> 交叉核对：[`brain.md`](../specs/brain.md) §11.1–§11.2、[`config.md`](../specs/config.md) §4、[`storage.md`](../specs/storage.md) §6.6、[`outbox.md`](../specs/outbox.md) §10

## 1. 结论

**FAIL（P1-R3b 已关闭，P1-R1b 仍未关闭）。** PR #366 已正确修复 attempt T4 vector：`brief_fragments` 按 UTF-8 bytes 升序，input/output/`options_json` 的对象 key 均为词典序，安全事件 `links_json` 也冻结了最终 canonical bytes。storage §6.6 的 batch vectors 同样已改为合法、完整、可复算的 canonical JSON。

但同一 §3.6 明称“均为 canonical JSON bytes”的 Report quota v1 T4 input/output 仍使用非词典序对象 key。input 顶层仍为 `run_id,attempt_no,interrupt`，其 `interrupt` 和 `candidate_options[]` 也未排序；output 仍为 `headline,conclusion,key_points,recommended_option_id,options`。这直接违反 config §4 的唯一 canonical JSON 规则。故 canonical T4 exact-vector 遗留并未全数关闭，`interrupt.md` 必须继续保持 `status: draft`；本报告不表示 M5 已实现。

## 2. P1 对账

| 项目 | 结论 | 证据 |
|---|---|---|
| P1-R1b · T4 vector 可接纳且 canonical | **未关闭** | attempt input 的 `brief_fragments=["/sift reject","<!-- sift-op:x -->","<b>风险</b>"]` 与 quota input 的 `["请人工处理","额度已耗尽"]` 均按 UTF-8 bytes 升序；attempt input/output、persisted/fallback `options_json` 和安全事件 `links_json` 的 key 也已正确排序。但 quota input/output 仍被明确称作 canonical bytes，却在顶层、嵌套 `interrupt` 及 option 对象中违反对象 key 词典序。 |
| P1-R3b · batch concurrency/replay exact vectors | **已关闭** | storage §6.6 统一引用 config §4；同 Channel、不同 Channel、单成员排除与 replay 的每份非空 payload 都含 `delivery_kind=attention_batch`。同 Channel双成员、双 Channel、排除、空批及 replay 均给出完整最终 JSON，而非 prose delta；所有对象层 key 经逐层检查均为词典序。三份声明 digest 的 payload 重算分别得到 `2e2c6a…bfc`、`c894af…f10`、`db6d98…e96`，与文档一致。 |

## 3. 剩余可执行 P1

### P1-R1c：统一修复 quota T4 canonical bytes

按 config §4 重写 interrupt §3.6 的 Report quota v1 合法 input/output：

1. input 顶层按 `attempt_no,interrupt,run_id` 排序；
2. `interrupt` 每层对象及 `candidate_options[]` 按 UTF-8 key 词典序排序；
3. output 按 `conclusion,headline,key_points,options,recommended_option_id` 排序；
4. 保留当前已经正确的 `brief_fragments=["请人工处理","额度已耗尽"]` 及领域字段值，不用 prose 声明替代最终 bytes。

## 4. 已确认且未回退

- attempt T4 的 `brief_fragments`、input/output、`options_json` 与安全事件 `links_json` 已满足本轮硬约束。
- batch payload 的 `delivery_kind`、逐层 canonical key、完整最终 bytes 和 digest 均已闭合。
- `interrupt.md` 仍为 `draft`。

## 5. 关闭检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #368 全文、Agent 建议、范围与硬约束
- [x] 获取并核对 #364、评论、PR #366 合入状态与实际 diff
- [x] 逐项复核 P1-R1b、P1-R3b
- [x] 硬查对象 key 词典序与 `brief_fragments` UTF-8 bytes 顺序
- [x] 硬查 `delivery_kind=attention_batch`
- [x] 硬查同/不同 Channel、排除、空批、重放的完整最终 JSON bytes
- [x] 重算 storage §6.6 声明的 payload digests
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `FAIL`
- [ ] P1-R1b 关闭（**NO**）
- [x] P1-R3b 关闭（**YES**）
- [ ] interrupt 第五次定向复审通过（**NO**）
