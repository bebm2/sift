# M5 T4→EmitInterrupt #465 定向复审

## 结论

**FAIL。** #465 已把 T4 caller 从 Gate 调用方字段提升为 DB 级统一接缝，`startup_stall` 与 Gate 因而都经过同一个事务外调用点；fragment 预转义也已移除，缺 T4 contract 的 replay 现在 fail closed。这些关闭了 P1-2 与 P2，并修正了 P1-3 所指的二次转义机制。

但 P1-1/P1-4 仍未关闭：实现仍只有 attempt `failure_review` 的 `retry,reject,hold` 模板，没有 Report quota v1 的 `reject,hold` variant 或分派身份；§3.6 attempt / quota exact golden 和 Gate HITL 定向测试均未交付。新增 storage 测试本身还有两项稳定失败，且当前 fragment shape 无法产生 §3.6 的 exact attempt input/persisted bytes。因此不能核销 #465 或 W-M5-t4-emit-p1-close。

## 评审基线

- Issue：#468（含 comments）；被评审 Issue：#465（含 comments）
- PR：#467，commit `03516c3d9812e3624224debce7bb17d0993ec4cd`
- 前次报告：[`2026-07-29-m5-t4-emit-interrupt-454-review-pi-gpt-5.6-sol.md`](2026-07-29-m5-t4-emit-interrupt-454-review-pi-gpt-5.6-sol.md)
- 对照：`docs/specs/interrupt.md` §3.2、§3.6、§7.1；`docs/specs/report.md` §5；wave1 I1
- PR #467 范围：7 个文件，`+94/-16`

## 阻断项

### P1-1：Report quota canonical variant 仍不存在

`internal/storage/interrupt.go:181-188` 的 `InterruptFailureReview` 仍固定为 attempt 的 `retry,reject,hold` 三项。`emitInterrupt` 在 `:338` 仅按 reason 取这一个模板，`:357-359` 再对该模板执行 `canonicalRecommendedAction`；代码中没有依据 `attempt_no=null`、Report quota binding 或其他 closed discriminator 选择 `reject,hold` variant 的路径。

结果是 interrupt §3.6 / report §5 的 quota arm 无法成立：合法 quota `hold` 虽偶然命中三项集合，但 persisted options 会错误包含 `retry`；quota/attempt 交叉混用也不能按 variant fail closed。前次 P1-1 要求的两个 variant 交叉校验未关闭。

此外，前置 action 校验改变了 §3.2 的确定性错误：`recommended_action="retry\nnow"` 现在先返回 generic `recommended_action is not a canonical option`，而不是规范要求的 `interrupt_brief_lf_rejected`，对应存量测试 `internal/storage/interrupt_test.go:188-203` 已稳定失败。

**关闭条件：** 引入 closed variant 分派并在 generation key/admission/operation 前分别校验 attempt 与 Report quota canonical options；覆盖合法值、未知值、交叉混用及 §3.2 CRLF/CR/LF 精确拒绝码。

### P1-4：§3.6 golden、quota matrix 与 Gate HITL 验收仍缺，新增测试不通过

PR #467 只新增两个 T4 storage 测试和一个 replay 测试，没有：

- interrupt §3.6 attempt canonical input/output/persisted bytes；
- Report quota fallback、合法 `reject,hold`、重排/加 `retry`/错误推荐及 arm 交叉污染；
- Gate HITL 真实生产接线、provider disabled/invalid fallback trace，以及 options/severity/generation key 不变断言。

当前 `interruptBriefFragments`（`internal/storage/interrupt.go:514-522`）生成 `key=value`，而 §3.6 attempt golden 冻结的是原始 value fragments `[/sift reject, <!-- sift-op:x -->, <b>风险</b>]`。新增测试也以 `review_requirement=<b>risk</b>` 固化了不同 shape（`internal/storage/interrupt_test.go:154-160`）。其期望 bytes 又遗漏 `EscapeT4Text` 对 `_` 的转义，故 `TestEmitInterruptT4UsesConfiguredSeamAndEscapesFragmentsOnce` 在 `:166-168` 稳定失败。即使修正测试字符串，仍不能替代 §3.6 的 attempt exact vector。

**关闭条件：** 逐字节兑现 §3.6 两套 golden 和全部负向矩阵，补 Gate HITL 定向生产接线测试，并保持定向测试集全绿。

## 已关闭项

### P1-2：统一生产接缝 — YES

- `cmd/siftd/main.go:47-50` 在生产 DB 上安装唯一 T4 shell；
- `internal/storage/interrupt.go:380-386` 的唯一 `emitInterrupt` 路径在 `BeginTx` 前消费该接缝；
- Gate 已移除调用方 opt-in，`startup_stall` 仍调用同一 `DB.EmitInterrupt`。

所以 Gate、startup recovery 与其他生产 `EmitInterrupt` 调用者不会再因忘记填 `cmd.T4` 而静默绕过。验收证据仍应按 P1-4 补齐，但实现结构已关闭前次 P1-2。

### P1-3：二次转义机制 — YES；exact vector 仍归 P1-4 阻断

`interruptBriefFragments` 不再调用 fallback `escapeBrief`，接纳后仅由 `escapeT4Text` 在 `internal/storage/interrupt.go:561-563` 执行一次 sink 转义。前次指出的预转义再转义已消除。当前 fragment shape 与 golden 不符是独立的 exact acceptance 缺口，见 P1-4。

### P2 replay：YES

`internal/replay/replay.go:116-123` 对任何缺 frozen contract 的 Brain trace 返回确定性错误，不再静默略过 T4；`internal/replay/replay_test.go` 已覆盖缺 T4 contract。该实现选择了 #465 允许的 fail-closed 方案。

## 验证

- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay`：**失败**；稳定失败：
  - `internal/storage/TestEmitInterruptT4UsesConfiguredSeamAndEscapesFragmentsOnce`；
  - `internal/storage/TestEmitInterruptRejectsBeforeAnyWrite`。
- `go test ./...`：**失败**；除上述两个本次范围内稳定失败外，还观察到既知的 doctor fixture kill 与 launchworker/wrapper 时序失败。后者不改变本次结论。

## 关闭清单

| #465 条件 | 结果 |
|---|---|
| variant-aware canonical action | **NO**：仅 attempt 三项模板，无 Report quota variant/discriminator |
| 统一生产 T4 接缝，含 startup_stall | **YES** |
| 消除 fragments 二次转义 | **YES**：但 fragment shape 尚不命中 §3.6 golden |
| §3.6 attempt golden | **NO** |
| Report quota variant/golden/负向矩阵 | **NO** |
| Gate HITL 定向测试 | **NO** |
| provider failure/fallback trace 与结构不变验收 | **NO** |
| replay 不静默跳过缺 contract 的 T4 trace | **YES**：fail closed |
| 定向测试全绿 | **NO** |
| 聚焦 T4/EmitInterrupt、未扩张 Command/Channel | **YES** |

**最终：FAIL。**
