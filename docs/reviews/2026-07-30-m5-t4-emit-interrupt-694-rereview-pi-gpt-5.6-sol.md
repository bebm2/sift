FAIL

# M5 #694 T4→EmitInterrupt after #688 定向复审

> 日期：2026-07-30
> 评审人：pi × GPT-5.6-sol
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#688 / PR #692，实现提交 `a4eafbd`，合入提交 `d2be519`
> 评审基线：`main` / `origin/main` `d2be519`
> 前次结论：[#682 FAIL](2026-07-30-m5-t4-emit-interrupt-682-rereview-pi-gpt-5.6-sol.md)
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`brain.md` §11](../specs/brain.md)、[`gate.md` §5/§7](../specs/gate.md)、[`report.md` §6.2/§7](../specs/report.md)、[`storage.md` §12.2.1](../specs/storage.md)

## 1. 结论

**FAIL（2×P1）。** #688 有有效增量：attempt 与 Report-quota 都新增了经过 production `Shell.CallT4` 的 invalid-output fallback；migration `0051` 与 `SchemaVersion=51`、51 条连续 migration 一致；Gate 新 trigger 拒绝一类 `gate_recheck` provenance 不匹配；Report quota 安全事件也补入 `failure_digest` 与 `generation_key`。要求的四包、全仓、定向重复、race、vet 和 PR CI 均通过。

但 #682 的关闭条件仍未闭合。production fallback 测试没有形成逐字节 golden 与 exact terminal source 对账；Gate negative 没有覆盖完整 evaluation/calibration provenance，且 `0051` 本身允许跨 Run Gate 链；Report 仍未实现结构拒发诊断，也没有补齐前次指出的 rate CAS/唯一 winner/提交边界及 charge/outbox 完整对象断言。因此不能核销 #688。

本轮仅新增本评审报告，未修改实现、测试、migration 或规格。

## 2. 阻断项

### P1-1：production fallback goldens 与 Gate provenance negatives 仍不完整

`internal/brain/t4_emit_interrupt_acceptance_test.go` 新增的两条 invalid-output 用例确实经过 production shell，这是明确进展；但它们只断言返回对象的部分字段及 `status=fallback`、非空 `fallback_reason`：

- attempt 用例没有逐字节读取并核对持久化 `headline/brief_markdown/options_json/links_json`；quota 用例也没有核对完整 links/raw JSON；
- 两者都没有断言 exact `fallback_reason=invalid_output`、terminal `logical_call_id`、T4 fallback version、prompt/schema version及其与本次 Interrupt 展示关联审计的一致性；
- 既有 storage seam 的 exact bytes 测试不能代替“production fallback golden”。

Gate 方面，`0051_failure_review_provenance.sql` 只比较 binding 的 change/head 与 snapshot/current Run，却没有要求 `gate_evaluations.run_id = interrupts.run_id`、`calibration_entries.run_id = interrupts.run_id`，也没有核对 snapshot `identity.run_id`。因此一个 Interrupt 若引用另一 Run 的 calibration/evaluation 链，只要 change/head 相同，trigger 仍可放行。新增/修改后的 negative 也只以全 NULL 的 `failure_review_attempt` arm 命中 trigger，没有分别证明 forged change/head、跨 Run evaluation/calibration 或 snapshot identity 被拒绝并全回滚。#682 要求的 provenance 正反闭包仍是 **NO**。

### P1-2：Report crash/concurrency closure 仍缺必需行为

#688 把 `failure_digest` 与 `generation_key` 加入既有 `security.report_quota_exhausted` event，并在四 writer 用例中检查它们非空且与 payload 相等；这不是 [`report.md` §6.2/§7](../specs/report.md) 要求的结构拒发诊断。

当前 `RecordReportQuotaExhaustion` / `recordBlockerReport` 在 exhaustion 已提交后若 `EmitInterrupt` 因 binding、publish target或其他结构问题被拒绝，只返回/吞并 `ErrInterruptRejected`；没有写 generation-key 幂等的确定性拒发诊断。事务内部错误要求的 `report_transaction_failed` 安全审计同样未实现。现有 emission-cut 测试仅核对 exhaustion 保留及表计数，未核对任何诊断身份。

此外，前次指出的第一事务 rate-bucket CAS cut、唯一 winner 冲突 cut与提交前/后边界仍未新增；四 writer 仍未逐对象核对 Report charge FK、attention charge 的 NULL/非 NULL关系，以及 forge-comment operation key/payload/subject。新增断言也只检查 digest/key 非空，没有按 canonical preimage 重算。故 Report crash/replay/concurrency P1 仍为 **NO**。

## 3. migration 与执行证据

- 完整读取 `gh issue view 694`、`gh issue view 694 --comments`，并回溯 #688、#682及 comments；检测到 GitHub。
- `0051_failure_review_provenance.sql` 存在，migration 文件连续 `0001..0051`，共 51 条；`TestMigrationRecordedAndIdempotent` 断言 `SchemaVersion=51` 与 reopen 后 51 rows：**PASS**。
- `git diff d2be519^..d2be519 --check`：**PASS**。
- `go test ./internal/storage/ ./internal/gate/ ./internal/brain/ ./cmd/siftd/ -count=1`：**PASS**。
- Brain fallback/canonical 定向测试 `-count=30`：**PASS**。
- Gate/Report 定向测试 `-count=20`：**PASS**。
- 上述 Brain、Gate、Report 定向测试 `-race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**PASS**。
- PR #692：vet/test、schema drift及四平台 build checks均 **PASS**。

## 4. Issue #694 验收清单

| 条件 | YES/NO | 说明 |
|---|---|---|
| 获取并阅读 #694 全文、Agent 建议、acceptance、constraints 与 comments | **YES** | GitHub；issue 无 comments。 |
| 回溯 #688 / #682 review 与 comments | **YES** | 已核对 PR #692、实现/合入提交及前次 FAIL。 |
| production attempt fallback golden 闭合 | **NO** | production fallback 已可达，但缺完整 persisted raw bytes 与 exact terminal source 对账。 |
| production Report-quota fallback golden 闭合 | **NO** | 同上；仅部分对象与非空 fallback reason。 |
| Gate provenance negatives 闭合 | **NO** | `0051` 缺跨 Run evaluation/calibration/snapshot identity约束，negative matrix 未覆盖。 |
| Report crash/replay/concurrency P1 闭合 | **NO** | 结构拒发/内部错误诊断未实现，第一事务及完整对象矩阵仍缺。 |
| migration `0051` 与 `SchemaVersion` 一致 | **YES** | 连续 51 条；version/count/reopen 测试一致。 |
| 要求的四个包测试 | **YES** | 全绿。 |
| 定向重复、race、vet、全仓测试与 PR CI | **YES** | 本轮全部通过。 |
| 仅在当前 conventional worktree 工作 | **YES** | `feat/issue-694-rereview-t4-emitinterrupt-after-688`。 |
| 仅写 `docs/reviews/`，未 push/MR/merge | **YES** | 仅新增本报告。 |
| #688 可按 #682 关闭标尺核销 | **NO** | 两项 P1 仍未关闭。 |

## 5. 最终裁决

**FAIL。** `0051`/`SchemaVersion` 一致且测试门禁全绿，但 production fallback exact goldens、Gate 完整 provenance negatives、Report 拒发诊断与 crash/concurrency 对象闭包仍未完成。
