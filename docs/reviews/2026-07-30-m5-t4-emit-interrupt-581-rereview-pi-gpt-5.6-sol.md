# M5 T4→EmitInterrupt #581 定向复审

## 结论

**FAIL（2×P1）。** #581 新增 migration 0034，但 binding identity 仍未闭合：为兼容缺少 calibration/Change/policy 的旧对象而加入的 `OR` 分支同样适用于迁移后的新 INSERT，canonical 但伪造的 `code_review` binding 仍可由 direct SQL 写入；当前 Gate 生产组装又没有填写 `review_policy_snapshot_digest`，会直接命中空 digest 放行分支。#576 要求的 SQL/T4/Gate/crash-concurrency 验收矩阵也未交付，且实现方在 PR #585 中已明确自述 “full matrix still open”。因此不能核销 #576。

## 评审基线

- Issue：#588（含 comments，无评论）；被评审 Issue：#581（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-569-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-569-rereview-pi-gpt-5.6-sol.md) **FAIL（2×P1）**
- #581 实现 commit：`0847d851f6fd12a8369981db0d39902e054e0884`；merge commit：`8436022`
- 当前基线：`4adc4d8`（`origin/main`，另含后续 migration 0035）
- 判定基准：`docs/specs/interrupt.md` §3.6/§5.1/§9.2、`docs/specs/storage.md` §6.4/§12.2/§12.2.1
- #581 只修改 `0034_emit_interrupt_binding_identity.sql` 与 `storage_test.go`，`+45/-4`；没有新增 SQL、T4、Gate、Report 或 crash/concurrency 业务测试

## 阻断项

### P1-1：0034 的 legacy 放行分支仍允许 forged binding

`0034_emit_interrupt_binding_identity.sql:25-30` 对 `code_review` / `merge_conflict` 使用宽松存在性谓词：

- `code_review` 只要 `i.calibration_id IS NULL`、`r.change_id IS NULL` 或 binding 自述 `review_policy_snapshot_digest=''` 任一成立，就不再比较 Change/head/policy；
- `merge_conflict` 只要 `r.change_id IS NULL` 就不再比较 Change/head；
- `:34-36` 对 attempt arms 也在 `i.attempt_no IS NULL` 时绕过 run/attempt/generation 比较；`failure_review(gate_recheck)` 仍没有 direct-SQL exact Change/head 校验。

这些条件没有把“历史行可保留”与“新 binding INSERT 必须闭合”分开。定向临时测试在当前完整 schema 上创建 calibration/Change 均缺失的 legacy-shaped `code_review` Interrupt，再插入 key order、类型和 SHA-256 digest 均 canonical、但 `change_id/head_sha/review_policy_snapshot_digest` 全伪造的 binding；INSERT 连续 10 次均被 0034 接受。临时测试已删除。这仍违反 storage §6.4 的错配 FK/direct-SQL 一律回滚要求。

生产路径也依赖该旁路，而非已经闭合后再为历史数据单独迁移：`internal/gate/interrupt.go:11-24` 只向 `InterruptGeneration` 写 `ChangeID` 与 `HeadSHA`，没有把本次 `in.EffectivePolicyHash` 写入 `PolicySnapshotID`。因此 `interruptEffectBinding` 持久化空 `review_policy_snapshot_digest`，0034 `:27` 的空 digest 分支直接放行；`TestGateT4NormalAndInvalidFallbackPreserveEmissionIdentity` 的 command 同样未填写该字段。当前合法测试通过并不能证明 exact policy identity，反而证明闭合依赖于放宽 SQL guard。

0034 新增的 value-type 和 reason/arm 检查可核销“类型/跨 reason”子项，但不能核销 Change/head/policy identity。

### P1-2：#576 的 full acceptance matrix 仍为 NO

PR #585 的实现自述明确写明 “Binding identity migration 0034; full matrix still open”，实际 diff 也只更新 migration 与 SchemaVersion。前次矩阵缺口原样存在：

- `TestAcceptInterruptT4AttemptGolden` 只断言接纳函数和 brief，并覆盖 option reorder/unknown option；没有完整 canonical input/output bytes、persisted headline/options/links 与 unknown-fragment 矩阵；
- Report-quota T4 没有按同一 canonical serializer 逐层重算并比较完整 input/output bytes；
- Gate T4 测试仍只有 normal 与 option reorder fallback 两例，未覆盖 unknown fragment/option、完整 links/options、evaluation/calibration/operation identity 及 Gate failure-review attempt；
- 没有 0034 malformed/cross-arm/type/canonical/Change-head-policy/options/FK direct-SQL rejection及整事务回滚矩阵；本次接受型 repro 证明这不是纯测试注记；
- `TestRecordBlockerReportKeepsRunningRun` 仍是串行 happy/replay，publish/binding/admission 失败回滚及首次 quota exhaustion crash/concurrent-writer 矩阵仍缺失。

#588 明示“实现方自述矩阵仍 NO”并要求严格核验，因此该项继续是阻断关闭条件。

## 已关闭项与 migration

- #569 已关闭的 public `ReportOnly` 移除、Report 专用包内 no-transition owner、receipt/run/attempt identity、0031 closed shape/order 未见回退。
- migration 当前唯一连续为 `0001`–`0035`，无缺号、无重复；`0034_emit_interrupt_binding_identity.sql` 只有一份。
- #581 合入时 SchemaVersion 已同步至 34；当前随 migration 0035 正确同步至 35。

## 验证

- `gh issue view 588` / `--comments`、#576/#581 及 comments、PR #585：已读取。
- 临时 direct-SQL legacy-shaped forged `code_review` binding repro，`-count=10`：**PASS（稳定复现非法 binding 被接受）**；临时文件已删除。
- `go test ./internal/storage ./internal/gate -run 'TestRecordBlockerReportKeepsRunningRun|TestEmitInterrupt|TestAcceptInterruptT4|TestGate|TestMigrationRecordedAndIdempotent' -count=10`：**PASS**。
- `go test ./...`：**PASS**。
- `go vet ./...`：**PASS**。
- migration 编号脚本：35 个版本，`0001`–`0035`，无缺号、无重复；0034 唯一。
- `git diff --check`：**PASS**。

## 关闭清单

| #588 / #581 条件 | 结果 |
|---|---|
| 对照 #576 FAIL / #581 | **YES** |
| binding canonical shape/type/reason guard | **YES** |
| binding exact Change/head/policy identity | **NO** |
| legacy compatibility 不削弱新 direct-SQL guard | **NO** |
| binding SQL rejection与全事务回滚矩阵 | **NO** |
| §3.6 exact T4 golden 完整 | **NO** |
| Gate normal/invalid/failure-review matrix 完整 | **NO** |
| quota crash/concurrency matrix | **NO** |
| 已关闭 ReportOnly/receipt/0031 项未回退 | **YES** |
| migration 0034 无重复 | **YES** |
| SchemaVersion 同步 | **YES** |
| 定向测试通过 | **YES** |
| 全量测试与 vet 通过 | **YES** |
| 结论写入 `docs/reviews/` | **YES** |
| 仅 worktree | **YES** |
| #581 可核销 | **NO** |

**最终：FAIL。**
