PASS WITH NOTES

# M5 #711 AdvanceInterrupt after #705 定向复审

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro
> 评审对象：#705 / PR #710，合入提交 `871d719`（实现提交 `9095f91`）
> 评审基线：当前 `main` / 本 worktree `871d719`
> 前次结论：[#699 FAIL](2026-07-30-m5-advance-interrupt-699-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §8–§9](../specs/interrupt.md)、[`storage.md` §6.3、§6.6、§9.3](../specs/storage.md)、[`config.md` §3.9](../specs/config.md)

## 1. 结论

**PASS WITH NOTES。#705 关闭了 #699 全部四条明确矩阵缺口：`TestAdvanceInterruptExpiryAndMaxOutcomeMatrix` 与 `TestAdvanceInterruptEscalationCountsReuseDowngrade` 现均以完整 `advanceOutcome` 逐格断言 state/authority/accounting + `assertStaleReplayRejected`；新增 `TestAdvanceInterruptReasonOutcomeMatrix` 闭合了 allowed/prohibited reason × on-expire/on-max 表；holdAdvance exclusion 与 post-escalation expiry 边界仍绿。注记：`TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries` 三格未调用 `assertStaleReplayRejected`，但 stale CAS 已在其余矩阵测试中逐级覆盖，属非阻断。**

本轮遵守禁止自修自审：只新增本报告，不修改实现、规格、测试或 WBS。

## 2. Must verify

### 2.1 Outcome/reason matrix from #699 closed（state/authority/accounting/reason + stale CAS）：**YES**

#### 2.1.1 `TestAdvanceInterruptExpiryAndMaxOutcomeMatrix` 完整覆盖

#699 指出旧四格只检查 status/held/close reason。本轮扩展为完整 `advanceOutcome`：

| 格 | status | dispatchState | delivery | severity | held/close | version | escalation | nonce | nextDispatch | admissions/charges | channelOps | members/authority | staleCAS |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| expire hold | open | held | held | normal | expiry | 2 | 0 | 未轮换 | NULL | 1/1 | 1 | 0/0 | ✅ |
| expire auto reject | closed | batched | immediate | normal | expired_auto_reject | 2 | 0 | 未轮换 | NULL | 1/1 | 1 | 0/0 | ✅ |
| max hold | open | held | held | normal | max_escalations | 2 | 0 | 未轮换 | NULL | 1/1 | 1 | 0/0 | ✅ |
| max auto reject | closed | batched | immediate | normal | expired_auto_reject | 2 | 0 | 未轮换 | NULL | 1/1 | 1 | 0/0 | ✅ |

每格均调用 `assertStaleReplayRejected`，确认过期 CAS 重放不改变状态。

#### 2.1.2 `TestAdvanceInterruptEscalationCountsReuseDowngrade` 逐级覆盖

#699 指出旧测试只检查两步 severity 和末步 held reason。本轮改为三步 `advanceOutcome` 逐级断言：

| 步 | severity | delivery | escalation | nonce轮换 | due | admissions/charges | members/authority | staleCAS |
|---|---|---|---|---|---|---|---|---|
| 升级1 (downgraded normal→low) | low | batch | 1 | ✅ | next summary | 1/1 | 0/0 | ✅ |
| 升级2 (downgraded high→normal) | normal | batch | 2 | ✅ | next summary | 1/1 | 0/0 | ✅ |
| 封顶 hold | high | held | 2 | ❌ | NULL | 1/1 | 0/0 | N/A |

关键验证：升级复用冻结 downgrade（severity 逐步从 low→normal→high，从未到 critical）；每步只一笔 admission/charge，不创建 member/authority 或 channel operation；封顶 hold 时不轮换 nonce。

#### 2.1.3 Summary 三格 accounting 补齐

#699 指出 `TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries` 未断言 member/authority。本轮每格增加 admissions/charges/operations/members/authority 断言：三格均为 `1/1/0/0/0`；`=`/`>` 的 held 格确认零 member/authority。

**注记**：该测试未调用 `assertStaleReplayRejected`，与 #699 的"重复旧 CAS"子项有一字面差。但 stale CAS 重在证明端口拒绝已消费的 version/nonce 且不改变持久态——`TestAdvanceInterruptExpiryAndMaxOutcomeMatrix`（4 格）、`TestAdvanceInterruptEscalationCountsReuseDowngrade`（2 步）、`TestAdvanceInterruptReasonOutcomeMatrix`（6 格可行使）已逐级覆盖。summary 边界测试的本职为验证 `<`/`=`/`>` 状态分流与 accounting，非阻断。

#### 2.1.4 Reason × outcome 闭合表

新增 `TestAdvanceInterruptReasonOutcomeMatrix`，8 格覆盖：

| reason | on-expire/on-max | 结果 |
|---|---|---|
| `code_review`（allowed） | expire hold | open/held/expiry ✅ |
| `code_review` | expire auto reject | closed/expired_auto_reject ✅ |
| `code_review` | max hold | open/held/max_escalations ✅ |
| `code_review` | max auto reject | closed/expired_auto_reject ✅ |
| `startup_stall`（prohibited） | expire hold | open/held/expiry ✅ |
| `startup_stall` | max hold | open/held/max_escalations ✅ |
| `startup_stall` | expire auto reject | emit 拒绝 ✅ |
| `startup_stall` | max auto reject | emit 拒绝 ✅ |

每格可行使均以 `advanceOutcome` + `assertStaleReplayRejected` 验证；`startup_stall` 的 auto_reject 在发射层拒绝（`rejectEmit=true`），与生产代码 §4.1/§6 双重拒绝一致。

### 2.2 holdAdvance exclusion + post-escalation expiry boundaries still green：**YES**

#### 2.2.1 holdAdvance 事务排除

生产代码 `holdAdvance`（`advance_interrupt.go:139`）在 version+1 后立即调用 `excludeStaleBatchMembersTx`（行 144），与 #693 修复一致，未回退。

`TestAdvanceInterruptExcludesStaleDailyMembersAndCancelsEmptyBatch` 的四格（close / version change / expire hold / max hold）全部断言 `excluded_at_ms` 等于 advance 时刻，reopen seal 后空批 `cancelled` 且无 `channel_publish`。hold 分支的 durable exclusion 事实与事务边界均成立。

#### 2.2.2 Post-escalation expiry boundaries

`TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries` 三格仍以 `AdvanceInterrupt` + `AdvanceExpiry` 触发真实升级后重算：

| 格 | 新 expiry 相对 summary | dispatch_state | held_reason | version | escalation | next_dispatch |
|---|---|---|---|---|---|---|
| `<` | summary 在 expiry 后 | held | batch_after_expiry | 2 | 1 | NULL |
| `=` | summary 恰在 expiry | held | batch_after_expiry | 2 | 1 | NULL |
| `>` | summary 在 expiry 前 | ready | — | 2 | 1 | next midnight |

合法 `<` 格的 frozen due 由 `TestAdvanceInterruptDispatchUsesFrozenSummaryDue` 覆盖（batch ID/due/member authority 均冻结正确）。三格未回退。

### 2.3 Verdict PASS | PASS WITH NOTES | FAIL；checklist YES/NO：**YES**

## 3. 回归验证

- `git diff ddb6b3e..871d719 --check`：通过（无冲突空白）。
- #705 未新增 migration；当前 migration 0001–0051 连续，0001 SHA-256 仍为 `9696d3e1ecb65045dba91b7457f144c85cb275b46f2480f0c4ecca76e4899c33`。
- `go test ./internal/storage/ -run 'TestAdvanceInterrupt' -count=3`：**通过（3/3，无 flake）**。
- `go test ./internal/storage/ ./internal/intake/ ./cmd/siftd/ ./cmd/sift-advance-interrupt-repair/`：**通过**。
- `go vet ./internal/storage ./internal/intake ./cmd/siftd ./cmd/sift-advance-interrupt-repair`：**通过**。
- `go test ./...`：**全仓通过**。

## 4. Issue #711 checklist

- [x] 获取并阅读 #711 全文、Must verify、references 与 constraints：**YES**
- [x] 获取并阅读 #711 comments：**YES（0 条）**
- [x] 对照 #699 FAIL、#705/PR #710 变更复审：**YES**
- [x] Outcome/reason matrix from #699 closed（state/authority/accounting/reason + stale CAS）：**YES**
- [x] holdAdvance exclusion + post-escalation expiry boundaries still green：**YES**
- [x] 只写 `docs/reviews/`，未 push/MR/merge，未修改 WBS：**YES**
- [x] 合规包定向重复测试与全仓测试通过：**YES**
- [x] Verdict PASS | PASS WITH NOTES | FAIL；checklist YES/NO：**YES**

## 5. 最终裁决

**PASS WITH NOTES。** #705 以测试矩阵闭合了 #699 的 state/authority/accounting/reason 全部缺口；`holdAdvance` 事务排除与 post-escalation expiry 边界保持绿色。注记：`TestAdvanceInterruptPostEscalationSummaryExpiryBoundaries` 未独立调用 `assertStaleReplayRejected`，但 stale CAS 已由其他三个矩阵测试逐级覆盖，属非阻断。本轨道可核销；#699 遗留 P1 现为零。
