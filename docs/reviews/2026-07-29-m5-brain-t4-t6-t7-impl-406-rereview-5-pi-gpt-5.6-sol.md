FAIL

# M5 Brain T4/T6/T7 调用壳（#445）第五次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`e84cd32e52f5734a7f2aadd62f408c4aa0279b33`（PR #446）
> 评审对象：Issue #447、Issue #445 / PR #446、[`brain.md` §11–§13](../specs/brain.md)
> 前序报告：[`#440 第四次定向复审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-rereview-4-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、测试、规格或 WBS，未自修

## 1. 结论

**FAIL（#440 的两个关闭点各仍有一处证据缺口）。**

PR #446 已补入 category/replay 各三个非 `total_samples` 负计数、category 域内重复 `evidence_id`、semantic 域内重复 `entry_id`，并新增 window、category、evidence summary、replay summary、semantic material 各层的大部分 required 单字段残缺案例。所有已提交案例继续共用同一 `Shell.Call` 三零断言，并在每个子测试中创建独立 DB。T4 links 也已改为 `(target,label)` UTF-8 升序，round-trip 后仍以 `reflect.DeepEqual` 比较整对象。

但 T7 顶层 required 矩阵遗漏了 `aggregate_key`。新增矩阵覆盖了其余四个顶层字段，旧 `required_missing` 案例却保留 `aggregate_key`、同时缺少其他字段，因此没有任何案例单独删除或缺失 `aggregate_key`。`brain.md` §13.1 明确顶层五字段均 required；故“各层 required 残缺”及要求中每例三零仍未完整闭合。

T4 的 `BuildT4Input(in)` 又发生在最终 fixture 组装完成之前：测试先设置非平凡 links 并调用 `BuildT4Input`，随后才把 `candidate_options` 改成最终两项，再将该已变更对象交给 `T4FallbackOutput` round-trip。这样证明合法的是修改前对象，不是最终被逐字段比较的对象；最终非平凡冻结 skeleton 仍没有先由 `BuildT4Input` 接纳。该调用应位于所有 fixture 字段赋值之后、fallback round-trip 之前。

因此 #440 剩余 P1 尚不能由 #445 核销。

## 2. 剩余可执行 P1

1. 在现有 T7 table-driven `Shell.Call` 矩阵中增加顶层 `aggregate_key` 缺失案例，并沿用独立 DB 的 call/attempt/provider 三零断言。
2. 将 T4 的 `candidate_options` fixture 赋值移到 `BuildT4Input(in)` 之前，使被验证对象与随后 round-trip、`DeepEqual` 的最终对象完全相同。

## 3. 已通过项

- category 与 replay 的 `negative_samples < 0`、`leak_count < 0`、`false_block_count < 0` 均已有独立案例。
- window、category、evidence summary、replay summary、semantic material 的嵌套 required 字段已有逐字段残缺案例。
- 不同 category 间重复 `evidence_id` 与不同 semantic item 间重复 `entry_id` 已覆盖。
- 所有已提交 malformed 案例均经 `Shell.Call`；每个子测试使用独立 DB，并直接比较调用前后 Brain call/attempt 数及 provider requests，三者增量均为 0。
- T4 links 已按 `(target,label)` UTF-8 bytes 升序；fallback round-trip 后保留整对象 `reflect.DeepEqual`。
- PR #446 只修改 `internal/brain/t4t6t7_issue415_test.go`，未改 channel、`EmitInterrupt`、规格或 WBS。

## 4. 测试证据

- `go test ./internal/brain -count=1`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**；复现前序已记录的并行时序 flake：
  - `TestDoctorBaselineChecksConfiguredDependencies` 的 agent/tmux fixture `signal: killed`；
  - `TestLaunchWorkerWrapperCrashSuite` 未观察到 started marker。
- 两个失败分别单独运行均 **PASS**：
  - `go test ./internal/controlplane -run '^TestDoctorBaselineChecksConfiguredDependencies$' -count=1`
  - `go test ./internal/launchworker -run '^TestLaunchWorkerWrapperCrashSuite$' -count=1`
- `git diff --check ba02f8d..e84cd32`：**PASS**。
- PR #446 的四平台 build、schema drift、vet + test checks 均为 **SUCCESS**。

## 5. Issue #447 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| category/replay 各负计数字段已覆盖 | YES | 六个新增案例齐全 |
| 各层 required 残缺矩阵完整 | NO | 顶层 `aggregate_key` 缺失案例遗漏 |
| 域内 `evidence_id` / `entry_id` 重复已覆盖 | YES | category↔category 与 semantic↔semantic 均有案例 |
| 每个已提交 malformed 例 call/attempt/provider = 0 | YES | 共用独立 DB 的前后计数断言 |
| 要求中的每个 malformed 例均有三零证据 | NO | `aggregate_key` 缺失案例不存在 |
| T4 links 排序正确 | YES | `https://...` 位于 `sift://...` 之前 |
| 最终 T4 fixture 先经 `BuildT4Input` 再逐字段无损 | NO | `BuildT4Input` 后又改写 `candidate_options` |
| #440 剩余 P1 可核销 | NO | 两处证据缺口仍在 |
| 结论已写入 `docs/reviews/` | YES | 本报告 |
