PASS

# config.md M5 增补字段第五次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/config.md`](../specs/config.md)
> 基线：[第四次定向复审](2026-07-29-m5-config-field-rereview-4-pi-gpt-5.6-sol.md) 的 P1-3b
> 声称关闭：#364 / PR #366（merge commit `f93a728`，实现 commit `7026e35`）
> 当前基线：`main` `ca8b4e4`
> 交叉核对：[`interrupt.md`](../specs/interrupt.md)、[`storage.md`](../specs/storage.md) §6.3–§6.6、[`outbox.md`](../specs/outbox.md) §10

## 1. 结论

**PASS（P1-3b 已关闭）。** PR #366 已把共享 batch fixture 统一到 config §4 的 canonical JSON 规则，并补齐同 Channel、不同 Channel、排除、空批与 sealed 后响应丢失重放的完整最终 JSON bytes。所有非空 `channel_publish.body` 都含 closed arm discriminator `delivery_kind=attention_batch`；没有再用 prose delta 代替最终 payload。

本结论仅核销 config 第四次定向复审遗留的 P1-3b。它不表示 M5 已实现或完整阶段门禁已通过；interrupt 第五次定向复审另有 quota T4 canonical-vector 遗留，不改变本报告对共享 batch/config P1-3b 的结论。

## 2. P1-3b 对账

| 关闭条件 | 结论 | 核验结果 |
|---|---|---|
| config §4 是唯一 canonical JSON 规则 | **YES** | storage §6.6 已删除“按出现顺序 canonical”的冲突定义，明确每一层对象 key 按 UTF-8 词典序；逐层解析检查该节全部 JSON，未发现乱序对象。 |
| 相同 Channel：完整双成员 payload 与最终投影 | **YES** | 完整 payload 含 `i-a,i-b`、各自 delivery identity、`delivery_kind=attention_batch` 及最终 rendered text；batch/member/operation 投影另以完整 canonical JSON 给出。 |
| 不同 Channel：两批、两 payload 与独立 identity | **YES** | `ops-slack` 与 `ops-teams` 各有完整 payload、不同 batch/delivery/operation key 与 digest，成员未混批，且两份 payload 均含 `delivery_kind=attention_batch`。 |
| 排除、空批、重放：完整最终 bytes | **YES** | 单成员排除固定 `excluded_at_ms=1785286800001` 并给 payload 与最终投影；全排除给 cancelled batch/member/`operation:null` 完整 bytes；响应丢失 replay 给原完整 payload及返回的 persisted identity bytes，无 prose delta。 |
| digest 可独立复算 | **YES** | 对 code block 的 payload UTF-8 bytes（不含 Markdown 换行）重算 SHA-256：双成员 `2e2c6af826d5b028e043204908e3aab88ad9f32babe89a4beb438337e4830bfc`、slack 单成员 `c894afd28c0c9cd20e4bdbd0c2e56df0675710188bb02f0bd3effe0f9bb5cf10`、teams 单成员 `db6d98f746eb3c6bb5af456841a5a914a91e16ddfe69a9e76fdb4e722557be96`，均一致。 |

## 3. 已确认关闭且未回退

- P1-1：stable Interrupt metric lineage 未回退。
- P1-2：timezone、午夜与 DST gap/fold vectors 未被 #366 改写。
- P1-3：同 occurrence 按冻结 Channel 的 batch identity 与 exact fixture 已闭合。
- P1-4：critical 严格毫秒边界与 successor 语义未回退。
- config 仍只引用 storage §6.6 的共享 fixture，没有复制第二套 batch bytes。

## 4. 关闭检查清单

- [x] 在检测到的 GitHub forge 获取并阅读 #368 全文、Agent 建议、范围与硬约束
- [x] 获取并核对 #364、评论、PR #366 合入状态与实际 diff
- [x] 复核第四次报告遗留 P1-3b
- [x] 硬查全部共享 fixture 的对象 key 词典序
- [x] 硬查每份非空 payload 的 `delivery_kind=attention_batch`
- [x] 硬查同/不同 Channel、排除、空批、重放的完整最终 JSON bytes
- [x] 重算并核对三个唯一 payload digest
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `PASS`
- [x] P1-3b 关闭（**YES**）
- [x] config M5 增补字段第五次定向复审通过（**YES**）

## 5. 验收判断

- P1-1：**YES（未回退）**
- P1-2：**YES（未回退）**
- P1-3：**YES**
- P1-4：**YES（未回退）**
- config M5 增补字段第五次定向复审：**PASS**
- 允许据此宣称 M5 已实现或完整阶段门禁通过：**NO**
