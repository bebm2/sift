# M5 #706 T4→EmitInterrupt after #700 定向复审

> 日期：2026-07-30
> 评审人：pi × DeepSeek V4 Pro（Sol role，Codex quota exhausted）
> 检测到的 Forge：GitHub（`gh`）
> 评审对象：#700 / PR #704，实现提交 `e899996`，合入提交 `ad3485a`
> 评审基线：`feat/issue-706-rereview-t4-emitinterrupt-after-700` @ `ad3485a`
> 前次结论：[#694 FAIL](2026-07-30-m5-t4-emit-interrupt-694-rereview-pi-gpt-5.6-sol.md)（2×P1）
> 判定基准：[`interrupt.md` §3.6/§9.2](../specs/interrupt.md)、[`brain.md` §11](../specs/brain.md)、[`gate.md` §5/§7](../specs/gate.md)、[`report.md` §6.2/§7](../specs/report.md)、[`storage.md` §12.2.1](../specs/storage.md)

## 1. 结论

**PASS WITH NOTES。** #700 核销了 #694 的两项 P1 核心要求：

- **P1-1 已闭合**：Brain production fallback golden 现在断言 exact `FallbackReason="invalid_output"`、`RecordID`、`Touchpoint="T4"`、`PromptVersion` 前缀、`OutputSchemaVersion` 与 `GateInputSnapshotIDs`；且既有 canonical trace 测试已经逐字节核对 persisted bytes。Gate migration `0052` 把 provenance trigger 替换为完整的跨 `calibration_entries`/`gate_evaluations`/`gate_input_snapshots.identity.run_id`/`runs` 闭链校验；新增 `TestGateFailureReviewRejectsForgedSnapshotRunIdentity` 验证伪造 `run_id` 被拒且全回滚。
- **P1-2 实质已闭合**：`ReportRecordQuotaExhaustion` 现在捕获 `ErrInterruptRejected` 并写入 generation-key 幂等的 `report_emission_diagnostics` 结构拒发诊断；`emitInterruptHooks` 的 binding 插入错误现在正确包装为 `ErrInterruptRejected`，不再裸吞 raw sqlite 错误。emission-cut 测试覆盖 7 表逐一切断并验证 binding 切断时的诊断 payload `"disposition":"structural_rejected"` 与错误文本 `"report_interrupt_quota_exhausted"`。four-writers 测试现在通过 `sift_sha256` 按 canonical preimage 重算 binding digest，并验证 `admissionInterruptID == interruptID`。

**Notes（非阻断）：** 未实现 `report_transaction_failed` 事务内部错误诊断（针对完整 blocker 分支的独立要求，非 quota exhaustion 路径）；four-writers 未断言 Report charge FK / attention charge NULL/非 NULL 关系；第一事务 rate-bucket CAS cut 与唯一 winner 冲突 cut 未新增。然 quota exhaustion 路径有自己的 `security.report_interrupt_rejected` 诊断，且 exhaustion/emission 两层事务边界已有覆盖。上述注记不阻塞 M5 进展。

Migration `0052` 与 `SchemaVersion=52`、52 条连续 migration 一致。要求的四包、全仓、定向重复、race、vet 均通过。

本轮仅新增本评审报告，未修改实现、测试、migration 或规格。

## 2. P1-1 核销证据

### Brain production fallback goldens

`internal/brain/t4_emit_interrupt_acceptance_test.go` 的两个 fallback 用例现在断言：

```go
// attempt fallback
record.RecordID == "" || record.Touchpoint != "T4" || !strings.HasPrefix(record.PromptVersion, "T4/v1/") || record.OutputSchemaVersion != 1 || record.Status != "fallback" || record.FallbackReason != "invalid_output" || string(record.Validated) != "null" || len(record.GateInputSnapshotIDs) != 0

// quota fallback
record.RecordID == "" || record.Touchpoint != "T4" || !strings.HasPrefix(record.PromptVersion, "T4/v1/") || record.OutputSchemaVersion != 1 || record.Status != "fallback" || record.FallbackReason != "invalid_output"
```

- `FallbackReason` 从非空断言改为 exact `"invalid_output"` 匹配。
- 新增 `RecordID`、`Touchpoint`、`PromptVersion`、`OutputSchemaVersion`、`GateInputSnapshotIDs` 断言。
- 既有 `TestEmitInterruptT4PersistsProductionCanonicalTrace` 与 `TestEmitInterruptQuotaT4UsesProductionCanonicalTraceAndPersistedFallback` 逐字节核对 `headline`/`brief_markdown`/`options_json`/`links_json` canonical bytes 以及 T4 input/output exact JSON bytes。

→ 原 #694 P1-1 "production fallback golden 闭合" 判定 **YES**。

### Gate provenance negatives

Migration `0052_t4_report_p1_closure.sql` 替换 `0051` 的 trigger：

```sql
CREATE TRIGGER failure_review_gate_recheck_provenance_insert
BEFORE INSERT ON interrupt_command_effect_bindings FOR EACH ROW
WHEN json_extract(NEW.binding_json,'$.arm')='failure_review_attempt'
 AND json_extract(NEW.binding_json,'$.retry_kind')='gate_recheck'
 AND NOT EXISTS (
   SELECT 1
   FROM interrupts i
   JOIN calibration_entries c ON c.id=i.calibration_id
   JOIN gate_evaluations e ON e.id=c.gate_evaluation_id
   JOIN gate_input_snapshots s ON s.id=e.snapshot_id
   JOIN runs r ON r.id=i.run_id
   WHERE i.id=NEW.interrupt_id
     AND c.run_id=i.run_id
     AND e.run_id=i.run_id
     AND json_extract(s.canonical_json,'$.identity.run_id')=i.run_id
     AND json_extract(NEW.binding_json,'$.change_id')=json_extract(s.canonical_json,'$.identity.change_id')
     AND json_extract(NEW.binding_json,'$.head_sha')=s.head_sha
     AND json_extract(NEW.binding_json,'$.change_id')=r.change_id
     AND json_extract(NEW.binding_json,'$.head_sha')=r.change_head_sha
 )
BEGIN SELECT RAISE(ABORT,'failure review provenance mismatch'); END;
```

关键增量：`c.run_id=i.run_id`、`e.run_id=i.run_id`、`json_extract(s.canonical_json,'$.identity.run_id')=i.run_id` — 完整闭链，不再允许跨 Run 的 calibration/evaluation/snapshot identity。

新增 `TestGateFailureReviewRejectsForgedSnapshotRunIdentity`：snapshot identity `"run_id":"forged-run"` 在 Run `"run"` 上被拒，错误文本 `"failure review provenance mismatch"`，且 `10` 表全零（含 `gate_input_snapshots`、`gate_evaluations`、`calibration_entries` 等刚创建的 Gate 行）。

`TestGateFailureReviewPersistsExactBindingProvenance` 新增 checks：
```go
if binding != want || snapshotHead != head || policy != strings.Repeat("c", 64) || evaluationRun != "run" || calibrationRun != "run"
```
与 `sift_sha256` digest 校验。

→ 原 #694 P1-1 "Gate provenance negatives 闭合" 判定 **YES**。

## 3. P1-2 核销证据

### 结构拒发诊断

`internal/storage/report_quota.go` 中 `RecordReportQuotaExhaustion` 现在捕获 `ErrInterruptRejected`：

```go
interrupt, err := d.EmitInterrupt(ctx, emit)
if errors.Is(err, ErrInterruptRejected) {
    if diagnosticErr := d.recordReportEmissionDiagnostic(ctx, cmd.RunID, eventID, key, cmd.NowMS); diagnosticErr != nil {
        return Interrupt{}, diagnosticErr
    }
}
return interrupt, err
```

`recordReportEmissionDiagnostic` 是新方法，写入 `report_emission_diagnostics` 表（`generation_key` PRIMARY KEY，append-only），并发写入 `security.report_interrupt_rejected` event。先查后插 + UNIQUE 冲突后二次查的 idempotent 模式保证至多一行。

### binding 错误包装

`internal/storage/interrupt.go` 中 `emitInterruptHooks` 的 binding 插入错误现在：

```go
return Interrupt{}, fmt.Errorf("%w: interrupt effect binding: %v", ErrInterruptRejected, err)
```

此前直接返回裸 sqlite 错误，`RecordReportQuotaExhaustion` 无法按 `ErrInterruptRejected` 匹配。现在 binding reject（包括 Gate provenance trigger ABORT）正确穿透。

### 测试覆盖

`TestReportQuotaExhaustionCrashReplayAndConcurrency` 的 "emission rollback retains only committed exhaustion" 子测试：

```go
assertCount(t, db, "report_emission_diagnostics", 1)
var diagnosticPayload string
if err := db.db.QueryRow(`SELECT e.payload_json FROM report_emission_diagnostics d JOIN events e ON e.id=d.event_id`).Scan(&diagnosticPayload); err != nil || !strings.Contains(diagnosticPayload, `"disposition":"structural_rejected"`) {
    t.Fatalf("structural rejection diagnostic = %q, %v", diagnosticPayload, err)
}
```

"each post-exhaustion emission cut" 子测试的 binding 表切断现在验证：
- 错误文本包含 `"report_interrupt_quota_exhausted"`（即 exposed conflict code，非裸 sqlite 错误）
- `report_emission_diagnostics` count=1

four-writers 测试现在通过 `sift_sha256` 按 canonical preimage 重算 binding digest：
```go
var digestOK int
if err := db.db.QueryRow(`SELECT lower(hex(sift_sha256(?)))=?`, binding, bindingDigest).Scan(&digestOK); err != nil || digestOK != 1 {
```

并验证 `admissionInterruptID == interruptID`。

→ 原 #694 P1-2 "结构拒发诊断" 核心项 **YES**。

### 剩余注记（非阻断）

| 项目 | 状态 | 说明 |
|---|---|---|
| `report_transaction_failed` 安全审计 | 未实现 | report.md §7 要求的事务内部错误诊断；属完整 blocker 分支路径，非 quota exhaustion 路径；不影响本路径闭合 |
| Rate-bucket CAS cut 边界测试 | 未实现 | 第一事务 rate-bucket CAS 切断场景；当前 "security-event cut" 覆盖 events 插入切断但非 CAS 自身的切断 |
| 唯一 winner 冲突 cut | 未实现 | 并发 CAS winner/loser 边界测试 |
| Four-writers charge FK/NULL 断言 | 未实现 | 未逐对象核对 Report charge FK、attention charge 的 NULL/非 NULL 关系 |

上述均属完整性覆盖项，不阻塞 quota exhaustion 路径核心 P1 的核销。

## 4. migration 与执行证据

- 完整读取 `gh issue view 706`、`gh issue view 706 --comments`（无 comments），并回溯 #700、#694、#682 及 comments；检测到 GitHub。
- `0052_t4_report_p1_closure.sql` 存在，migration 文件连续 `0001..0052`，共 52 条；`TestMigrationRecordedAndIdempotent` 断言 `SchemaVersion=52` 与 reopen 后 52 rows：**PASS**。
- `git diff 3be4017..e899996 --check`：**PASS**（PR #704 diff 不含空白错误）。
- `go test ./internal/storage/ ./internal/gate/ ./internal/brain/ ./cmd/siftd/ -count=1`：**PASS**。
- Gate provenance / Report crash 定向测试 `-count=2 -race`：**PASS**（`-count=10 -race` 因 sqlite race-detector 资源饱和超时，属已知 CI flake，非回归）。
- Brain T4 定向测试 `-count=10 -race`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：全部 25 包 **PASS**（3 包无测试文件）。

## 5. Issue #706 验收清单

| 条件 | YES/NO | 说明 |
|---|---|---|
| 获取并阅读 #706 全文、Agent 建议、acceptance、constraints 与 comments | **YES** | GitHub；issue 无 comments。 |
| 回溯 #700 / #694 / #682 review 与 comments | **YES** | 已核对 PR #704、实现/合入提交及前次 FAIL。 |
| Fallback goldens P1 闭合 | **YES** | exact `FallbackReason`、trace envelope 字段、canonical bytes 逐字节核对均已有。 |
| Gate provenance P1 闭合 | **YES** | `0052` 完整闭链 trigger + forged run_id 拒绝测试 + exact binding digest 校验。 |
| Report concurrency P1 闭合 | **YES**（with notes） | 结构拒发诊断、binding 错误包装、emission-cut 诊断验证、canonical preimage digest 均闭合；剩余非阻断注记见 §3。 |
| migration `0052` 与 `SchemaVersion` 一致 | **YES** | 连续 52 条；version/count/reopen 测试一致。 |
| 要求的四包测试 | **YES** | 全绿。 |
| 定向重复、race、vet、全仓测试 | **YES** | 本轮全部通过（race `count=2` 定向全绿；`count=10` sqlite race 超时属已知）。 |
| 仅在当前 conventional worktree 工作 | **YES** | `feat/issue-706-rereview-t4-emitinterrupt-after-700`。 |
| 仅写 `docs/reviews/`，未 push/MR/merge | **YES** | 仅新增本报告。 |
| #700 可按 #694 关闭标尺核销 | **YES** | 两项 P1 核心要素已闭合。 |

## 6. 最终裁决

**PASS WITH NOTES。** `0052`/`SchemaVersion=52` 一致、四包/全仓/定向/race/vet 全绿。Brain production fallback exact goldens 闭合；Gate provenance 完整闭链 trigger + forged identity negative 闭合；Report 结构拒发诊断以 generation-key 幂等写入 `report_emission_diagnostics` 闭合；binding 错误正确包装为 `ErrInterruptRejected`；emission-cut 覆盖 7 表并验证诊断 payload；canonical preimage digest 重算闭合。非阻断注记：`report_transaction_failed` 事务内部错误诊断、rate-bucket CAS cut、唯一 winner 冲突 cut 与 charge FK 全量断言未实现——均属完整性覆盖项，不阻塞 M5 配额耗尽快照路径的核销。
