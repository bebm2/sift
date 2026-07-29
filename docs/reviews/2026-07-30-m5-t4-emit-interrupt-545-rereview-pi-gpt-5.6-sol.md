# M5 T4→EmitInterrupt #545 定向复审

## 结论

**FAIL（4×P1）。** #545 引入 `emitInterruptHooks`，把 receipt/event、Report charge、Interrupt/admission/outbox 与 receipt link 放进同一 SQLite 事务，这是正确方向；0025 也开始核验 binding JSON 的原始字节 digest、字段名和 failure-review option ID 顺序。但生产 blocker 入口当前被无效 SQL join 全部拒绝；修正 join 后仍会错误地把 running Run 转为 `waiting_human`。quota exhaustion 重放仍会重复消费 rate token。0025 还回退了 0022 已有的 failure-review terminal/null guards，并未闭合 canonical digest、组合 FK 或完整 option 对象。#540 要求的 P1-4 golden/matrix 没有新增测试。因此 #545 不能核销 #540。

## 评审基线

- Issue：#552（含 comments，无评论）；被评审 Issue：#545（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-533-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-533-rereview-pi-gpt-5.6-sol.md) **FAIL**
- #545 实现 commit：`054c4628c8d51a00a2fbbc11a4b01f82772d0c63`；merge commit：`3cc98b8d0702953a864fe02ac2bee5d00cd182f5`
- 当前基线：`4587f03`（`origin/main`，另含后续 migration 0026）
- 判定基准：`docs/specs/interrupt.md` §3.6/§6、`docs/specs/storage.md` §6.4/§12.2.1、`docs/specs/report.md` §6–§7
- #545 实现 commit 修改 5 个文件，`+260/-18`；测试只更新 SchemaVersion 断言，没有新增功能测试

## 阻断项

### P1-1：生产 blocker 入口不可达，且 Report-only 状态语义仍错误

`internal/storage/report.go:182`、`:195`、`:283` 使用 `attempts ... JOIN runs r USING(id)`；`attempts` 没有 `id` 列，正确关系是 `r.id=a.run_id`。真实 SQLite 对该查询固定返回 `cannot join using column id - column not present in both tables`，而 `recordBlockerReport` 在 `:182-184` 将它映射为 `report: unauthorized`。因此任何合法 blocker 都到不了新事务，更不能创建 receipt、charge 或 Interrupt。仓库没有一条调用 `RecordReport` 的测试，所以绿测未观察到该生产断路。

即使修正 join，`recordBlockerReport:192` 仍以普通 `agent_blocked` 调用 `emitInterruptHooks`，没有 Report-only no-transition discriminator。`interrupt.go:546-589` 只对 quota `failure_review` 保持 running；普通 `agent_blocked` 会落入 `:582-588`，把 Run 转为 `waiting_human` 并增加 version。这违反 Report §7 与 Interrupt §6 的固定矩阵：Report 直接 blocker 必须保持 Run running。

新 hooks 确实让可用配额分支的 receipt/event、rate token、Report charge、Interrupt、binding、admission/outbox 和 link 具备同事务结构；但生产不可达且状态错误，故 atomic Report 关闭条件仍为 **NO**。

### P1-2：quota exhaustion 重放仍重复扣 rate token

可用配额尝试在 `report.go:206-222` 的事务内扣 token 后发现子配额已满并回滚，随后 `commitReportQuotaExhaustion` 建立独立安全事务，这个两段方向符合规格。但该函数在 `:286` 先调用 `consumeReportTokenTx`，到 `:293-295` 才查询当日既有 exhaustion。故同日第二次及后续 blocker 会再次消费 token，再复用同一 exhaustion；崩溃后以相同 key 重放也一样重复扣 token。

并发首次触顶时，loser 在 `:309` 命中唯一冲突会回滚 tentative token，但函数直接返回存储错误，没有按 storage §12.2.1 要求重读 winner、复用其事实并继续 generation-key 发射/返回固定 closed conflict。因而“只有线性化当日 exhaustion 的请求消费一次 token”仍未成立，触顶崩溃/并发矩阵也没有测试。

### P1-3：0025 binding guards 仍非 closed union，并回退 0022 约束

`0025_emit_interrupt_binding_closed_union.sql` 有若干正确增量：验证字段名集合、Interrupt/Run/reason 归属、exhaustion 复合身份，并检查 raw `binding_json` 的 SHA-256。但仍有 P1 缺口：

