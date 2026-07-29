# M5 T4→EmitInterrupt #521 定向复审

## 结论

**FAIL。** #521 新增了 `report.submit → RecordReport` 的生产入口和 report rate bucket，但没有把 Report 每日 Interrupt 子配额、Report charge 或 quota-exhaustion 发射接入该入口；`RecordReportQuotaExhaustion` 仍只有测试调用。0019 的 binding triggers 也仍不是 closed union。#516 指出的 §3.6 exact golden / Gate 完整矩阵没有新增测试，缺口保持不变。migration `0019` 本身编号唯一，但当前全量测试另因 0020 合入后 schema version 断言仍为 19 而失败。

## 评审基线

- Issue：#528（含 comments，无评论）；被评审 Issue：#521（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-509-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-509-rereview-pi-gpt-5.6-sol.md) **FAIL**
- #521 实现 commit：`f7fbd0f12f18f67b54dab759d16338e9ccc2147d`；merge commit：`9b7c4f7`
- 当前基线：`0cde4ae`（`origin/main`）
- 判定基准：`docs/specs/interrupt.md` §3.6、`docs/specs/storage.md` §6.4、§12.2.1、`docs/specs/report.md` §6–§7
- #521 实现范围：4 个文件，`+251/-6`；没有新增或修改功能测试

## 阻断项

### P1-1：Report quota production owner 仍未接通

`internal/controlplane/server.go` 已把 `report.submit` 接到 `DB.RecordReport`，`internal/storage/report.go:76-126` 也在 receipt/event 事务中消费 report rate token；这关闭了“完全没有生产 Report 写口”的一部分缺口。但每日 Report Interrupt 子配额仍未实现：

1. `RecordReport` 没有读取或更新 `budget_counters` / `kind=report` charge，新增的 `report_receipts.report_interrupt_charge_entry_id` 从未写入。
2. 全仓 `RecordReportQuotaExhaustion` 仍只有 `internal/storage/interrupt_test.go` 的直接测试调用；`RecordReport` 不调用它，生产流永远到不了 quota-exhaustion `failure_review`。
3. blocker 在 `internal/storage/report.go:126` 已提交 token、receipt 和领域 event，随后才在 `:166` 调 `EmitInterrupt`，并在 `:167-169` 静默忽略发射或 receipt-link 更新失败。这不满足 report §7 对“子配额可用时 token + Report charge + receipt/event + Interrupt/admission/outbox 同事务”的要求，也使结构拒绝无法回滚领域写入。
4. 子配额满的规定结果（提交 token + 唯一 exhaustion/security event，不写 receipt/key，再 best-effort 专用发射并返回 closed conflict）没有生产分支、并发线性化或崩溃重放证据。

因此 Report production owner 为 **PARTIAL / NO**；存在 RPC 和普通 rate-token CAS 不能核销 #516 要求的 Report quota owner。

### P1-1：0019 binding guards 仍非 closed union

`0019_report_production_owner.sql` 只对两个 `failure_review` arm 检查少数字段类型，并只禁止 quota arm 混入五个具名 attempt 字段：

- `design_approval`、`guardrail_violation`、`code_review`、`agent_blocked`、`merge_conflict`、`startup_stall` 仍没有 reason/arm 映射和 required/null/extra-field shape；例如合法 arm 名配错误 reason 仍可通过。
- `failure_review_attempt.retry_kind` 只要求 JSON type 为 text，不限制 closed enum；没有落实 `new_attempt` terminal pair 或 `gate_recheck` 的 change/head、terminal NULL 与 exact checked-head 组合。
- attempt arm 可混入 quota 字段；两个 arm 都可混入任意未列出的 extra fields。
- trigger 不重算 `binding_digest`，任意非 NULL digest 仍可通过；注释所称 application 校验不能约束其他 SQL writer。
- #521 没有新增 malformed/cross-arm/digest/binding INSERT failure 的拒绝与全事务回滚测试。

因此 binding constraints 和负向矩阵均为 **NO**。

### P1-4：§3.6 exact golden / Gate matrix 未关闭

#521 没有修改 `internal/storage/interrupt_test.go`、`internal/gate/*_test.go` 或其他 T4 功能测试，故 #516 的缺口原样保留：

- attempt 特殊 fragment 仍只直测 `acceptInterruptT4`；没有经 `EmitInterrupt` 逐字节断言 canonical input/output、normal persisted headline/brief/options/links、安全事件 link、`recommended_action=hold` 和三种 invalid output 的唯一 fallback bytes。
- quota 用例仍未逐字节断言完整 fallback renderer、canonical T4 input/output、persisted `headline/options_json/links_json`。
- cross-arm 用例仍主要直调 validator，没有证明错误 variant/FK 在全部领域写入前整体回滚。
- Gate 仍缺 normal/invalid 完整 options/links/generation/calibration/evaluation/operation 身份矩阵及 Gate `failure_review` attempt vector。

因此 P1-4 为 **PARTIAL / NO**。

## 未回退项与 migration

- #516 已核销的 failed attempt/generation 校验和真实 32 位 lowercase-hex security event 身份，静态检查及定向测试未见回退。
- migration 文件编号当前为唯一的 `0001`–`0020`，`0019_report_production_owner.sql` 没有重复版本号。
- 但 #522 的 `0020` 已在当前基线，`internal/storage/storage_test.go` 仍断言 schema version/count 为 19；这不是 0019 重号，却使当前全量门禁失败。

## 验证

- `go test ./internal/storage -run 'TestEmitInterrupt|TestRecordReportQuota|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**PASS**。
- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay ./internal/controlplane`：**FAIL**，仅 `TestMigrationRecordedAndIdempotent` 报 `SchemaVersion = 20, want 19`。
- `go test ./...`：**FAIL**，同一 schema version 断言；其余 package 通过。

## 关闭清单

| #528 / #521 条件 | 结果 |
|---|---|
| 对照 #516 FAIL / #521 | **YES** |
| Report `report.submit → RecordReport` 生产入口 | **YES** |
| Report 普通 rate-token CAS | **YES** |
| Report quota production owner / charge / exhaustion 流 | **NO** |
| P1-4 §3.6 exact golden 完整 | **NO** |
| P1-4 Gate normal/invalid/failure-review matrix 完整 | **NO** |
| binding closed union / digest / 组合 guards | **NO** |
| binding 拒绝与全事务回滚矩阵 | **NO** |
| failed-generation 与 security-event 已关闭子项未回退 | **YES** |
| migration 0019 无重复版本 | **YES** |
| 定向测试全绿 | **YES** |
| 全量测试全绿 | **NO**：schema version 20/19 断言不一致 |
| #521 可核销 | **NO** |

**最终：FAIL。**
