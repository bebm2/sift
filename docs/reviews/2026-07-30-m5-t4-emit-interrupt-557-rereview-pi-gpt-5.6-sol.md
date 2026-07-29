# M5 T4→EmitInterrupt #557 定向复审

## 结论

**FAIL（3×P1）。** #557 修正了 production JOIN，直接 blocker 现可原子创建 receipt/charge/Interrupt 且保持 Run `running`；顺序重放也不再重复扣 rate token。migration 0028 编号唯一且 SchemaVersion 已同步。但 no-transition 被实现为可由任意存储调用方设置的公开 `ReportOnly` 开关，并未绑定到真实 Report receipt；0028 仍接受非 canonical、错类型和未绑定 Change/head 的 direct-SQL binding；#552 要求的 golden/Gate/SQL/crash-concurrency 矩阵也未交付。因此不能核销 #552。

## 评审基线

- Issue：#564（含 comments，无评论）；被评审 Issue：#557（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-545-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-545-rereview-pi-gpt-5.6-sol.md) **FAIL（4×P1）**
- #557 实现 commit：`3b5a5110c05fad3d59741baaf91f7aa3c28747ef`；merge commit：`62dded6cf3701b08f7d6b14974aa8d8cfa6dee81`
- 当前基线：`de9cbae`（`origin/main`，另含后续 migration 0029）
- 判定基准：`docs/specs/interrupt.md` §3.6/§5.1/§6、`docs/specs/storage.md` §6.4/§12.2/§12.2.1
- #557 修改 5 个文件，`+184/-25`；新增 1 个串行 Report 测试，未修改 T4 golden、Gate 或 SQL rejection 测试

## 已关闭项

### JOIN、直接 blocker 与顺序 quota replay

`internal/storage/report.go:60,185,198,286` 已全部改为 `JOIN runs r ON r.id=a.run_id`，不再使用无效的 `USING(id)`。`recordBlockerReport` 在 `:195` 传入 no-transition 标志，新增的 `TestRecordBlockerReportKeepsRunningRun` 通过生产 `RecordReport` 证明 receipt、Interrupt 均创建且 Run 保持 `running`。

`commitReportQuotaExhaustion:291-297` 先读取当日 exhaustion，再决定是否调用 `consumeReportTokenTx`；新增测试连续提交两个触顶请求，证明已存在 exhaustion 的顺序重放不再扣第二枚 rate token。前次 P1-1 的生产断路、直接 blocker 状态错误，以及 P1-2 的**顺序重放**缺陷均已修复。

## 阻断项

### P1-1：Report-only 状态特权未绑定到 Report owner

`internal/storage/interrupt.go:186-188` 把 `ReportOnly` 放进公开的 `EmitInterruptCmd`；`:268` 只检查 `reason=agent_blocked` 与 `source=agent`，`:588` 即据此跳过 `waiting_human` 转移。它不要求调用来自 `RecordReport`，也不要求 `Generation.ReportID` 命中同一 `(run,attempt)` 的 `report_receipts`。0028 对 `agent_blocked.report_id` 也只检查它是 text。

临时定向 repro 直接调用 `DB.EmitInterrupt`，传 `ReportOnly=true`、`SourceAgent` 与不存在的 `report_id=not-a-receipt`：调用成功创建 open Interrupt/binding/outbox，Run 保持 `running`，而 `report_receipts` 仍为 0。临时测试已删除。这违反 storage §12.2.1 “Report-only 只允许 RecordReport 调用”的 owner 边界，也把普通 `agent_blocked` 的状态不变量降为调用方可选布尔值。

生产 `RecordReport` 的正向路径虽已修复，但 no-transition 权限闭包仍为 **NO**。

### P1-2：0028 binding 仍不是 canonical、typed、组合绑定的 closed union

`0028_emit_interrupt_binding_p1_closure.sql` 有正确增量：恢复 failure-review terminal/null 关系，校验完整 failure-review options，并补 design/attempt 的部分 identity。但仍有可实际写入的 bypass：

1. `interrupt_binding_canonical_json_insert:18-21` 仅断言 `binding_json=json(binding_json)`；SQLite `json()` 只压缩文本，不按 canonical serializer 排序 object keys。`:23-30` 的 key-order trigger只覆盖 agent/startup/failure 两个 variant，完全未覆盖 design、guardrail、code-review、merge-conflict。
2. `interrupt_binding_exact_shape_insert:8-15` 在替换 0027 trigger 时删掉了大部分字段类型判断。startup/failure 的 `attempt_no`/`generation`、design snapshot、Change/head/digest 等均没有完整 `json_type` 拒绝。
3. `interrupt_binding_identity_insert:32-62` 没有 guardrail、code-review 或 merge-conflict 分支；故没有断言 Change 属于 Interrupt 的 Run、head 是 exact checked head，也没有闭合 guardrail head/rule/path identity。agent-blocked 的 `report_id` 同样没有 receipt 组合绑定。
4. `interruptEffectBinding` 给 `agent_blocked` 新增 `report_id`，而 active storage §6.4 仍定义 `agent_blocked(run_id,attempt_no,generation)`；实现没有同步规格，当前 contract 自相矛盾。

