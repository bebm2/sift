# M5 T4→EmitInterrupt #533 定向复审

## 结论

**FAIL（3×P1）。** #533 已让 `RecordReport` 进入 Report 日子配额 counter，并写 Report charge；触顶时也会从生产入口调用 `RecordReportQuotaExhaustion`。但可用配额的 blocker 仍先提交 token/charge/receipt/event，再另事务发 Interrupt、第三次写 receipt link，违反 Report 的原子结果矩阵；触顶分支也先单独提交 token，随后才建立 exhaustion，留下崩溃缺口，并会让同日后续请求重复消费 rate token。#528 要求的 §3.6 / Gate 矩阵没有新增测试。0022 的 guards 只检查对象字段数和 digest 外形，仍允许错误字段集合、伪 digest 与不完整 option 集。因此 #533 不能核销 #528。

## 评审基线

- Issue：#540（含 comments，无评论）；被评审 Issue：#533（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-521-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-521-rereview-pi-gpt-5.6-sol.md) **FAIL**
- #533 实现 commit：`52ebc9a58819874f6c0f1954ec3f379067eb4098`；merge commit：`2c7ebda`
- 当前基线：`73c7476`（`origin/main`，另含后续 migration 0023）
- 判定基准：`docs/specs/interrupt.md` §3.6、`docs/specs/storage.md` §6.4/§12.2.1、`docs/specs/report.md` §6–§7
- #533 仅修改 3 个文件，`+142/-42`；没有新增或修改功能测试

## 阻断项

### P1-1：Report quota 已有生产调用，但事务与重放语义仍不闭合

`internal/storage/report.go:122-152` 现在读取冻结配置，创建/更新 `kind=report` counter，并在额度可用时写 `report_agent_blocked` charge；`:141-149` 在触顶时从生产 `RecordReport` 调用 `RecordReportQuotaExhaustion`。这是正确增量，但仍有两类 P1：

1. **可用配额的 blocker 被拆成三次提交。** `:156-164` 先提交 rate token、Report charge、receipt 和领域 event，`:172-180` 才另调 `EmitInterrupt`，`:182` 又单独回填 `direct_interrupt_id`。任何 worktree 查询、结构校验、T4、attention、binding、outbox 或 link 更新失败，都会留下已占 report key、已消费 token/Report quota 且已写 charge/event、却没有完整 Interrupt/link 的部分状态。`0022:4-13` 甚至专门放宽 append-only trigger 来容许该事后 link。规格要求完整 blocker 的这些事实与 `EmitInterrupt` 五件事同事务，结构拒绝则全部回滚。
2. **quota-exhaustion 安全事实不与 rate token 同一线性化事务。** `report.go:141-145` 先提交仅含 rate-token 更新的事务，再由 `RecordReportQuotaExhaustion` 建立 exhaustion/security event。两者之间崩溃会留下“token 已扣、exhaustion 不存在”；重放会再次扣 token。已有同日 exhaustion 时，`RecordReport` 也不在 rate CAS 前复用它，因此每个后续触顶请求都会再次消费 token后才重用 exhaustion。规格只允许线性化当日唯一 exhaustion 的请求消费一次 token，并要求并发冲突回滚 tentative CAS 后重读。
3. 没有 production `RecordReport` 测试。全仓具名 quota 测试仍只有直接调用 `RecordReportQuotaExhaustion` 的 `TestRecordReportQuotaExhaustionUsesSystemEventIdentity`；未覆盖可用配额原子回滚、触顶崩溃点、重复/并发 token、receipt/key 不占位及 production RPC closed outcome。

因此 quota wiring 为 **PARTIAL / NO**。

### P1-2：0022 binding guards 仍不是 closed union

`0022_report_interrupt_closure.sql` 增强了 arm/reason 枚举、failure-review 字段数、new-attempt terminal pair 和 quota exhaustion 复合身份，这是正确增量；但 SQL writer 仍可写入不合法 binding：

