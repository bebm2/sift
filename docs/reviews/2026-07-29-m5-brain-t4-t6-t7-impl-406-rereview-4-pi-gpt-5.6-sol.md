FAIL

# M5 Brain T4/T6/T7 调用壳（#436）第四次定向复审

> 日期：2026-07-29
> 评审人：pi × GPT-5.6-sol
> 评审基线：`b91552b211b2fcd099c3db10605657a1f24eaa38`（PR #439）
> 评审对象：Issue #440、Issue #436 / PR #439、[`brain.md` §11–§13](../specs/brain.md)
> 前序报告：[`#432 第三次定向复审`](2026-07-29-m5-brain-t4-t6-t7-impl-406-rereview-3-pi-gpt-5.6-sol.md)
> 范围纪律：只出报告；未修改实现、规格或 WBS，未自修

## 1. 结论

**FAIL（T7 malformed 矩阵与 T4 合法 skeleton 证据仍未闭合）。**

PR #439 明显补齐了大部分 #432 P1：所有已提交的 malformed T7 case 都经 `Shell.Call`、独立 DB，并直接比较 call/attempt 基线及 provider request；scope/subject/project/window、concrete/all、三组跨域 evidence ID、category/semantic 未排序均已覆盖。T4/T6/T7 六值 fallback source、详情防泄漏、T6 四档、T7 no-draft，以及三触点 valid output/source 也已有完整字段断言。

但 #432 明列的完整 malformed 矩阵仍有缺项。category 与 replay 都只测试了 `total_samples < 0`，没有分别测试 `negative_samples`、`leak_count`、`false_block_count` 为负；required 残缺也只有一份几乎空的顶层对象，没有逐 required 字段/嵌套 required 结构的缺失矩阵。全局 evidence ID 唯一性只覆盖 category↔replay、category↔semantic、replay↔semantic 三组跨域碰撞，没有覆盖两个不同 category 共用 `evidence_id` 或两个 semantic item 共用 `entry_id`。因此不能把“每个 malformed 例三零”读作完整矩阵已三零：已交付案例是三零，但要求中的案例仍不存在。

此外，T4 lossless 用例的非平凡 fixture 不是合法的冻结 skeleton：`internal/brain/t4t6t7_issue415_test.go:282` 把 `sift://...` link 放在 `https://...` link 之前，违反 `brain.md` §11.1 / `BuildT4Input` 要求的 `(target,label)` UTF-8 升序；该测试直接调用 `T4FallbackOutput`，没有先证明 fixture 可由 `BuildT4Input` 接纳。它只能证明任意 Go struct 的 canonical JSON round-trip，不能作为合法 T4 输入逐字段无损的关闭证据。

故 #432 剩余 P1 尚不能由 #436 核销。

## 2. 剩余可执行 P1

### P1-1：补齐 T7 malformed Shell.Call 三零矩阵

在现有 table-driven 测试中至少补入：

- category/replay 各自的 `negative_samples < 0`、`leak_count < 0`、`false_block_count < 0`；
- closed schema 各层 required 字段残缺矩阵，而非仅一份几乎空的顶层样例；
- 不同 category 间重复 `evidence_id`、不同 semantic item 间重复 `entry_id`。

每例继续使用独立 DB，经 `Shell.Call` 并断言 `brain_calls += 0`、`brain_attempts += 0`、provider requests `+= 0`。

### P1-2：让 T4 无损 fixture 先满足输入契约

将 links 按 `(target,label)` 排序，并在 round-trip 前调用 `BuildT4Input(in)` 证明该非平凡 fixture 是合法冻结 skeleton；随后保留整对象逐字段等值断言。

## 3. 已通过项

- 已提交的 23 个 malformed case 均经 `Shell.Call`，每例独立 DB，call/attempt/provider 增量均为 0。
- scope、subject、project、window 漂移，以及 concrete/all 类别漂移、all nil/空/缺类/多类已覆盖。
- category 与 replay 的三类不等式越界已覆盖。
- 三组跨域 evidence ID 冲突及 category/semantic 未排序已覆盖。
- T4/T6/T7 六种 canonical fallback reason 均断言完整 `{kind,logical_call_id,version,reason}`。
- attempts-exhausted 与 recovery 自由文本详情未泄漏到 source。
- T6 low/normal/high/critical 四档 fallback 与 T7 `Proposal=nil, NoDraft=true` 保持通过。
- valid T4/T6/T7 均断言完整 decoded output 与 `{kind,logical_call_id,prompt_version,output_schema_version}`。
- PR #439 仅修改 `internal/brain` 测试，未越界改 channel、EmitInterrupt、规格或 WBS。

## 4. 测试证据

- `go test ./internal/brain -count=1`：**PASS**。
- `go vet ./...`：**PASS**。
- `go test ./... -count=1`：**FAIL**；复现既有并行时序 flake：
  - `TestDoctorBaselineChecksConfiguredDependencies` 的 agent/tmux fixture `signal: killed`；
  - `TestLaunchWorkerWrapperCrashSuite` 未观察到 started marker。
- 两个失败分别单独运行均 **PASS**：
  - `go test ./internal/controlplane -run '^TestDoctorBaselineChecksConfiguredDependencies$' -count=1`
  - `go test ./internal/launchworker -run '^TestLaunchWorkerWrapperCrashSuite$' -count=1`
- `git diff --check 2722362..b91552b`：**PASS**。
- PR #439 的 build、schema drift、vet + test checks 均为 **SUCCESS**。

## 5. Issue #440 验收清单

| 检查项 | YES/NO | 判断 |
|---|---|---|
| T7 malformed Shell.Call 完整矩阵 | NO | 缺六个非 total count 负值、required 分层缺失及两类域内 ID 重复 |
| 每个已提交 malformed 例 call/attempt/provider = 0 | YES | 独立 DB、前后计数及 provider requests 直接断言 |
| 要求中的每个 malformed 例均有三零证据 | NO | 上述要求案例尚未提交 |
| T4/T6/T7 六值 fallback source 完整 | YES | 三触点均断言完整 closed source |
| attempts-exhausted/recovery 详情不泄漏 | YES | 两类输入含自由文本，输出只保留 canonical reason |
| T6 四档与 T7 no-draft | YES | 四档循环与显式 union 断言均在 |
| T4 非平凡合法 skeleton 逐字段无损 | NO | DeepEqual 在，但 fixture links 未排序且未先通过 `BuildT4Input` |
| valid 三触点 decoded output + 完整 source | YES | 三触点均逐对象 DeepEqual，含 schema version |
| #432 剩余 P1 可核销 | NO | malformed 矩阵及合法 T4 skeleton 证据未闭合 |
| 结论已写入 `docs/reviews/` | YES | 本报告 |
