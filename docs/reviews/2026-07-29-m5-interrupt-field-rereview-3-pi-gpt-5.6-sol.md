FAIL

# M5 interrupt.md 字段级第三次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审对象：[`docs/specs/interrupt.md`](../specs/interrupt.md)
> 基线：[第二次定向复审](2026-07-29-m5-interrupt-field-rereview-2-pi-gpt-5.6-sol.md) 的 P1-R1…P1-R3
> 声称关闭：#343 / PR #346（merge commit `b754dc5`，实现 commit `9fde57f`）
> 交叉核对：[`brain.md`](../specs/brain.md) §11、[`config.md`](../specs/config.md) §3.7.1/§3.9、[`storage.md`](../specs/storage.md) §6/§9/§15、[`outbox.md`](../specs/outbox.md) §10、[`ledger.md`](../specs/ledger.md) §2.4、[`command.md`](../specs/command.md) §4

## 1. 结论

**FAIL（P1-R1、P1-R3 未关闭；P1-R2 已关闭）。** PR #346 已统一 canonical recommended action 校验，删除独立 unsafe-fragment 接纳分支；manual timed hold 现有唯一可恢复扫描谓词、CAS 前态、CHECK 与索引；batch identity/schema/payload 也已加入 Channel 和 delivery identity。

但新增 T4 golden vector 与 `brain.md` 的唯一 `EscapeT4Text` 算法逐字节矛盾；batch 部分仍没有第二次复审要求的双 Channel 并发、排除、空批和响应丢失重放的 **exact payload vectors**。因此不能核销全部 P1。`interrupt.md` 必须继续保持 `status: draft`；本报告只复核字段规格，不表示 M5 已实现。

## 2. P1-R1…P1-R3 对账

| 项目 | 结论 | 证据 |
|---|---|---|
| P1-R1 · T4/fallback 接纳器 | **未关闭** | `interrupt.md` §3.6 已要求正常 T4 与 fallback action 命中 canonical option，并把 fragment 安全域委托给 `brain.md`，方向正确。但新增声称为最终 UTF-8 的 vector 对 `<b>风险</b>` 没有按 `brain.md` §11.2 转义全部 `>`，却转义了算法列表中不存在的 `/`（`\/sift reject`）；该结果不可能由权威 `EscapeT4Text` 产生。它也只给出 fragment/result 片段，没有冻结完整合法 T4 output object 与最终 persisted headline/brief/options JSON bytes。故 closed 接纳器仍有两个实现结果。 |
| P1-R2 · hold/dispatch 可恢复状态机 | **已关闭** | `storage.md` §6.1 删除了重复 `hold_max_duration_ms`，冻结 expiry/dispatch 两个唯一谓词，明确 `held/manual` 可扫描而其他 held/probe 排除，并给出 `(open,held,manual,version,nonce)` CAS 前态和到期后唯一 expiry 配方；dispatch/held 成对约束、正毫秒约束以及 expiry/next-dispatch 索引均已补齐。`interrupt.md` §8.2 与 Command hold 时钟一致，重启与旧 tick 仍由同一 CAS 收敛。 |
| P1-R3 · Channel/delivery batch authority | **未关闭** | identity 已统一为 `daily:<zone>:<due>:<channel>` / `critical:...:<channel>`；batch/member schema、sealed payload、单条与 batch delivery ID、daily scope 的真实非零 epoch 也已贯通。可是第二次复审的关闭条件还要求两个不同 Channel 同 zone/due 并发入批，以及 version/nonce 排除、空批、seal 后响应丢失重放的 **exact payload vectors**。当前只有一个带占位符的 `ops-slack` payload 示例和验收性文字，没有双 Channel 的两份最终 payload bytes，也没有其余三条路径的 exact payload/result。唯一协议的字段方向已闭合，但可逐字节实现/回放的核销证据仍不完整。 |

## 3. 剩余可执行 P1

### P1-R1a：修正并补全 T4 exact vectors

1. 以 `brain.md` §11.2 的唯一 `EscapeT4Text` 列表重算 vector；`>` 与 `<` 都按同一算法处理，不得额外转义 `/`。
2. 给出完整 closed T4 input/output JSON，以及最终 persisted `headline`、`brief_markdown`、`options_json` 的 canonical JSON/UTF-8 bytes。
3. 对 unknown fragment、option 重排、非 canonical recommended action 与安全事件链接分别给出唯一 fallback/accept 的最终 bytes，而不只写结果名称。

### P1-R3a：补 batch exact concurrency/replay vectors

至少冻结同一 zone/due 下两个不同 Channel 的 batch/member IDs 与两份最终 sealed payload；另给出 member version/nonce 失配排除、排除后空批 `cancelled`、sealed 后响应丢失重放的 persisted batch/member/operation/payload 逐字段结果。占位符示例和验收描述不能替代 exact vectors。

## 4. 已确认关闭且未回退

- fallback 与正常 T4 的推荐动作均须命中 reason 的 canonical option；unsafe fragment 不再形成与 Brain 平行的拒绝域。
- manual hold 到期可由 supervisor 在重启后恢复；automatic held/probe 不重复命中。
- daily/critical batch identity 已包含 Channel，batch/member 均冻结 Channel snapshot 与 delivery ID；daily scope 已统一为 `<zone>:<due_at_ms>`。
- critical 半开窗口、metric lineage、nullable charge 与 M3 首发/generation/startup-stall 基线未被 #346 回退。

## 5. 关闭检查清单

- [x] 获取并阅读 #349 全文、Agent 建议、范围与约束
- [x] 获取并核对 #343、评论、PR #346 合入状态及实际 diff
- [x] 逐项复核 P1-R1…P1-R3
- [x] 交叉核对 interrupt/brain/config/storage/outbox/ledger/command
- [x] 只新增评审报告，未修改规格或实现
- [x] 报告首行为 `FAIL`
- [x] `docs/specs/interrupt.md` 保持 `draft`
- [ ] P1-R1…P1-R3 全部关闭（**NO：P1-R1、P1-R3 仍未关闭**）
