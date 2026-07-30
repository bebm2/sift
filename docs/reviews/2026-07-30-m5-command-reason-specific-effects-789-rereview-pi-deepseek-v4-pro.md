# M5 Command reason-specific effects 定向复审（#789）

**复审对象:** Issue #789 / PR #790（`feat/issue-789-wire-remaining-command-reason-specific-effects-n`，commit `f872df5`，合入 main `d8873c3`）

**复审人:** pi × DeepSeek v4 Pro

**复审日期:** 2026-07-30

**复审范围:** WBS §5.4 未勾「逐项 reason×action 效果」——非 startup retry + guardrail/code_review approve（`ErrCommandEffectNotWired` 薄片）

---

## Verdict: **PASS**

（0×P0，0×P1，2×P2 记录不阻塞）

---

## Checklist

| # | 判据 | 结果 | 证据 |
|---|------|------|------|
| 1 | `guardrail_violation` approve：消费 one-time exemption → `command_effects` → close/responded → 入队 Gate re-eval | **YES** | `commandApproveTx` + `insertCommandEffectTx` + `enqueueGateReEvaluationTx`；`command_effects_test.go` |
| 2 | `code_review` approve：immutable human_review_approval → close/responded → Gate re-eval | **YES** | 同上 |
| 3 | `merge_conflict` retry：frozen head Gate re-eval，无 attempt/merge op | **YES** | `commandRetryTx` → `commandGateReEvalTx` |
| 4 | `failure_review` retry `gate_recheck`：binding 驱动 Gate re-eval | **YES** | `RetryKind==gate_recheck` 分支 |
| 5 | `failure_review` retry `new_attempt`：terminalize + next attempt/claim/launch | **YES** | `commandNewAttemptRetryTx` + `spawnNextAttemptTx` |
| 6 | `agent_blocked` retry：terminalize + next attempt（无 Task Spec 变更） | **YES** | 同上路径 |
| 7 | 仍诚实拒绝非 canonical / stale CAS | **YES** | table-driven `rejected_option` + all-or-nothing stale CAS；`-race` 绿 |
| 8 | 无第二写口；复用 transition / HumanDecision / outbox | **YES** | Sol 全文核对 |

---

## Findings（不阻塞）

### [P2] `commandEffectBinding` 缺显式 terminal pair 交叉验证

依赖 `EmitInterrupt` 保证 `terminal_attempt_no=attempt_no` 等式；运行时正确但无法在 Command 侧独立重验证。`fixer=switch:agent::glm-5.2`

### [P2] `failure_review` RetryKind 隐式 else→new_attempt

应显式 `new_attempt` case + `default: ErrCommandEffectNotWired`。`fixer=switch:agent::glm-5.2`

---

## 诚实缺口（不读作本片闭合）

- `gate_re_evaluation` worker / `CompleteOutboxAttempt` 路径仍缺（ops 可 pending）
- `/sift ask` 的 agent_blocked 全契约（Task Spec 快照 + 终结 + 新 attempt）仍未接线
- 探测失败升级计数 / 达上限 hold 端到端未验（§5.3 / AdvanceInterrupt）
- §5.3 `max_escalations` → `auto_reject|hold` 映射未勾
- 不读作 M5 已实现；勿启动 #748+
