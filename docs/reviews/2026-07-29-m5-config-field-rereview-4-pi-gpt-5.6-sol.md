FAIL

# config.md M5 增补字段第四次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/config.md`](../specs/config.md)
> 基线：[第三次定向复审](2026-07-29-m5-config-field-rereview-3-pi-gpt-5.6-sol.md) 的 P1-3a
> 声称关闭：#355 / PR #357（merge commit `92ec811`，实现 commit `35afae6`）
> 当前交叉基线：`main` `32c0d44`
> 交叉核对：[`interrupt.md`](../specs/interrupt.md)、[`storage.md`](../specs/storage.md) §6.3–§6.6、[`outbox.md`](../specs/outbox.md) §10

## 1. 结论

**FAIL（P1-3a 未关闭）。** PR #357 已把 config/interrupt/outbox 统一指向 storage §6.6，并给不同 Channel 的稳定 batch、delivery、operation 身份以及排除、空批、重放路径；此前已关闭的 P1-1、P1-2、P1-4 未见回退。

但是共享 fixture 不是合法的 canonical `attention_batch` payload：它与 config §4 的 canonical JSON 单一规则冲突，缺 outbox closed union 的必填 discriminator，并且同 Channel 路径仍没有完整 sealed payload bytes。故同一 occurrence 的并发分批规则虽已清楚，尚不能逐字节实现和核销；`config.md` 的 M5 增补仍不通过。既有 M1–M4 `status: active` 基线不回退，本结论不表示 M5 已实现。

## 2. P1-3a 对账

| 关闭条件 | 结论 | 核验结果 |
|---|---|---|
| 相同 Channel：同一 batch、排序成员、delivery、operation、sealed payload | **NO** | ID、成员顺序、两个 member delivery ID、operation key 已列出；但 sealed payload 只以“使用第一份 schema，加入 `i-b` 并改写 `rendered_text`”描述，没有一份完整 canonical JSON bytes，仍需实现者推导。 |
| 不同 Channel：两批及两份 payload | **NO** | 两个 batch/delivery/operation identity 已分离，两份 JSON 的成员也未混合；但两份 JSON 都缺 outbox §10 closed arm 必填的 `delivery_kind=attention_batch`，不是合法 operation payload。 |
| 并发 insert 重放及 canonical bytes | **NO** | 文本规定竞争的唯一结果和 sealed 后复用 key/payload，方向正确；但 storage §6.6 把“对象键按出现顺序”称为 canonical，违反 config §4 的对象 key 词典序唯一规则。示例 `batch_id,delivery_id,batch_kind,...` 与空批对象也均非词典序，故 payload digest 与持久化 bytes 不可按共享规范复算。单成员排除还没有 exact `excluded_at_ms` 和完整 persisted result。 |

## 3. 剩余可执行 P1

### P1-3b：把共享 batch fixture 改成合法 exact vectors

1. 以 config §4 为唯一 canonical JSON 规则，按词典序重写所有对象键，删除 storage 中相反的“出现顺序”定义。
2. 两个不同 Channel 及同 Channel 双成员的完整 payload 都必须包含 `delivery_kind=attention_batch`，并满足 outbox §10 closed schema；不要用 prose delta 代替同 Channel bytes。
3. 为并发重放、单成员 version/nonce 排除、全排除 cancelled、sealed 后响应丢失分别列出最终 batch/member/operation/payload bytes；固定相关时间值并使 digest 可独立复算。

## 4. 已确认关闭且未回退

- P1-1：stable Interrupt metric lineage 仍保留。
- P1-2：Shanghai 午夜与 New York gap/fold 的 zone/time/quota day vectors 未被 #357 改写。
- P1-4：critical 严格毫秒边界与 successor 语义未回退。
- Channel 已进入 daily/critical batch identity，daily scope 仍为 `<zone>:<due_at_ms>`。
- config 只引用共享 fixture，没有复制第二套 batch 事实。

## 5. 关闭检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #359 全文、Agent 建议、范围与约束
- [x] 获取并核对 #355、评论、PR #357 合入状态与实际 diff
- [x] 复核第三次报告遗留 P1-3a
- [x] 交叉核对 config/interrupt/storage/outbox
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `FAIL`
- [x] `config.md` 既有 M1–M4 active 基线不回退
- [ ] P1-3a 关闭（**NO**）
- [ ] config M5 增补第四次定向复审通过（**NO**）

## 6. 验收判断

- P1-1：**YES（未回退）**
- P1-2：**YES（未回退）**
- P1-3：**NO（identity 已闭合，合法 exact vectors 未闭合）**
- P1-4：**YES（未回退）**
- M5 增补字段第四次定向复审：**FAIL**
- 允许仅凭当前共享 fixture 宣称完整字段门禁通过：**NO**