两个临时 direct-SQL repro 均被当前 schema 接受：一是把合法 code-review binding 改为 compact 但反 canonical key order并重算 digest；二是把 startup `attempt_no/generation` 改成 JSON string `"1"` 并重算 digest。临时测试均已删除。由此 canonical digest、required type 与 SQL rejection closure 都是实际 **NO**，不只是缺测试。

### P1-3：§3.6 golden、Gate、SQL 与 quota crash/concurrency 矩阵仍未交付

#557 只新增 `internal/storage/report_test.go` 的一个串行 happy/replay 测试，没有修改 `interrupt_test.go`、`gate_test.go` 或 SQL constraint tests：

- `TestAcceptInterruptT4AttemptGolden` 仍只测接纳函数和部分 brief，不逐字节捕获完整 canonical T4 input/output、persisted headline/brief/options/links；
- quota 测试仍未逐层重算并断言 §3.6 的完整 input/output bytes；
- Gate 仍只有 normal 与 option-reorder fallback 两例，没有 unknown fragment/unknown option、完整 links/options、evaluation/calibration/operation identity 与 Gate failure-review attempt vector；
- 0028 没有 malformed/cross-arm/type/canonical/Change-head/options/FK SQL rejection 与整事务回滚测试；上述两个接受型 repro 正说明缺口真实存在；
- Report 没有 publish/binding/admission 任一步失败的全事务回滚，也没有首次 quota exhaustion 的 crash/concurrent-writer 矩阵。新增测试只能证明单 goroutine 顺序重放。

因此 #552 的 P1-4 以及本次关闭条件明确要求的 binding/Gate/SQL matrix 仍为 **NO**。

## migration

- 当前 migrations 唯一连续为 `0001`–`0029`；`0028_emit_interrupt_binding_p1_closure.sql` 只有一份，无重复版本。
- #557 将 `TestMigrationRecordedAndIdempotent` 同步至 28；当前 main 因后续 0029 已同步至 29。

## 验证

- `gh issue view 564`、`gh issue view 564 --comments`、#552/#557 及 comments：已读取。
- 临时 Report-only owner bypass repro：**PASS（稳定复现无 receipt 仍可 no-transition 发射）**；临时文件已删除。
- 临时 direct-SQL canonical/type repro：**PASS（两种非法 binding 均被接受）**；临时文件已删除。
- `go test ./internal/storage -run 'TestRecordBlockerReportKeepsRunningRun|TestEmitInterrupt|TestAcceptInterruptT4|TestGate|TestMigrationRecordedAndIdempotent' -count=10`：**PASS**。
- `go test ./internal/storage ./internal/gate ./internal/brain ./internal/replay ./internal/controlplane -count=1`：storage/gate/brain/replay PASS；controlplane **FAIL** 于已知 doctor fixture 时序（agent CLI `signal: killed`）。
- `go test ./...`：除同一既知 doctor fixture 时序（本次为 tmux `signal: killed`）外其余包 PASS。
- `go vet ./...`：**PASS**。
- migration 编号脚本：29 个版本，`0001`–`0029`，无缺号、无重复。
- `git diff --check`：**PASS**。

## 关闭清单

| #564 / #557 条件 | 结果 |
|---|---|
| 对照 #552 FAIL / #557 | **YES** |
| production JOIN 修复 | **YES** |
| production blocker 保持 Run running | **YES** |
| blocker receipt/charge/Interrupt/outbox 正向同事务 | **YES** |
| Report-only owner/receipt binding | **NO** |
| quota exhaustion 顺序重放不重复扣 token | **YES** |
| quota crash/concurrency matrix | **NO** |
| binding canonical/type/组合 FK/options closed union | **NO** |
| binding SQL rejection与全事务回滚矩阵 | **NO** |
| §3.6 exact golden 完整 | **NO** |
| Gate normal/invalid/failure-review matrix 完整 | **NO** |
| migration 0028 无重复 | **YES** |
| SchemaVersion 同步 | **YES** |
| 定向测试通过 | **YES** |
| 全量测试全绿 | **NO（既知 doctor 时序 flake）** |
| 结论写入 `docs/reviews/` | **YES** |
| 仅 worktree | **YES** |
| #557 可核销 | **NO** |

**最终：FAIL。**
