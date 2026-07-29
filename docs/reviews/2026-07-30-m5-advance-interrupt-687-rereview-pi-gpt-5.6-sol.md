FAIL

# M5 #687 AdvanceInterrupt after #681 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 评审对象：#681 / PR #686，合入提交 `ddb6b3e`（实现提交 `efc32ad`）
> 评审基线：当前 `main` / 本 worktree `ddb6b3e`
> 前次结论：[#675 FAIL](2026-07-30-m5-advance-interrupt-675-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3、§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**FAIL。#681 补上了 close/escalate 后 collecting member 排除、reopen 后 empty cancellation，以及 repeated-fuse reopen/close 的定向覆盖，但没有真正关闭 #675 要求的升级后 summary 三边界，且 `AdvanceInterrupt` 的 hold 分支仍违反 collecting member 必须在 supersede 事务内标记 excluded 的契约；#639 的 5 个 P1 仍为 4/5。**

`TestEmitInterruptSummaryExpiryBoundaries` 测的是初发 `EmitInterrupt` 的 `BatchAtMS` `< / == / >` 初始 expiry，不调用 `AdvanceInterrupt`，因此不能替代 #675 明列的“升级后首个 summary 与新 expiry”矩阵，也没有逐格验证升级后的 escalation count、version、nonce、new expiry、delivery、next dispatch、admission/charge。生产升级代码虽以严格 `< new expiry` 计算 due，已有 frozen-due 测试也未回退，但本轮关闭条件要求的是该 AdvanceInterrupt 验收矩阵，不能由另一端口的初发测试核销。

另外，新增 `excludeStaleBatchMembersTx` 只接在 escalate、auto-reject close 和 startup-stall 外部关闭路径。`holdAdvance` 会把 Interrupt version 加一，使旧 collecting authority 立即失效，却直接 `finishAdvance`，没有在同一事务写 `excluded_at_ms`。这与 storage §6.3 “关闭或由事实 supersede 的事务在 batch 仍 collecting 时必须把成员标为 excluded”相悖。sealing 查询的 version/nonce 防线会阻止旧成员泄入 payload，并最终取消空批，因此当前缺口不是错误发送；但 durable exclusion 事实和规定事务边界仍不成立，且新增 close/version-change 测试只选了 auto-reject 与 successful escalate，恰好漏掉 on-expire/on-max hold。

本轮遵守“禁止自修自审”：评审者未参与 #681 实现，只新增本报告，不修改被评审代码、规格或测试。

## 2. #675 遗留矩阵核验

### 2.1 summary `< / == / >` 新 expiry：**NO**

- 新测试 `TestEmitInterruptSummaryExpiryBoundaries` 调用 `emitTestInterrupt`，以显式 `BatchAtMS` 检验初发边界；它不产生 expiry advance/escalation。
- 测试没有证明升级复用冻结的 `daily_summary_at` 重算“该次升级后首个 summary”，也未观察升级后的 version/nonce/escalation/new expiry。
- `<` 格只停在 `ready`，没有 dispatch 到 frozen due 后核验 daily batch ID/due/member authority；该事实仍由上一轮单一路径测试覆盖，不构成三格 AdvanceInterrupt 矩阵。
- `==` / `>` 格对初发 held 的断言正确，但不能覆盖 `internal/storage/advance_interrupt.go` 的 escalation 分支。

### 2.2 restart 后 daily/critical seal：**YES**

- daily close/escalate 两格在 Advance 后关闭并 reopen DB，再执行 `PrepareDueAttentionBatches`，断言唯一旧成员被排除、空批 `cancelled` 且无 `channel_publish`。
- repeated critical-fuse 的有效 current authority 用例现在在 seal 前 reopen，seal 后仍核验 payload 使用当前 version/nonce。
- repeated-fused close 用例在多次 nonce/version 轮换后 auto-reject，reopen 后断言 critical batch `cancelled` 且无 operation。

### 2.3 close/version-change/empty cancellation：**部分关闭，整体 NO**

- auto-reject close 与 successful escalate 会调用 `excludeStaleBatchMembersTx`，新增测试证明 `excluded_at_ms=NowMS`，reopen seal 后空批取消。
- `holdAdvance` 在 `on_expire=hold`、达到 max 后 hold、以及 startup-stall 禁止 auto-reject 而 hold 时都会 version+1，但未调用 exclusion helper。旧 member 虽被 sealing 的 authority predicate安全过滤，仍没有按契约在 supersede 事务内标为 excluded。
- 因此“关闭或 version-change 排除”没有覆盖 AdvanceInterrupt 的完整终态分支。

### 2.4 repeated-fuse restart/close：**YES**

- current repeated-fuse authority 在 reopen 后 seal，payload 继续使用最新 version/nonce。
- repeated-fused member 在后续 close 事务中标记 excluded，reopen 后空批 cancelled，未创建 channel operation。

### 2.5 完整 outcome / authority / accounting 矩阵：**NO**

#681 没有扩展既有 outcome table；仍未逐格核验 escalation count、version、nonce、expires、delivery、next dispatch、admission/charge/outbox 不重复，也没有把允许 reason 与 `startup_stall` 对 on-expire/on-max 的 hold/auto-reject 组合闭合。新增测试覆盖的是本轮几个排除场景，不能替代 #663/#675 连续要求的完整状态矩阵。

## 3. migration 与回归

- #681 **未新增 migration**；当前 migration 为 0001–0050 连续、唯一，符合“如需要从 0051+ 起”。
- 已发布 0001 SHA-256 仍为 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- `git diff f63bc2e..ddb6b3e --check`：**通过**。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go test ./internal/storage -run 'TestAdvanceInterrupt|TestEmitInterruptSummaryExpiryBoundaries|TestNextDailySummary' -count=10`：**通过**。
- `go test ./cmd/sift-advance-interrupt-repair -count=10`：**通过**。
- `go vet ./internal/storage ./internal/intake ./cmd/sift-advance-interrupt-repair ./cmd/siftd`：**通过**。
- `go test ./...`：**通过**。

## 4. Issue #687 验收清单

- [x] 从检测到的 GitHub forge 获取并阅读 #687 全文、Agent 建议、关闭条件与约束：**YES**
- [x] 获取并阅读 #687 comments：**YES（0 条）**
- [x] 对照 #675 FAIL / #681 核验 5×P1：**YES**
- [x] frozen daily-summary due 修复未回退：**YES**
- [x] reopen 后 daily/critical sealing 定向覆盖：**YES**
- [x] auto-reject close / successful escalation / empty cancellation 覆盖：**YES**
- [x] repeated-fuse reopen/close 排除覆盖：**YES**
- [x] 无新 migration，0001 未漂移：**YES**
- [x] 规定四包测试、定向重复测试、vet、全仓测试：**YES**
- [x] 结论写入 `docs/reviews/`，且只在当前 worktree 操作：**YES**
- [x] 禁止自修自审；本轮只新增评审报告：**YES**
- [ ] 升级后 summary 早于/等于/晚于新 expiry 三边界完整：**NO**
- [ ] hold supersede 在同一事务标记 collecting member excluded：**NO**
- [ ] 完整状态/authority/accounting/reason 矩阵：**NO**
- [ ] #639 的 5 个 P1 全部关闭：**NO（4/5）**
- [ ] AdvanceInterrupt / wave1 I4 可核销：**NO**
- [ ] WBS 波次一 Adv 项可勾选：**NO（未修改 WBS）**
- [ ] 遗留 P1 为零：**NO（1）**

## 5. 最终裁决

**FAIL。** 下一实现至少须让 `holdAdvance` 在同一事务排除 collecting stale members，并以真正调用 `AdvanceInterrupt` 的 low/normal escalation 测试覆盖 summary `< / == / >` 新 expiry；同时逐格断言 count/version/nonce/new expiry/delivery/next dispatch、member/authority、admission/charge 与 operation，并闭合 on-expire/on-max × allowed reason/startup-stall。之后再由不同代理复审；本轨未 PASS，不勾 WBS，也不开波次二。
