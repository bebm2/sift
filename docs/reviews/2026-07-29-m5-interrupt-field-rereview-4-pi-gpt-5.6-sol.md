FAIL

# M5 interrupt.md 字段级第四次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[第三次定向复审](2026-07-29-m5-interrupt-field-rereview-3-pi-gpt-5.6-sol.md) 的 P1-R1a、P1-R3a
> 声称关闭：#355 / PR #357（merge commit `92ec811`，实现 commit `35afae6`）
> 当前交叉基线：PR #358 及冲突标记 hotfix #361 后的 `main`（`32c0d44`）
> 交叉核对：[`brain.md`](../specs/brain.md) §11.1–§11.2、[`config.md`](../specs/config.md) §3.9/§4、[`storage.md`](../specs/storage.md) §6.3–§6.6、[`outbox.md`](../specs/outbox.md) §10

## 1. 结论

**FAIL（P1-R1a、P1-R3a 均未关闭）。** PR #357 已把 `EscapeT4Text` 的展示结果改为不转义 `/`、转义全部 `>`，并补了双 Channel、排除、空批与响应丢失段落；这两处方向正确。当前稿也已由 #361 清除 #358 引入的冲突标记。

但所谓“合法 canonical T4 input”本身违反 `brain.md` 的输入排序约束和全局 canonical JSON 规则；batch fixture 又同时违反 canonical JSON 单一来源与 `outbox.md` 的 closed tagged-union payload，并仍以 prose delta 代替同 Channel 的完整 bytes。它们不能直接生成合法 fixture、稳定 digest 或可重放 operation。因此两个剩余 P1 都不能核销，`interrupt.md` 必须继续保持 `status: draft`；本报告不表示 M5 已实现。

## 2. P1 对账

| 项目 | 结论 | 证据 |
|---|---|---|
| P1-R1a · T4 exact vectors | **未关闭** | `brief_markdown` 的 `\<b\>风险\</b\>`、marker 和 `/sift reject` 已与 `EscapeT4Text` 对齐。但 input 的 `brief_fragments=["<b>风险</b>","<!-- sift-op:x -->","/sift reject"]` 不满足 `brain.md` §11.1 的 UTF-8 bytes 升序；合法顺序应为 `[/sift reject, <!-- sift-op:x -->, <b>风险</b>]`。此外 config §4 是 canonical JSON 唯一来源，要求对象 key 词典序；input 顶层 `run_id,attempt_no,interrupt`、output `headline,conclusion,...` 和 `options_json` 内的 `id,label,effect,risk` 都不是词典序，却被声明为 canonical bytes。实现若先作 closed/domain 校验会拒绝该 input，若作 canonicalization 又会得到与文档不同的 bytes。安全事件链接也只写“唯一变化为 links_json”，没有冻结该字段的最终 canonical bytes。 |
| P1-R3a · batch concurrency/replay exact vectors | **未关闭** | storage §6.6 声明对象键“按出现顺序”即 canonical，直接违反 storage §1 对 config §4 的引用及 config §4 的对象 key 词典序规则。两份所谓完整 payload 都缺少 outbox §10 closed `channel_publish.body` 必填 discriminator `"delivery_kind":"attention_batch"`，因此不能作为合法 operation payload。不同 Channel 虽给了两份 JSON，同 Channel 仍只要求在第一份 schema 上以 prose 加入 `i-b` 和改写 `rendered_text`，没有给最终完整 JSON bytes；单成员排除也未给 `excluded_at_ms` 的 exact 值及完整持久化结果。故 operation digest、同 Channel sealed payload 和 replay bytes 仍不唯一。 |

## 3. 剩余可执行 P1

### P1-R1b：使 T4 vector 本身可被接纳

1. 按 UTF-8 bytes 重排 `brief_fragments`，并同步完整 input；output 阅读顺序可继续由 `key_points` 独立冻结。
2. 按 config §4 对所有被称为 canonical 的对象键排序，或删除错误的 canonical bytes 声称并另给真正的 canonical bytes；input、output、`options_json` 必须采用同一规则。
3. 给出安全事件链接 accept/fallback 的最终 `links_json` canonical bytes，而不是“唯一变化”描述。

### P1-R3b：补合法且逐字节完整的 batch fixture

1. 所有 JSON 按 config §4 的对象 key 词典序重写，不能在 storage 另定义“出现顺序 canonical”。
2. 每份 operation payload 加入 `delivery_kind=attention_batch`，逐字段满足 outbox §10 closed arm。
3. 逐字节列出同 Channel 双成员 payload、单成员排除结果、空批结果及 replay 返回的 persisted batch/member/operation；不得以“使用同样 schema并加入成员”代替 bytes。

## 4. 已确认且未回退

- `EscapeT4Text` 现在不错误转义 `/`，并转义 `<`、`>`、`!` 与 `-`；展示文本本身重算一致。
- attempt `failure_review` 与 Report quota variant 的 canonical option 集已区分；#361 已移除意外 conflict markers。
- daily batch ID 已含 Channel，同一 occurrence 的不同 Channel 不再碰撞；不同 Channel 的 ID、delivery ID 和 operation key 方向一致。
- 排除、空批和 sealed 后响应丢失均已有收敛意图，且规格仍保持 `draft`。

## 5. 关闭检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #359 全文、Agent 建议、范围与约束
- [x] 获取并核对 #355、评论、PR #357 合入状态与实际 diff
- [x] 逐项复核 P1-R1a、P1-R3a
- [x] 交叉核对 interrupt/brain/config/storage/outbox
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `FAIL`
- [x] `docs/specs/interrupt.md` 保持 `draft`
- [ ] P1-R1a 关闭（**NO**）
- [ ] P1-R3a 关闭（**NO**）
- [ ] interrupt 第四次定向复审通过（**NO**）
