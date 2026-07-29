FAIL

# M5 #622 T4→EmitInterrupt after #616 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#616 / PR #619，实现提交 `e879517`，合入提交 `3513160`
> 评审基线：`main` / `origin/main` `4e6bbb0`（另含后续 #617）
> 前次结论：[#605 FAIL](2026-07-30-m5-t4-emit-interrupt-605-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[#588 FAIL](2026-07-30-m5-t4-emit-interrupt-581-rereview-pi-gpt-5.6-sol.md)、[`interrupt.md` §3.6/§5.1/§9.2](../specs/interrupt.md)、[`report.md` §7](../specs/report.md)、[`storage.md` §6.4/§12.2/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（1×P1）。** #616 恢复了 #605 中稳定失败的 Intake fixtures，且新增的六条 `code_review` identity 负测和 unknown-fragment/option 接纳负测是有效增量；要求的三个包、定向 race 与 PR CI 均通过。但 #616 只新增 `internal/storage/emit_interrupt_acceptance_test.go` 76 行，PR 自述也明确为 **“crash/concurrency matrix still partial”**。它没有补 Report quota 的 crash/concurrency、Gate T4、Report-quota exact T4 或完整 SQL/事务矩阵，不能按 #605 的关闭标尺核销。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试或规格。

## 2. 阻断项

### P1-1：SQL/T4/Gate/crash-concurrency 完整验收矩阵仍为 NO

#### SQL / binding：PARTIAL

`TestEmitInterruptBindingIdentityAcceptanceMatrix` 覆盖 unknown arm、跨 reason，以及 `code_review` 的 Change/head/policy/missing identity 六条拒绝，证明 0039 的 narrow identity guard 未回退。但它不是 #605 要求的完整矩阵：

- 没有 malformed JSON、null/required、错误 value type、额外字段、canonical key order/digest、binding options 一致性；
- 没有逐一覆盖 `design_approval`、`guardrail_violation`、`agent_blocked`、`merge_conflict`、`startup_stall`、两个 `failure_review` arms 的 source identity/FK 错配；
- 每例先完整提交一个合法 Gate/Interrupt/binding，再对同一个 `interrupt_id` 单独插入第二条 binding，最后只断言总数仍为 1（`emit_interrupt_acceptance_test.go:44-50`）。这能证明该额外 INSERT 被拒绝，却不能证明 EmitInterrupt 五件事在 binding/admission/publish 等拒绝点整事务回滚；文件注释所称“did not retain any partial write”超出实际断言。

#### attempt T4：PARTIAL

`TestAcceptInterruptT4RejectsUnknownFragmentAndPreservesCanonicalBytes` 增加 unknown conclusion/key point、option reorder、错误 recommended option 与 escaped brief golden，增量有效。但它仍只直接调用 `acceptInterruptT4`：没有比较完整 canonical T4 input/output JSON bytes，也没有经过 `EmitInterrupt` 断言 persisted headline/options/links/source。名称中的 “CanonicalBytes” 实际只比较 renderer 生成的 brief string。

#### Report-quota T4：NO 增量

#616 没有修改 Report 或既有 quota T4 测试。仍未用同一 canonical serializer 逐层重算完整合法 `reject,hold` input/output bytes及 persisted bytes，也未形成规格要求的 fallback/reorder/added-retry/wrong-recommended 完整表。

#### Gate T4：NO 增量

#616 没有修改 `internal/gate`。`TestGateT4NormalAndInvalidFallbackPreserveEmissionIdentity` 仍只有 normal 和 option reorder 两例；unknown fragment/option、完整 links/options、evaluation/calibration/operation identity及 Gate failure-review attempt矩阵未补。

#### Report quota crash/concurrency：NO 增量

#616 没有修改 `RecordReport` 或 `report_test.go`。`TestRecordBlockerReportKeepsRunningRun` 仍是单 DB、串行 happy/replay。没有 publish target、binding、admission/SQLite失败回滚，没有 exhaustion 线性化提交前后和专用发射前后的 crash injection，也没有四个并发触顶 writer 至多一个 exhaustion/security event/generation-key Interrupt 的矩阵。PR #619 已准确自述该矩阵仍 partial；按 #622 明示约束必须严格记 **NO**。

## 3. 已关闭项与回归

- #605 的 Intake provenance 回归已由 #610 fixture 修复；本轮 `internal/intake` 通过，未见回退。
- 0039 binding exact Change/head/policy identity继续有效；#616 未改 migration 或生产实现。
- #616 未新增 migration，符合“仅矩阵需要时才从 0042+ 起”的约束。当前基线因后续 #617 已有 42 个 migration，`0001`–`0042` 连续、无重复或缺号；这不是 #616 的交付。
- PR #619 的 `vet + test`、schema drift与四平台 build均为成功。
- 全仓本地测试只有已知 doctor 并行时序失败；该用例单独 `-count=3` 通过，按既有非阻断 flake 记录。

## 4. 执行证据

- 获取并完整阅读 `gh issue view 622`、`gh issue view 622 --comments`（0 comments），并回溯 #605、#616、PR #619及实现 diff。
- `git diff 3513160^1..3513160 --check`：**通过**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/intake/ -count=1`：**通过**。
- 新增测试与既有 T4/Report定向集合 `-count=10`：**通过**。
- storage/Gate/Intake 定向 `-race`：**通过**。
- `go vet ./...`：**通过**。
- `go test ./... -count=1`：**仅 `TestDoctorBaselineChecksConfiguredDependencies` 出现 `signal: killed`**；该用例单独 `-count=3` 通过，其余包通过。
- migration扫描：42个版本，`0001`–`0042`连续，无重复/缺号。

## 5. Issue #622 验收清单

| 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #622全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #605 FAIL / #616 | **YES** | 已逐项复审。 |
| Intake回归恢复 | **YES** | 三个要求包均绿。 |
| SQL identity负测有增量 | **YES / PARTIAL** | 六条 `code_review`向量；非完整 union/事务矩阵。 |
| SQL binding拒绝与五件事整事务回滚矩阵完整 | **NO** | 只测合法提交后的第二次 standalone INSERT。 |
| attempt T4 exact golden完整 | **NO / PARTIAL** | 增加 fragment/option拒绝和 brief；无完整 canonical/persisted bytes。 |
| Report-quota T4 exact golden完整 | **NO** | 无本轮增量。 |
| Gate normal/invalid/failure-review矩阵完整 | **NO** | 无本轮增量。 |
| Report quota crash/concurrency矩阵完整 | **NO** | 实现方自述 partial，diff中不存在。 |
| `go test ./internal/storage/ ./internal/gate/ ./internal/intake/` | **YES** | 全绿。 |
| migration约束 | **YES** | #616无需 migration；未抢占错误编号。 |
| 结论写入 `docs/reviews/`、仅当前 conventional worktree | **YES** | `feat/issue-622-rereview-t4-emitinterrupt-after-616`。 |
| 禁止自修自审、未 push/MR/merge | **YES** | 仅新增评审报告。 |
| #616可按 #605关闭标尺核销 | **NO** | 完整矩阵仍未交付。 |

## 6. 最终裁决

**FAIL。** #616 的 narrow SQL identity和 T4 fragment/option vectors有效，Intake回归也已恢复；但 PR仅增加两个 storage测试，且自述 crash/concurrency仍 partial。后续实现须补齐 closed binding union及真实五件事回滚矩阵、attempt与 Report-quota完整 canonical/persisted byte goldens、Gate normal/invalid/failure-review纵向矩阵，以及 Report quota各 crash cut和并发触顶矩阵，再交由不同代理复审。