1. `:38` 替换 0022 的 `interrupt_binding_exact_shape_insert` 后，只校验 failure-review 的字段集合、整数类型和 `retry_kind` 枚举；0022 已有的 `new_attempt` terminal pair 等于 `(attempt_no,generation)`、change/head 为 NULL，以及 `gate_recheck` terminal pair 为 NULL、change/head 为 text 的检查被删除。0017 的剩余 trigger 只证明 failed attempt 存在，故错 terminal pair/null 组合可进入数据库。
2. `:23-25` 只对 startup/failure attempt 做组合查询；`agent_blocked` 的 `(run_id,attempt_no,generation)`、Change/head arms 的所属 Run 与 exact checked-head、design/task snapshot 等组合身份仍未由 SQL guard 闭合。
3. `:15` 只证明 digest 等于提交的原始 JSON 文本，不证明该文本是 `canonical_json(binding_json)`；改变空白/键顺序并重算 digest 的直接 SQL writer 仍可写入语义相同但非 canonical 的 binding。
4. `:44-47` 只比较 failure-review option 数量和 ID 顺序，不比较 canonical `label/effect/risk` 完整对象；同 ID、错误 effect 的 options 仍可绑定。

#545 没有新增 malformed/cross-arm/digest/options/FK SQL 拒绝及全事务回滚测试。因此 binding guards 为 **NO**。

### P1-4：§3.6 exact golden / Gate / Report matrix 未交付

#545 没有修改 `interrupt_test.go`、`gate_test.go` 或新增 Report 测试。现有测试仍只覆盖部分结构与渲染结果，未逐字节捕获 `EmitInterrupt` 的完整 canonical T4 input/output、persisted headline/brief/options/links；Gate 仍只有 normal 与单个 option 重排 fallback，没有三类 invalid output、完整 links/options、evaluation/calibration/operation 身份及 Gate failure-review attempt vector。Report production 原子回滚、quota crash/replay/concurrency 和 binding SQL 负向矩阵均为空。

因此实现方已注明仍为 NO 的 P1-4 在代码库中也确实为 **NO**。

## migration 与未回退项

- 当前 migration 唯一连续为 `0001`–`0026`；`0025_emit_interrupt_binding_closed_union.sql` **无重复版本**。
- `TestMigrationRecordedAndIdempotent` 在 #545 更新至 25；当前 main 因后续 0026 已同步至 26。
- #533 已接通的 Report counter/charge 基础结构，以及 #545 新增的同事务 hooks/raw digest/字段名 guards，未见删除；但不能抵消上述生产断路和闭包缺口。

## 验证

- 临时定向 repro 执行 `attempts JOIN runs USING(id)`：**PASS（稳定复现 SQL logic error）**；临时测试文件已删除。
- `go test ./internal/storage -run 'TestEmitInterrupt|TestRecordReportQuota|Test.*T4|Test.*Report.*Quota|Test.*Gate.*Interrupt|TestMigrationRecordedAndIdempotent' -count=10`：**PASS**。
- `go test ./internal/brain ./internal/storage ./internal/gate ./internal/replay ./internal/controlplane`：首次 **FAIL** 于既知 doctor fixture 时序（agent CLI `signal: killed`），其余包 PASS。
- `go test ./...`：**PASS**。
- migration 编号脚本：26 个版本，`0001`–`0026`，无缺号、无重复。
- `git diff --check`：**PASS**。

绿测不改变判定：suite 没有调用生产 `RecordReport`，也没有覆盖 #540 指定的 crash/concurrency、binding 与 golden/Gate 矩阵。

## 关闭清单

| #552 / #545 条件 | 结果 |
|---|---|
| 对照 #540 FAIL / #545 | **YES** |
| Report 完整 blocker 同事务结构 | **PARTIAL** |
| production `RecordReport(blocker)` 可达 | **NO** |
| Report-only blocker 保持 Run running | **NO** |
| exhaustion + rate-token 线性化、崩溃/并发重放 | **NO** |
| binding exact closed shape/canonical digest/组合 FK/options | **NO** |
| binding 拒绝与全事务回滚矩阵 | **NO** |
| P1-4 §3.6 exact golden 完整 | **NO** |
| P1-4 Gate normal/invalid/failure-review matrix 完整 | **NO** |
| migration 0025 无重复 | **YES** |
| 定向测试全绿 | **YES** |
| 全量测试全绿 | **YES** |
| 结论写入 `docs/reviews/` | **YES** |
| 仅 worktree | **YES** |
| #545 可核销 | **NO** |

**最终：FAIL。**
