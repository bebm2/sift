# M5 T4→EmitInterrupt #569 定向复审

## 结论

**FAIL（2×P1）。** #569 已移除公开 `EmitInterruptCmd.ReportOnly`，把 no-transition 收进 Report 专用的包内入口，并由同一事务中先建立的 blocker receipt 约束 binding；这一项可核销。migration 0031 也补齐了各 arm 的类型与 canonical key order，但仍未把 `code_review`、`merge_conflict`、`guardrail_violation` binding 绑定到 Interrupt 所属 Run 的冻结 Change/head/policy 事实，direct SQL 可写入 canonical 但伪造的 Command effect identity。#564 明确要求的 exact T4、Gate、SQL rejection 与 crash/concurrency full matrix 亦未交付；#569 本身没有新增这些测试。因此不能核销 #564。

## 评审基线

- Issue：#576（含 comments，无评论）；被评审 Issue：#569（含 comments）
- 前次结论：[`2026-07-30-m5-t4-emit-interrupt-557-rereview-pi-gpt-5.6-sol.md`](2026-07-30-m5-t4-emit-interrupt-557-rereview-pi-gpt-5.6-sol.md) **FAIL（3×P1）**
- #569 实现 commit：`238038eb9f77d0bc9fbc9f40ba7859d512352b19`；merge commit：`0496b2a3b695605e0b7ec7f54ea0af6b77e9c55e`
- 当前基线：`c243c181a63e6709b57cc0bf61d7ed308e6da091`（`origin/main`，另含后续 migration 0032）
- 判定基准：`docs/specs/interrupt.md` §3.6/§5.1/§9.2、`docs/specs/storage.md` §6.4/§12.2/§12.2.1
- #569 修改 4 个文件，`+77/-21`；未修改 `interrupt_test.go`、`gate_test.go`、`report_test.go` 或 SQL constraint tests

## 已关闭项

### Public ReportOnly 与 receipt owner

`internal/storage/interrupt.go:179` 的公开 command 已不再暴露 `ReportOnly`。普通 `DB.EmitInterrupt` 固定经 `emitInterrupt(..., reportOnly=false)`；no-transition 只能走包内 `emitReportInterruptHooks`（`:450-457`），且限制为 `agent_blocked + SourceAgent`。`internal/storage/report.go:199` 是唯一调用点，其 `before` callback 在 binding 插入前完成 token/attempt 校验并创建 blocker receipt，`after` callback 再回填 `direct_interrupt_id`；任一步失败均回滚同一事务。

0031 的 `interrupt_binding_identity_insert`（`:45-47`）要求非 NULL `report_id` 命中同 Run、同 attempt 的 blocker receipt，并要求该 attempt generation 匹配。公开入口即使提供伪造 Report ID也只能走普通 `waiting_human` 转移，且 binding 会因 receipt 不存在而整笔拒绝。前次 P1-1 的 public no-transition privilege 已关闭。

## 阻断项

### P1-1：0031 仍未闭合 Change/head/policy binding identity

0031 的 shape/order trigger（`:14,16,32,34`）只证明 `code_review` / `merge_conflict` 字段是 canonical text；identity trigger（`:40-49`）完全没有这两个 arm，也没有 `guardrail_violation` arm。它没有断言：

- `code_review(change_id,head_sha,review_policy_snapshot_digest)` 是该 Interrupt 所属 Run 的 exact checked Change/head/policy；
- `merge_conflict(change_id,head_sha,conflict_digest)` 绑定同一 Run 的冻结 Change/head；
- `guardrail_violation(run_id,head_sha,rule_id,matched_paths_digest)` 的 head/rule/path identity 命中同一 Gate/Run 事实。

临时 direct-SQL repro 先由生产 `EmitInterrupt(code_review)` 建立合法对象，再克隆一条同 Run/reason 的 Interrupt，向其插入 shape、key order、digest 全部 canonical、但 `change_id/head_sha/review_policy_snapshot_digest` 全为伪造值的 binding。当前完整 schema 接受该 INSERT。临时测试已删除。此结果直接违反 storage §6.4 的“Change/head 组合 FK、exact checked head、错配 FK 一律回滚”，故 binding identity 仍为 **NO**。

