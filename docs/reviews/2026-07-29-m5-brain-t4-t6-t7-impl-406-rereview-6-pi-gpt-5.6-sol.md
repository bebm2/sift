PASS

# M5 Brain T4/T6/T7 调用壳（#449）第六次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`7112c50ff69c9240cd2b93682906007138ccd037`（PR #450）
> 评审对象：Issue #451、Issue #449 / PR #450、[`brain.md` §11 与 §13](../specs/brain.md)
> 前序报告：[`#447 第五次定向复审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-rereview-5-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、测试、规格或 WBS，未自修

## 1. 结论

**PASS。**

#449 已关闭 #447 FAIL 的两个剩余证据缺口。

T7 table-driven `Shell.Call` malformed 矩阵新增 `missing_aggregate_key`：测试从完整合法的 `base()` canonical input 中只删除顶层 `aggregate_key`，其余顶层和嵌套字段保持完整。该案例与矩阵其余案例一样在自身子测试中创建独立 DB，并在调用前后比较 Brain call、attempt 数量，同时断言 fake provider request 数为零；因此该单字段残缺已有 call/attempt/provider 三零证据。

T4 lossless fixture 现已先完成 `AttemptNo`、fallback brief、brief fragments、links 和最终两项 `candidate_options` 的全部赋值，再调用 `BuildT4Input(in)`。调用成功后、`T4FallbackOutput(in)` 与 closed decode / `reflect.DeepEqual` 之间不再改写 `in`，故被合法性校验、round-trip 和整对象逐字段比较的是同一个最终 fixture。

PR #450 仅修改 `internal/brain/t4t6t7_issue415_test.go`，未改 channel、`EmitInterrupt` 或任何生产实现。#447 剩余 P1 可全部核销。

## 2. 测试与变更证据

- `go test ./internal/brain -count=1`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**PASS**。
- `git diff --check e234470..7112c50`：**PASS**。
- PR #450 的四平台 build、schema drift、vet + test checks 均为 **SUCCESS**。
- PR #450 实现提交 `bd30e8b`：仅 `internal/brain/t4t6t7_issue415_test.go`，2 additions / 1 deletion。

## 3. Issue #451 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| T7 顶层仅 `aggregate_key` 缺失案例存在 | YES | 从完整 `base()` 只删除 `aggregate_key` |
| 该案例使用独立 DB | YES | 每个 table subtest 内调用 `openShellDB(t)` |
| 该案例 call/attempt/provider 三零 | YES | 调用前后 trace 计数不变且 `provider.Requests == 0` |
| 最终 T4 fixture 含非平凡 `candidate_options` | YES | 最终对象含 `review`、`retry` 两个完整 option |
| 最终 T4 fixture 先经 `BuildT4Input` | YES | 所有字段赋值后调用，且成功 |
| Build、fallback round-trip 与 DeepEqual 对象完全相同 | YES | `BuildT4Input` 后不再改写 `in` |
| #447 剩余两点可由 #449 关闭 | YES | 两处证据缺口均已闭合 |
| 仅 worktree、未改实现 | YES | 本次只新增该评审报告 |
| 结论已写入 `docs/reviews/` | YES | 本报告 |

## 4. 剩余风险

无阻断风险。本结论仅核销 #447 指定的两个测试证据缺口，不扩张为 M5 整体实现或阶段门禁结论。