- `:33-40` 对六个普通 arm 只检查总字段数，`:41` 只统一检查 `run_id` 类型，没有逐 arm 检查 required key 名、字段类型、组合 FK 或 Interrupt 所属 Run。例如 `design_approval` 的 `{arm,run_id,x}` 与正确对象同为三个字段，可通过这些新 guards。
- `:21-22` 只检查 `binding_digest` 是 64 位 lowercase hex，未验证它等于 `SHA-256(canonical_json(binding_json))`；任意伪 digest 可入库。
- attempt 的 failed-attempt 旧 trigger仍存在，但没有把 binding Run 与 bound Interrupt Run 对齐；`gate_recheck` 的 Change/head 也只检查 text 类型，没有组合 FK/exact checked-head 约束。
- `:48-52` 的 option guard只拒绝集合外 ID；空数组、缺项、重复项和重排都不会命中，不能证明 attempt 恰为 `retry,reject,hold`、quota 恰为 `reject,hold` 且同序。
- #533 没有新增 malformed/cross-arm/digest/options/FK SQL insert 拒绝及全事务回滚测试。

因此 binding guards 为 **NO**。

### P1-4：§3.6 exact golden / Gate matrix 没有新增交付

#533 的 diff只有 `report.go`、migration 0022 与 schema version 断言，没有修改任何功能测试；故 #528 的缺口不能因现有绿测自动核销：

- attempt / quota 测试仍未从 `EmitInterrupt` 逐字节捕获并断言完整 canonical T4 input/output 与全部 persisted headline/brief/options/links bytes；cross-arm 负例仍主要直调 `validateFailureReviewVariant`，没有证明 SQL/FK 错配在所有领域写入前回滚。
- Gate 现有 `TestGateT4NormalAndInvalidFallbackPreserveEmissionIdentity` 只有 normal 与一次 option 重排 fallback，未覆盖完整 options/links、三类 invalid output、evaluation/calibration/operation 逐项身份及 Gate `failure_review` attempt vector。
- Report production 路径没有连接上述 quota golden/matrix，也没有 binding guard 的负向矩阵。

因此 P1-4 为 **PARTIAL / NO**。

## migration 与未回退项

- migration 文件为唯一连续的 `0001`–`0023`；`0022_report_interrupt_closure.sql` **无重复版本**。
- `TestMigrationRecordedAndIdempotent` 已在 #533 更新至 22；当前 main 因后续 0023 已同步至 23，全量测试通过。
- #528 已核销的 failed attempt/generation 基础校验、32 位 lowercase-hex security event identity，以及 quota `reject,hold` T4 dispatch，未见回退。

## 验证

- `go test ./internal/storage -run 'TestEmitInterrupt|TestRecordReportQuota|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt' -count=10`：**PASS**。
- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay ./internal/controlplane`：**PASS**。
- `go test ./...`：**PASS**。
- migration 编号脚本：23 个版本，`0001`–`0023`，重复集合为空。
- `git diff --check`：**PASS**。

绿测不改变上述判定：#533 没有新增覆盖关闭条件的功能测试，现有 suite 无法观察拆分事务、重放 token 与 malformed SQL binding。

## 关闭清单

| #540 / #533 条件 | 结果 |
|---|---|
| 对照 #528 FAIL / #533 | **YES** |
| Report quota production 调用可达 | **YES** |
| Report charge/counter 基础写入 | **YES** |
| 完整 blocker 单事务与失败回滚 | **NO** |
| exhaustion + rate-token 线性化、崩溃/并发重放 | **NO** |
| P1-4 §3.6 exact golden 完整 | **NO** |
| P1-4 Gate normal/invalid/failure-review matrix 完整 | **NO** |
| binding closed shape/digest/组合 FK/options guards | **NO** |
| binding 拒绝与全事务回滚矩阵 | **NO** |
| migration 0022 无重复 | **YES** |
| 定向测试全绿 | **YES** |
| 全量测试全绿 | **YES** |
| 结论写入 `docs/reviews/` | **YES** |
| 仅 worktree | **YES** |
| #533 可核销 | **NO** |

**最终：FAIL。**