0030 的注释称初发 code-review/merge 由 owner port 校验，不能替代 SQL guard：§6.4 明确要求写端口及 CHECK 共同保证，并把 direct-SQL rejection 列为固定验收；Command 后续只读取这条 durable binding，不能信任插入者自述。

### P1-2：#564 要求的 full matrix 仍未交付

#569 没有新增任何业务测试；唯一测试改动是 `storage_test.go` 更新 migration 版本及一个历史升级 fixture。现存覆盖仍与前次相同：

- `TestAcceptInterruptT4AttemptGolden`（`interrupt_test.go:189`）只测试接纳函数和 brief，未逐字节断言 §3.6 的完整 canonical input/output、persisted headline/brief/options/links；unknown fragment 向量仍缺；
- quota T4 测试未按同一 canonical serializer 逐层重算并比较完整 input/output bytes；
- `TestGateT4NormalAndInvalidFallbackPreserveEmissionIdentity`（`gate_test.go:146`）仍只有 normal 与 option reorder，两例未覆盖 unknown fragment/unknown option、完整 links/options、evaluation/calibration/operation identity 及 Gate failure-review attempt；
- 没有 0031 malformed/cross-arm/type/canonical/Change-head-policy/options/FK direct-SQL rejection 与整事务回滚矩阵；本次接受型 repro 证明这不是纯覆盖注记；
- `TestRecordBlockerReportKeepsRunningRun`（`report_test.go:8`）仍是串行 happy/replay；publish/binding/admission 失败回滚及首次 quota exhaustion crash/concurrent-writer 矩阵缺失。

Issue #569 的关闭条件 3 与 #576 明示“实现方自述 full matrix 仍 NO”，因此该项继续是阻断条件，而非可留到后续的测试注记。

## migration

- 当前 migration 唯一连续为 `0001`–`0032`；`0031_emit_interrupt_binding_t4_closure.sql` 只有一份，无缺号、无重复版本。
- 当前 `TestMigrationRecordedAndIdempotent` 已随后续 0032 同步至 `SchemaVersion=32`。

## 验证

- `gh issue view 576`、`gh issue view 576 --comments`、#564/#569 及 comments：已读取。
- 临时 direct-SQL wrong code-review identity repro：**PASS（稳定复现非法 binding 被接受）**；临时文件已删除。
- `go test ./internal/storage -run 'TestRecordBlockerReportKeepsRunningRun|TestEmitInterrupt|TestAcceptInterruptT4|TestGate|TestMigrationRecordedAndIdempotent' -count=10`：**PASS**。
- `go test ./...`：**PASS**。
- `go vet ./...`：**PASS**。
- migration 编号脚本：32 个版本，`0001`–`0032`，无缺号、无重复；0031 唯一。
- `git diff --check`：**PASS**。

## 关闭清单

| #576 / #569 条件 | 结果 |
|---|---|
| 对照 #564 FAIL / #569 | **YES** |
| Public `ReportOnly` 移除 | **YES** |
| Report no-transition 仅包内 owner 可达 | **YES** |
| blocker receipt/run/attempt identity guard | **YES** |
| binding canonical key order与字段类型 | **YES** |
| binding Change/head/policy 等组合 identity | **NO** |
| binding SQL rejection与全事务回滚矩阵 | **NO** |
| §3.6 exact T4 golden 完整 | **NO** |
| Gate normal/invalid/failure-review matrix 完整 | **NO** |
| quota crash/concurrency matrix | **NO** |
| 已关闭 JOIN/blocker/quota/0028 unique 未回退 | **YES** |
| migration 0031 无重复 | **YES** |
| SchemaVersion 同步 | **YES** |
| 定向测试通过 | **YES** |
| 全量测试与 vet 通过 | **YES** |
| 结论写入 `docs/reviews/` | **YES** |
| 仅 worktree | **YES** |
| #569 可核销 | **NO** |

**最终：FAIL。**
