FAIL

# M5 #699 AdvanceInterrupt after #693 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#693 / PR #698，合入提交 `eb1a43b`（实现提交 `7f4cd81`）
> 评审基线：当前 `main` / 本 worktree `eb1a43b`
> 前次结论：[#687 FAIL](2026-07-30-m5-advance-interrupt-687-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3、§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#693 已关闭两个明确缺陷：`holdAdvance` 现在在同一事务排除 superseded collecting member；新增测试也真正通过 `AdvanceInterrupt` 覆盖升级后 summary `<`、`=`、`>` 新 expiry。可是 #687 明列的剩余完整 state/authority/accounting/reason 验收矩阵并未关闭，故 issue #699 的 Must verify 3 为 NO。**

本轮生产改动只有在 `holdAdvance` 中调用既有 `excludeStaleBatchMembersTx`；测试改动则新增 summary 三边界，并把旧 stale-daily-member 表扩展为 expire hold 与 max hold。既有 `TestAdvanceInterruptExpiryAndMaxOutcomeMatrix` 未扩展，仍然只断言 status、held reason 和 close reason；multi-escalation 测试也仍只断言 severity 与最终 held state。#687 要求逐格观察的 version/nonce/escalation/new expiry/delivery/next dispatch、member/authority、admission/charge、operation/replay，以及 allowed reason / `startup_stall` 的 on-expire/on-max 组合，只有个别路径被分散覆盖，尚未形成闭合矩阵。

本轮遵守禁止自修自审：只新增本报告，不修改实现、规格、测试或 WBS。

## 2. Must verify

### 2.1 `holdAdvance` 事务排除 superseded collecting member：**YES**

- `holdAdvance` 在 Interrupt version 更新后、`finishAdvance` 提交前调用 `excludeStaleBatchMembersTx`；任一步失败均由外层事务回滚。
- helper 只更新仍为 `collecting`、未排除且不再匹配当前 open Interrupt version/nonce authority 的 member，不会排除仍有效 authority。
- `TestAdvanceInterruptExcludesStaleDailyMembersAndCancelsEmptyBatch` 新增 `expire hold` 和 `max hold` 两格，断言 `excluded_at_ms` 与 advance 时刻相等；关闭并 reopen DB 后，旧空 batch 为 `cancelled` 且不创建 `channel_publish`。
- close 与 successful escalation 两格、repeated-fuse reopen/close 两格继续通过，未见回退。

### 2.2 升级后 summary `<` / `=` / `>` 新 expiry：**YES**

- 新 `TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries` 三格都先创建 Interrupt，再以 `AdvanceExpiry` 触发真实升级，不再以初发 `EmitInterrupt` 测试替代。
- `<` 格进入 `ready` 并冻结 next midnight；`=` 与 `>` 格均进入 `held/batch_after_expiry` 且 `next_dispatch_at_ms=NULL`，符合生产代码的严格 `< new expiry`。
- 三格均断言 version=2、nonce 轮换、escalation_count=1、new expiry，以及 admission/charge 不重复、无提前 channel operation。
- 合法 `<` 格的后续 frozen due、daily member version/nonce 与 authority 由既有 `TestAdvanceInterruptDispatchUsesFrozenSummaryDue` 覆盖。

### 2.3 #687 剩余 matrix gaps：**NO**

#693 没有完成 #687 最终裁决要求的完整矩阵：

1. `TestAdvanceInterruptExpiryAndMaxOutcomeMatrix` 的四格仍只检查 status/held/close reason；未逐格检查 version、nonce 是否应轮换、escalation count、expiry、delivery、next dispatch、admission/charge 与 operation。
2. `TestAdvanceInterruptEscalationCountsReuseDowngrade` 仍只检查两步 severity 和末步 held reason；没有逐步断言上述状态、authority/accounting 与 stale replay 零副作用。
3. summary 新三格补了状态与 accounting，但没有在各格核验 member/authority 或重复旧 CAS；有效 member/authority 只由另一条单一路径测试证明，`=`/`>` 的零 member/authority 也未在该 AdvanceInterrupt 三格中断言。
4. reason 维度仍是分散用例：普通 allowed reason 的四种 outcome 与 `startup_stall` 只覆盖“非法 auto-reject 配置在创建时拒绝”和合法 max-hold；没有形成 #687 要求的 allowed/prohibited reason × on-expire/on-max 闭合表。

这些是 issue #693 明写的 Must close 3，而不是可延期的波次二增强，因此即使生产主路径未发现新确定性错误，也不能以绿灯核销。

## 3. 回归验证

- `git diff ddb6b3e..eb1a43b --check`：**通过**。
- #693 未新增 migration；当前 migration 0001–0051 连续，0001 SHA-256 仍为 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go test ./internal/storage -run 'TestAdvanceInterrupt|TestEmitInterruptSummaryExpiryBoundaries|TestNextDailySummary' -count=10`：**通过**。
- `go test ./cmd/sift-advance-interrupt-repair -count=10`：**通过**。
- `go vet ./internal/storage ./internal/intake ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。

## 4. Issue #699 checklist

- [x] 获取并阅读 #699 全文、Must verify、references 与 constraints：**YES**
- [x] 获取并阅读 #699 comments：**YES（0 条）**
- [x] 对照 #693、#687 FAIL 与 WBS M5 §5.2/§5.3 复审：**YES**
- [x] `holdAdvance` 在同一事务排除 superseded collecting member：**YES**
- [x] 升级后 summary `<` / `=` / `>` 新 expiry：**YES**
- [x] required packages、定向重复测试、vet 与全仓测试：**YES**
- [x] 只写 `docs/reviews/`，未 push/MR/merge，未修改 WBS：**YES**
- [ ] #687 剩余 state/authority/accounting/reason matrix gaps 全部关闭：**NO**
- [ ] #639 的 5 个 P1 全部关闭：**NO（仍为 4/5）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] WBS 波次一 AdvanceInterrupt 项可勾选：**NO**
- [ ] 遗留 P1 为零：**NO（1）**

## 5. 最终裁决

**FAIL。** 下一实现应把 outcome × reason 矩阵做成逐格验收：对 on-expire/on-max 的 hold/auto-reject/escalate、允许 reason 与 `startup_stall`，逐格断言 status、held/close reason、count/version/nonce/expiry/delivery/due、member/authority、admission/charge、operation及 stale replay 零副作用；再由不同代理复审。当前不得勾 WBS，也不得据此开启波次二。
