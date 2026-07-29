FAIL

# M5 #605 T4→EmitInterrupt after #599 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#599 / PR #603，实现提交 `9a713ee522d45d0011914eae55e063a905153156`，合入提交 `1836e09`
> 评审基线：`main` / `origin/main` `1836e09`
> 判定基准：[#588 FAIL](2026-07-30-m5-t4-emit-interrupt-581-rereview-pi-gpt-5.6-sol.md)、[`interrupt.md` §3.6/§5.1/§9.2](../specs/interrupt.md)、[`storage.md` §6.4/§12.2/§12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（2×P1）。** #599 已关闭 #588 实测的 binding identity 绕过：migration `0039` 不再给 legacy-shaped 新 INSERT 留空 calibration/Change/policy 旁路；Gate 生产组装会填写 effective-policy digest，首次 Gate reconcile 也会冻结 Run 的 Change/head。canonical 但 forged 的新 `code_review` binding 因而会被 `0038`/`0039` 的叠加 trigger 拒绝。

但 #599 / PR #603 明确自述 **“Full acceptance matrix still open”**，实现 diff 也没有交付 #588 要求的 SQL/T4/Gate/crash-concurrency 矩阵。除此之外，完整仓库测试出现稳定的 Intake 回归：五个 external-merge 场景在创建既有 Gate `code_review` Interrupt 时被 `0038` 的 `invalid interrupt binding provenance` 拒绝；定向 `-count=10` 每轮均失败。PR 虽修改了 ambiguous case 的第二个 fixture，却没有为第一次以及其他 Gate fixture 冻结 `runs.change_head_sha`，因此合入基线不是全绿。

本轮遵守“禁止自修自审”：只新增本评审报告，不修改被评审实现、测试或规格。

## 2. 阻断项

### P1-1：完整 acceptance matrix 仍为 NO

#599 的 16 文件 diff 主要增加严格 identity trigger、Gate Change/head 冻结、policy digest 接线，并适配既有 happy fixtures；没有新增完整验收矩阵。#588 的缺口仍在：

- **SQL/binding：** `TestGateHITLIsAtomicWithCalibration` 只增加一条 forged `code_review` INSERT 负测。没有 malformed/unknown arm、required/null、跨 reason、value type、canonical order/digest、逐 identity 字段错配、options 一致性及各拒绝点整事务回滚矩阵；其他 binding arm 也没有本轮增量。
- **attempt T4：** `TestAcceptInterruptT4AttemptGolden` 仍只直接调用接纳函数并覆盖正常 brief、option reorder 与 unknown recommended option；没有按 §3.6 断言完整 canonical input/output bytes、persisted headline/options/links，也没有 unknown fragment 与安全事件 link 的完整组合矩阵。
- **Report-quota T4：** 既有测试只检查部分字段和 invalid output fallback，没有使用同一 canonical serializer 逐层重算并逐字节比较规格中的完整 input/output。
- **Gate T4：** 仍只有 normal 与 option reorder fallback；没有 unknown fragment/option、完整 links/options、evaluation/calibration/operation identity，以及 Gate failure-review attempt 路径。
- **Report crash/concurrency：** `TestRecordBlockerReportKeepsRunningRun` 仍是单线程 happy/replay；publish/binding/admission 失败回滚、首次 quota exhaustion crash/replay 与 concurrent writer 矩阵均未出现。

PR #603 的实现方自述与实际 diff一致，必须按 #605 要求严格记为 **NO**，不能用现有定向绿灯替代缺失 vectors。

### P1-2：`go test ./...` 存在稳定 Intake 回归

`go test ./... -count=1` 中以下场景失败于 `constraint failed: invalid interrupt binding provenance (1811)`：

- `TestReconcilerOnceExternalMergeCompletesWaitingHuman`
- `TestReconcilerExternalInconclusiveMergeConvergesWithoutSettlement`
- `TestReconcilerExternalMergeFactsFirstWithoutExactBinding/exact_binary`
- `.../exact_inconclusive`
- `.../ambiguous`

对这五个场景单独执行 `-count=10`，每轮均以同一错误失败，不是时序 flake。根因是 `intakeGateRecord`/`intakeGateInterrupt` 使用 frozen head `bbbb…`，但第一次 `RecordGateEvaluationAndEmitInterrupt` 前仍只通过 `RecordCreatedChange` 写 Change ID，没有写 `runs.change_head_sha`；`0038_advance_interrupt_p1_closure.sql` 的 `interrupt_binding_provenance_insert` 要求 Run head、snapshot head 与 binding head完全一致。#599 只在 ambiguous 分支第一次失败之后补第二个 Change 的 `change_id`，没有修复任何首次 binding。

`go test ./internal/storage/ ./internal/gate/` 通过，说明 #599 明示的两个包已适配；但全仓稳定红灯证明跨 owner 调用 fixture/契约未同步，不能接受为合入完成态。

同一轮全量测试还出现 doctor fixture `signal: killed` 与 launchworker marker 时序失败；两者单独重跑通过，按既有 flake 记非阻断。Intake 失败则稳定复现，属于本次阻断。

## 3. binding identity 复审

本轮 narrow identity 修复本身有效：

- `0039_emit_interrupt_binding_identity_closure.sql` 删除 0034 的 legacy 放行表达式；新 `code_review` binding 必须经 Interrupt→calibration→evaluation→snapshot 链命中同 Run，并匹配 Run Change、snapshot head 与 effective-policy hash。
- 既有 `0038` provenance trigger额外要求 `runs.change_head_sha` 等于 binding/snapshot head；因此 #588 的 canonical forged Change/head/policy INSERT 不再能利用空 calibration、空 Change 或空 policy digest绕过。
- `interruptCommand` 将 `in.EffectivePolicyHash` 写入 `PolicySnapshotID`；`RecordGateEvaluationAndEmitInterrupt` 拒绝调用方提供的不同 digest并以 Gate record 的 hash补齐该字段。
- production Gate reconciler在组装/evaluate之前调用 `FreezeGateChangeHead`，通过 Run version CAS冻结 Change/head；stale identity不继续评估。
- `TestGateHITLIsAtomicWithCalibration` 的新增负测证明一条 forged binding被拒绝；尽管它不构成完整 SQL矩阵，静态 trigger与生产接线足以核销 #588 指定的 legacy-shaped `code_review` 绕过。

因此“binding exact Change/head/policy identity”记 **YES**；“完整 binding/事务验收矩阵”仍记 **NO**。

## 4. migration 与回归核验

- migration 文件为 `internal/storage/migrations/0039_emit_interrupt_binding_identity_closure.sql`：**YES**。
- 当前 migrations 共 39 个，`0001`–`0039` 连续、无缺号、无重复；`0039` 位于合入后的 `0038_advance_interrupt_p1_closure.sql` 之后：**YES**。
- embedded SchemaVersion、migration row count与 reopen expectation均更新至 39：**YES**。
- #588 已关闭的 public `ReportOnly` 移除、Report专用 no-transition owner、receipt identity与 `0031` closed shape/order未见回退。
- migration 编号正确不抵消 acceptance matrix及全仓测试回归。

## 5. 执行证据

- 获取并完整阅读 `gh issue view 605`、`gh issue view 605 --comments`（0 comments），并回溯 #576/#581/#588/#599、comments、PR #585/#603及实现 diff。
- `git diff 1836e09^1..1836e09 --check`：**通过**。
- `go test ./internal/storage/ ./internal/gate/ -count=1`：**通过**。
- storage/Gate 定向矩阵子集 `-count=10`：**通过**。
- storage/Gate 定向 race测试：**通过**。
- `go vet ./...`：**通过**。
- `go test ./... -count=1`：**失败**；含上述稳定 Intake provenance 回归及两个可单独通过的时序失败。
- Intake 五个失败场景 `-count=10`：**失败（稳定复现）**。
- doctor与 launchworker失败用例单独重跑：**通过**。
- migration扫描：39个版本，`0001`–`0039`连续，无缺号、无重复。

## 6. Issue #605 验收清单

| #605 / #599 条件 | 结果 | 说明 |
|---|---|---|
| 获取并阅读 #605全文、Agent建议、关闭条件、约束与comments | **YES** | GitHub，0 comments。 |
| 对照 #588 FAIL / #599 | **YES** | 已复审 identity与完整矩阵。 |
| binding exact Change/head/policy identity | **YES** | 0038/0039 trigger与Gate接线关闭 legacy-shaped forged路径。 |
| Gate 不再持久化空 policy digest | **YES** | production builder填写，storage port校验/补齐。 |
| SQL binding rejection/全事务回滚矩阵 | **NO** | 本轮只有一条 forged `code_review`负测。 |
| §3.6 attempt T4 exact golden完整 | **NO** | input/output/persisted bytes及 unknown-fragment矩阵不完整。 |
| Report-quota T4 exact golden完整 | **NO** | 未逐层 canonical serializer重算完整 bytes。 |
| Gate normal/invalid/failure-review矩阵完整 | **NO** | 仍只有 normal与 option reorder fallback。 |
| Report quota crash/concurrency矩阵完整 | **NO** | 仍为串行 happy/replay。 |
| 实现方“full matrix still open”严格记 NO | **YES** | 自述与 diff一致。 |
| `go test ./internal/storage/ ./internal/gate/`全绿 | **YES** | 两包通过。 |
| `go test ./...`全绿 | **NO** | Intake provenance回归稳定失败。 |
| migration为 0039且无重复 | **YES** | 0001–0039连续唯一。 |
| 已关闭 ReportOnly/receipt/0031项未回退 | **YES** | 未见回退。 |
| 结论写入 `docs/reviews/`、仅当前 conventional worktree | **YES** | 当前分支 `feat/issue-605-rereview-t4-emitinterrupt-after-599`。 |
| 禁止自修自审、未 push/MR/merge | **YES** | 本轮仅新增复审报告。 |
| #599可按 #588 原关闭标尺核销 | **NO** | identity子项可核销；完整矩阵与全仓绿灯不可核销。 |

## 7. 最终裁决

**FAIL。** migration `0039` 编号正确，#588 的 legacy-shaped forged `code_review` binding identity绕过已关闭，Gate policy digest与 Change/head生产接线也已补齐；但实现方已准确自述完整 SQL/T4/Gate/crash-concurrency矩阵仍未交付，而且 #599 引入/暴露的 strict provenance contract使 Intake external-merge测试稳定全红。后续实现须先同步全部 Gate caller/fixture的 frozen Change/head并恢复 `go test ./...`，再补齐 #588 列出的完整验收矩阵，交由不同代理复审。
